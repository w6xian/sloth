# 媒体代理（HTTP 网关 → 内网客户端）设计

场景：用户通过 HTTP 访问公网服务端，服务端把请求经 sloth 长连接转发到内网客户端，
由客户端提供图片 / 视频（本地文件）与实时采集（摄像头 / 屏幕）。即"花生壳 / frp 那类"
反向代理，但隧道用现成的 sloth 连接，不引入额外软件。

链路：tcp / quic（fn 帧），媒体下行 = **客户端 → 服务端 → 浏览器**。

## 零、三条已核实的硬事实（决定下面所有参数）

1. **媒体字节走"返回值"，不走参数编码。**
   服务端 `handleCall` 拿到业务返回值 `rst` 后直接 `ch.Send(ctx, id, rst, err)`
   （`nrpc/utils.go:96-103` → `nrpc/conn.go:234`），响应用 **fn 帧裸 payload**，
   不经 `ag` 编码、不 base64。fn 帧 `Length` 是 uint32，上限 `FnMaxDataSize = 1<<30`
   （`decoder/fn/fn.go:36`）。
   → **tcp / quic 上单块可以到 1GB，取 256KB 毫无压力。**
2. **入参才受 `ag` 限制：单参数 ≤ 65535 字节**（`ArgumentMaxDataSize = 1<<16 - 1`，
   `decoder/ag/ag.go:32`）。→ 入参只放元数据（path / offset / length），数据一个字节都不放。
3. **服务端打客户端用 `*ClientRpc`**：`sloth.DefaultServer()` 返回它，
   `server.Call(ctx, userId, mtd, arg...)` / `CallWithHeader(ctx, header, userId, ...)`。
   `userId` 直接取上一轮 `smap` 分配的**负数 ID**（`SMap.Reg` → -1、-2…，与 Sign 的正数 ID 不冲突）。

ws 链路不适用第 1 条的乐观值：它走 `frame.DataSlice`，`T`/`I` 是 `byte`（≤256 片）
且 JSON 化有 33% 膨胀，可靠上限约 192KB。本次只做 tcp / quic。

## 一、总体结构

```
浏览器 GET /media/shop1/a.mp4   Range: bytes=0-262143
   │
   ▼
┌──────────── 公网服务端（examples/multi 加一个网关）────────────┐
│ HTTP 网关（独立 http.Server，不占用 RPC 端口）                  │
│  ① 鉴权 + svc 白名单 + 路径早筛                                │
│  ② 解析 Range / If-None-Match                                 │
│  ③ server.Call(ctx, userId, "file.Stat" / "file.Read", ...)    │
│     userId ← smap.Get(svc)（负数 ID）                          │
│  ④ 边收边写响应（206 / 200），顺序读时预取                      │
└────────────────────────┬───────────────────────────────────────┘
                         │ sloth 长连接（tcp / quic，fn 帧）
                         ▼
┌──────── 内网客户端（ws/node 那类，自己注册服务）────────────┐
│  file 服务：Stat / Read（本地磁盘）                          │
│  live 服务：Open / Chunk / Close（实时采集，可选）            │
└──────────────────────────────────────────────────────────────┘
```

服务端**按需向客户端要字节**，一次一块。HTTP 的 `Range` 就是天然的分块协议，
正好绕开"整包进内存"。

## 二、接口约定（业务层，不改库）

方法签名遵守框架约定：首参 `context.Context`，末返回值 `error`，返回值 `[]byte`。

### 2.1 `file` 服务（本地文件：图片 / 视频点播）

```go
type FileMeta struct {
	Size  int64  `json:"size"`
	Mime  string `json:"mime"`  // image/jpeg、video/mp4…
	ETag  string `json:"etag"`  // 建议 size-mtime，加引号：`"1048576-1712345678"`
	Mtime int64  `json:"mtime"`
}

// Stat 取元信息；返回值由业务自己序列化（tlv.Json 或 json.Marshal 均可）
func (s *FileService) Stat(ctx context.Context, path string) ([]byte, error)

// Read 按 offset/length 取**裸字节**（不 JSON，省 33% 膨胀）；
// 客户端必须遵守 length，不得多返回
func (s *FileService) Read(ctx context.Context, path string, offset, length int64) ([]byte, error)

// List 目录列表（可选，P1）
func (s *FileService) List(ctx context.Context, dir string) ([]byte, error)
```

