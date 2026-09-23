看完了，`examples/media` 是"方案③：隧道里跑 HTTP"——一次 RPC 承载一个完整 HTTP 报文（请求走入参、响应走返回值），Range/206/MIME/304 全交给客户端本地的 `http.FileServer`，已实测 206 分片正确。

结论：**能放进库，建议分两步。协议层今天其实已经能承载 media，缺的是库级封装 + 流式/背压。**

## 一、协议侧的既有约束（决定方案边界）

| 环节 | 现状 | 出处 |
|---|---|---|
| 请求（入参） | ag 编码，单参数 ≤ 65535 → 只放得下请求头 | `decoder/ag` `ArgumentMaxDataSize` |
| 响应（返回值） | fn 帧裸 payload，`Length` 是 uint32 → 上限 1GB；不经 ag、不 base64 | `decoder/fn/fn.go:16-25` |
| 帧结构 | magic(2)+Action(1)+ID(8 BE)+Length(4 BE)+Data，Action 只用了 0x01/0x02/0x03/0xFF | `decoder/fn/fn.go` |
| ws | 额外一层 `DataSlice`，`T`/`I` 是 byte（≤255 片）、单片上限 65535，还有 JSON 膨胀 | `decoder/frame/utils.go:9-38` |
| 回包路径 | 统一进 `PRpcBacker` 单写队列（默认容量 10），写侧串行 | `nrpc/conn.go:262-279` |

所以：**tcp/quic 现在就能跑大块，ws 不适合**；`examples/media` 里那句"ws 不划算"是对的。

## 二、方案 A：把 media 收进库（不动协议，推荐先做）

现在两端契约是散的：服务端写 `const svcRpcMethod = "http.Do"`，客户端 `conn.Register("http", proxy)`——两个 main.go 各写一份字符串，对不上就是 404/502。示例里也**没有任何鉴权**（网关在公网，内网目录裸奔）。

提议新增 `media` 包：

```go
// 服务端：网关（独立于 RPC 端口，自带鉴权 hook 与默认上限）
mux.Handle("/m/{svc}/{path...}", media.NewGateway(server, media.GatewayOptions{
    Lookup:  smap,                                   // svc -> userId
    Auth:    func(r *http.Request, svc string) bool, // 缺省即拒绝
    Timeout: 30 * time.Second,
}))

// 客户端：把本地目录暴露出去（起 FileServer + Reg + 注册，一行）
media.Serve(conn, "media", root)

// 或只拿转发服务，自己挂任意 http.Handler
conn.Register(media.Service, media.Forward(media.ForwardOptions{
    Handler: http.FileServer(http.Dir(root)),
    MaxRespBytes: 8 << 20,
}))
```

要点：
- `media.Method` 常量由库定义，两端共用，消灭字符串漂移
- 只转无 body 的 GET/HEAD；请求头超 65535 → 431（示例里已有此逻辑，收进来即可）
- 错误映射：RPC 超时 504、连接断 502、未登记 404、鉴权失败 401
- 接 `metrics`：转发次数/字节/耗时
- `examples/media` 瘦到几十行，只演示怎么调库

工作量：一个新包 300~400 行 + 测试，风险低，协议零改动。

## 三、方案 B：让协议真正"支持 media"（流式响应）

A 有两个硬伤，都是协议/传输层问题，靠封装解决不了：

1. **整包进内存**：客户端必须 `resp.Write(&buf)` 攒完整个响应才能返回（`examples/media/client/main.go:151-178`），网关也要 `http.ReadResponse` 整包解析——大文件与实时流都不可行。
2. **队头阻塞**：大回包走 `PRpcBacker` 共享单写队列（`nrpc/conn.go:262-279`），一个 32MB 回包会把同连接的心跳、普通 RPC 全堵在后面。

改法（向后兼容，不改帧头）：
- 新增 action：`ACTION_REPLY_CHUNK`（如 0x04）、`ACTION_REPLY_END`（0x05）；chunk 元数据放 payload 前缀，**帧头一字不动**
- 老版本收到未知 action 会走 default 分支报错（不解错帧），因此默认关闭，按 option/握手协商开启
- `RpcChannel.pending` 从"容量 1 的 `chan []byte`"扩成流式等待者，暴露 `CallStream(ctx, mtd, args...) (io.ReadCloser, error)`；被调侧 `SendStream(ctx, id, io.Reader)`
- chunk 走独立发送队列或限速，保证大块不堵心跳
- ws 的 `DataSlice.T/I` 是 byte，要承载流式就得改字段（真正的协议变更，需新帧类型/版本）→ 建议 ws 只走"限制大小"策略，不上流式

风险：动 `nrpc/conn.go` 的回包分发 + 三个传输写侧，是最容易埋并发 bug 的一块，必须补 tcp/quic/ws 的流式 e2e。

## 四、方案 C：QUIC 多流专用通道（暂不建议）

媒体走独立 QUIC stream，天然不占 RPC 队列。但只有 QUIC 受益，且需要连接层支持"开副通道 + 协商"。等 QUIC 用稳了再说。

## 五、建议路线

- **v4.1**：方案 A（`media` 包 + 鉴权 + 默认上限 + 瘦身示例），1~2 天含测试，协议不动
- **v4.2**：若点播大文件/实时流是硬需求，再做方案 B 的流式响应（先 tcp/quic）
- 文档与代码注释写死"媒体走 tcp/quic，ws 只做小图"，避免误用

## 六、需要你拍板

1. `media` 进主库（承诺兼容）还是 `contrib/media`（不承诺）？
2. 鉴权默认**强制**（没配 hook 就全部 401）还是保留开发模式放行？我倾向强制——网关面向公网。
3. v4.1 只做 A，还是直接把 B 一起做？
4. 实时流（`live` / MJPEG / HLS 代理）这轮做不做？还是只做点播？
5. ws 是否要求支持大块？——决定要不要动 `DataSlice` 字段。

你定方向，我再动手。