### 2.2 `live` 服务（实时采集，P2）

```go
func (s *LiveService) Open(ctx context.Context, src string, opt string) ([]byte, error)   // → {streamId, mime}
func (s *LiveService) Chunk(ctx context.Context, streamId string, seq int64, waitMs int) ([]byte, error)
func (s *LiveService) Close(ctx context.Context, streamId string) ([]byte, error)
```

- 客户端内维护**环形缓冲**（如 32 帧）：满了丢最老的帧（实时优先，宁可丢帧不积压）；
- `Chunk` 取不到就在客户端侧等 `waitMs`（≤ `waitMs` 的阻塞，不要长占），超时返回空包且 `seq` 不变；
- 网关侧把 `Chunk` 序列写成 `multipart/x-mixed-replace`（MJPEG，`<img>` 可直接显示）
  或 chunked 的 fragmented MP4 / HTTP-FLV；
- 浏览器断连 → `r.Context().Done()` → 网关调 `live.Close`。

### 2.3 与 `_.Funcs` 自省的协同

网关可先调 `_.Funcs` 探测客户端是否提供 `file.Stat` / `file.Read`，没有就 404，
不硬编码能力假设。

## 三、HTTP 网关

### 3.1 路由

| 路径 | 用途 |
|---|---|
| `GET /media/{svc}/{path...}` | 图片 / 视频点播（Range） |
| `GET /live/{svc}/{src}` | 实时流（MJPEG multipart，P2） |

网关用独立 `http.Server`（照 `newHTTPServer` 的做法设 `ReadHeaderTimeout` / `IdleTimeout`），
不挂在 RPC 端口上；不要复用 `WithDebugAddr` 那个调试服务（无鉴权、语义不同）。

### 3.2 Range 解析（RFC 7233 子集）

| 情况 | 处理 |
|---|---|
| 无 `Range` | 200 全量；超过阈值仍**分块流式**写，不整包进内存 |
| `bytes=start-end` | `end` 省略 → `size-1`；`end ≥ size` → 截断为 `size-1` |
| `bytes=-N`（后缀） | `start = max(0, size-N)` |
| `start ≥ size` 或 `start > end` | **416** + `Content-Range: bytes */size` |
| 多区间 `bytes=0-99,200-299` | 只取**第一个**区间返回 206（浏览器基本不发多区间） |
| `If-Range` 与 ETag 不匹配 | 退回 200 全量 |
| `If-None-Match` 命中 | **304**，不向客户端发 RPC |

响应头（无论 200 / 206 都带前四个）：

```
Accept-Ranges: bytes          ← 没有它 <video> 不给拖动
Content-Type:  <Stat.Mime>
ETag:          <Stat.ETag>
Last-Modified: <Stat.Mtime>
Content-Length: n
Content-Range: bytes start-end/size    ← 仅 206
Cache-Control: private, max-age=0
```

### 3.3 分块与预取

- 块大小 `chunk`：**256KB**（tcp/quic 默认，可调 64KB~1MB）
- 顺序读判定：同一 `(svc,path)` 上次 `end+1 == 本次 start` → 顺序读 → 预取窗口 `prefetch=2`
  （后台取后续 2 块进内存队列，队列上限 8 块 ≈ 2MB）
- 非顺序（seek）→ 窗口清零，只取当前块
- 每拿到一块立即 `w.Write` + `http.Flusher.Flush()`，不攒整文件

### 3.4 并发与背压

- 每条客户端连接并发窗口 **4**（写侧串行，大块会队头阻塞心跳与普通 RPC）
- 按 `svc` 令牌桶限流，超限 503 + `Retry-After`
- 单次 RPC 超时：库默认 `defaultTimeout = 10s`（`nrpc/utils.go:108`）；大块 + 慢链路网关可放宽到 30s（用 context 超时）
- 浏览器断连 → `r.Context()` 取消 → 取消在途预取与 RPC

### 3.5 鉴权与安全（必做）

- 入口鉴权：`Authorization: Bearer <token>` 或带 `exp` 的 HMAC 签名 query
- `{svc}` 必须在 `smap` 已登记，否则 **404**（不区分"不存在/未登记"，避免探测）
- 路径穿越：客户端侧 `filepath.Clean` + 根目录前缀校验；服务端只做 `..` 早筛，
  **最终以客户端为准**（符号链接只有客户端能判）
- 根目录由客户端自己配置（启动参数 `--root`），网关无法指定

## 四、错误码映射

客户端返回**固定前缀**的错误文本，网关按前缀映射（不要返回裸 `os.ErrNotExist`，
文本不稳定）。建议定义成 `errs` 常量：

| 客户端 error | HTTP | 说明 |
|---|---|---|
| `fs: not found` | 404 | 文件不存在 |
| `fs: forbidden` | 403 | 越出根目录 |
| `fs: not a file` | 404 | 目录 / 特殊文件 |
| `fs: range` | 416 | offset 越界 |
| `fs: busy` | 503 | 读忙，网关可重试 1 次 |
| `live: not found` | 404 | 采集源不存在 |
| `live: closed` | 410 | 流已关闭 |
| `live: timeout` | — | 空帧，网关继续拉 |
| `fs: unsupported` | 415 | 类型不支持 |
| 其它 / 未知 | 502 | 客户端异常 |
| svc 未在 smap 登记 | 404 | 服务不在线 |
| RPC 超时 / 连接断 | 504 / 502 | |
| 鉴权失败 | 401 | |

## 五、数据格式

请求帧由库负责（`{method, header, args}`），`args` 走 ag 编码：

```json
file.Stat  args: ["/pic/a.jpg"]
file.Read  args: ["/pic/a.jpg", 0, 262144]
```

- `file.Stat` 返回（业务自己 JSON）：
  `{"size":1048576,"mime":"video/mp4","etag":"\"1048576-1712345678\"","mtime":1712345678}`
- `file.Read` 返回：**裸字节**（不 tlv / 不 JSON）
- 网关 → 浏览器：标准 HTTP，无自定义封装

## 六、实时采集：两条路

**6.1 拉模型（P2 先做这个，零库改动）**：`live.Open/Chunk/Close` + MJPEG multipart。
延迟 ≈ 一次 RTT + 一帧编码；`<img src="/live/shop1/cam0">` 直接看。
限制：JPEG 每帧 50~150KB（720p），带宽高、无音频。

**6.2 代理客户端本地 HLS / FLV（接受秒级延迟时的主推方案）**：
客户端本地起切片（ffmpeg → m3u8 + ts，或 HTTP-FLV），网关**完全复用 §2.1 的 `file.*`**
代理 m3u8 / ts —— HLS 就是一堆小文件 + Range，天然适配，不需要 `live` 服务。
延迟 3~10s（HLS）/ 1~3s（FLV），有音频。

低于 500ms 的硬实时（WebRTC 级别）不建议塞进 RPC，另开通道。

## 七、参数建议（tcp/quic）

| 参数 | 默认 | 说明 |
|---|---|---|
| chunk | 256KB | 上限 1GB；调大省 RTT，调小省内存 |
| prefetch | 2（顺序）/ 0（seek），上限 8 块 | |
| 并发窗口 | 4 / 连接 | 防队头阻塞 |
| RPC 超时 | 10s（大块 30s） | |
| 网关内存 | chunk × (1+prefetch) × 并发 ≈ 3MB / 流 | |
| Stat 缓存 | 30s（按 ETag/路径） | 省一次往返 |

## 八、实施阶段

- **P0（不动库）**：`file.Stat` / `file.Read` + 网关 Range/206 + 预取；
  新增 `examples/media`（网关服务端 + 客户端），tcp/quic 上验证图片与 mp4 拖动。
- **P1**：鉴权、错误码常量、限流、Stat 缓存、304。
- **P2**：`live` 拉模型（MJPEG）或客户端 HLS 代理（二选一，看可接受延迟）。
- **P3（可选，改库）**：ws 链路 `DataSlice.T/I` 扩到 uint32 + 发送侧 `BinaryMessage`，
  让 ws 也能用大块——**协议字段改动，需要版本号/新帧类型**。

## 九、待确认

1. 是否需要 `file.List`（目录浏览）与 `file.Thumb`（视频截图 / 缩略图）
2. 实时场景可接受延迟：>3s（HLS，省事）还是 <1s（MJPEG 拉模型）
3. 同一 svc 是否会有多条连接（要不要做负载均衡 / 故障切换）
4. 网关是否要落盘缓存热文件（省回源，代价是一致性）

## 十、已落地：方案③（隧道里跑 HTTP）在 `examples/media`

第八节的 P0（`file.Stat` / `file.Read`）**已被本方案取代**：同样甚至更少的代码，
换来了完整的 HTTP 语义。

- `examples/media/server`：RPC（tcp:8991）+ HTTP 网关（:8080）
  - `gateway()`：`/m/{svc}/{path...}` → 请求报文序列化 → `server.Call(ctx, userId, "http.Do", raw)`
    → 解析响应报文回写。`userId` 取 `smap` 的负数 ID（`v1.Reg` 登记）。
  - `dumpRequest()` 改写路径与 Host、剥 hop-by-hop 头；`writeResponse()` 原样回写
    状态码 / 头 / body。
- `examples/media/client`：本地 `http.FileServer`（回环随机端口）+ 注册 `http.Do`
  - `Do`：`http.ReadRequest` → 改写成本地地址 → `RoundTrip` → `resp.Write` 返回报文。
  - Range / 206 / Content-Type / MIME / 304 全部由 `http.FileServer` 提供，本例未实现一行。

实测（tcp，1MB `sample.bin`）：

| 请求 | 结果 |
|---|---|
| `/m/media/hello.txt` | 200，`Content-Type: text/plain`，正文正确 |
| `-r 100-199` | **206**，`Content-Range: bytes 100-199/1048576`，100 字节逐字节正确 |
| 全量 | 200，`Content-Length: 1048576`，`Accept-Ranges: bytes` |
| 未登记的 svc | 404（不区分"不存在"与"未登记"） |

损耗：请求报文 84~107 字节，单块转发耗时 4~21ms（首次 514ms 是连接预热）。

**这节的结论**：要点播能力，不需要新帧、不需要自造协议，也不需要自己实现 Range——
把 HTTP 报文塞进 RPC 就行，剩下的交给 HTTP 生态。第八节 P0 保留仅作对照。

## 十一、已落地：网关侧分块转发（解决大文件 502）

第十节"一次 RPC 一个完整报文"有个硬上限：响应是**整包**进内存的
（客户端 `ProxyService.Do` 里 `resp.Write` 攒完整个报文才返回，另有
`maxRespBytes` 32MB 兜底）。100MB 的 mp4 整包转发必然 502——`media.mp4`
打不开就是这个原因：网关回 502，日志里 `err` 是 `response too large`。

改法是**网关侧分块**，客户端一行没动：

- 转发前先探测大小：发一次 `Range: bytes=0-0`，从响应的
  `Content-Range: bytes 0-0/SIZE` 取总大小，按 `sizeTTL`(30s) 缓存，
  省掉每个 Range 请求多出的一个 RTT；
- 不管浏览器有没有发 Range，网关都按 `mediaChunk = 256KB` 一段段向客户端
  要字节：每块一次 RPC，拿到就 `w.Write` + `Flush`；
- 应答：浏览器发了 Range → 206 + `Content-Range: bytes start-end/size`；
  没发 → 200 全量，但内部同样分块（内存与文件大小无关）；
- Range 解析覆盖 `bytes=a-b` / `bytes=a-` / `bytes=-n` / 多区间取第一个；
  end 越界截断，start 越界 → 416 + `Content-Range: bytes */size`，
  语法不认识的按 RFC 忽略（当全量处理）；
- 客户端忽略 Range 回了 200，或回了 404 / 304 / 416 → 原样回写，不进分块循环；
- 浏览器断连 → `r.Context()` 取消在途块，不继续拉。

于是单块永远 256KB，不再撞客户端上限，网关与客户端内存恒定在几百 KB。

实测（tcp，120MB `big.bin`，480 块）：

| 请求 | 结果 |
| --- | --- |
| `-r 0-99` | 206，`bytes 0-99/125829120`，100 字节 |
| `-r 100000000-100000099` | 206，中间块正确（1.6ms） |
| `-r -1000` | 206，`bytes 125828120-125829119/125829120` |
| `-r 999999999-` | **416** + `bytes */125829120` |
| 全量 | 200，125829120 字节，**MD5 与源文件一致**，0.7s |
| 未登记 svc / 文件不存在 | 404 |

> 媒体仍只建议走 tcp / quic：ws 还有一层 `DataSlice` 分片（T/I 是 byte）与
> JSON 膨胀，大块不划算。要做实时流或想彻底摆脱"借 Range 分块"，是上一节
> 说的流式响应（协议层改动）。
