# Sloth v4

Sloth 是一个面向“长连接 + 实时 RPC”的 Go 框架：既可以像传统 RPC 一样调用远端方法，也可以像 IM/网关一样按 Room 做广播/推送。

目前项目内已落地的传输层：

- WebSocket：`ws / wss`（适合浏览器、跨语言）
- TCP：`tcp / tcp4 / tcp6`（FN 帧分帧的裸字节流，适合两端同构、不需要浏览器的场景）
- QUIC：`quic`（UDP + TLS 1.3，弱网与网络切换场景友好；强制 TLS，见下）
- KCP：`kcp`（基于 `kcp-go` 的 UDP + ARQ，弱网/丢包环境友好；自带 BlockCrypt 加密，**不需要 TLS**）

network 参数可以直接用包级常量（无类型字符串常量，传给 `Listen / Dial` 无需转换）：

| 常量 | 值 | 说明 |
|---|---|---|
| `sloth.WEBSOCKET`（简写 `sloth.WS`） | `ws` | WebSocket |
| `sloth.WSS` | `wss` | WebSocket over TLS |
| `sloth.TCP` | `tcp` | 裸 TCP |
| `sloth.QUIC` | `quic` | QUIC（`sloth.QUIK` 是拼写兼容别名，正确写法是 QUIC） |
| `sloth.KCP` | `kcp` | KCP（UDP，自带 BlockCrypt 加密，不需要 TLS） |

```go
conn.Listen(ctx, sloth.QUIC, "localhost:8992")
conn.Dial(ctx, sloth.TCP, "localhost:8991")
```

## 特性

- 多协议统一入口：同一套 `Listen / Serve / Dial / Call` API
- 反射式服务注册：`Register("v1", &Svc{}, "")`，通过 `v1.Method` 直接调用
- Header / Auth：header 透传、登录后可设置 `AuthInfo`
- Bucket / Room：面向海量连接的分桶与房间广播
- 安全：服务端 IP 黑名单 + 连接数限制（全局 / 分协议；单 IP 限额目前仅 ws 生效——它依赖 HTTP 请求头取 IP）
- 诊断：内置 `pprof.Info` 服务方法，返回内存/连接/room 等信息（含 `next_gc`）

## 安装

```bash
go get github.com/w6xian/sloth/v4
```

## 快速开始

### 启动服务端（WS）

示例见 [examples/ws/main.go](examples/ws/main.go)：

```go
ctx := context.Background()

server := sloth.DefaultServer()
conn := sloth.ServerConn(
    server,
    sloth.WithMaxConnsGlobal(20000),
    sloth.WithMaxConnsPerIP(50),
    sloth.WithMaxConnsWS(15000),
    sloth.WithMaxConnsTCP(3000),
    sloth.WithMaxConnsKCP(3000),
    sloth.WithTrustProxyHeaders(true),
)

_ = conn.Register("v1", &HelloService{}, "")
_ = conn.Listen(ctx, "ws",  "localhost:8990", option.WithServerHandleMessage(&Handler{}))

if err := conn.Serve(); err != nil {
	panic(err)
}
```

### 启动客户端并调用

示例见 [examples/ws/client/main.go](examples/ws/client/main.go)：

```go
client := sloth.DefaultClient()
conn := sloth.ClientConn(client)

go conn.Dial(ctx, sloth.WEBSOCKET, "localhost:8990")

time.Sleep(time.Second)
data, err := client.Call(ctx, "v1.Sign", []byte("sign"))
_ = data
_ = err
```

## 运行示例

```bash
# WebSocket
go run ./examples/ws
go run ./examples/ws/client

# TCP
go run ./examples/tcp
go run ./examples/tcp/client

# QUIC（服务端运行时生成自签证书，客户端跳过校验，仅示例）
go run ./examples/quic
go run ./examples/quic/client

# KCP（两端必须用同一份 KCP 参数：加密方式 / 密钥 / FEC）
go run ./examples/kcp
go run ./examples/kcp/client

# 多协议同时监听：一个进程同时开 ws / tcp / quic，三条链路共用同一份服务注册表
go run ./examples/multi
go run ./examples/multi/client

# 媒体代理（HTTP 反向代理）：把内网客户端的目录暴露成公网 HTTP
go run ./examples/media/server   # RPC :8991 + HTTP 网关 :8080，浏览器打开 http://localhost:8080/
go run ./examples/media/client   # 连上来，把 -root 指向的目录暴露成 /m/media/
```

### 多协议同时监听

同一个 `Connect` 上可以 `Listen` 多条协议，一次 `Serve()` 全部拉起，业务侧无感：

```go
drpc := sloth.ServerConn(server, sloth.WithTLSConfig(tlsConf)) // QUIC 需要 TLS
drpc.Listen(ctx, sloth.WS,   "localhost:8990", wsOpts...)
drpc.Listen(ctx, sloth.TCP,  "localhost:8991", streamOpts...)
drpc.Listen(ctx, sloth.QUIC, "localhost:8992", streamOpts...)
go drpc.Serve()
```

- 三条链路共用同一份服务注册表，客户端从哪条协议连上来都能调到全部方法；
- 服务端主动推送（`Call` / `CallRoom` / `CallBucket` / `Broadcast`）会**覆盖三条链路**——
  连接分桶是每个传输各自持有的，库内部用一个合成实例跨传输查找；
- 给 QUIC 配的 TLS 只作用于 QUIC（与 `wss`），ws / tcp 端口仍是明文。

## 怎么选传输：ws / wss / tcp / quic / kcp

五个 network 共用同一套服务注册与调用 API，差异全在"链路特性"上。先给结论，再给理由。

### 一句话结论

| 选它 | 当且仅当 |
|---|---|
| **ws** | 内网 / 可信网络，客户端有浏览器，或链路要过 HTTP 基础设施（Nginx 反代、80/443、CDN） |
| **wss** | 同上，但连接要过公网——生产环境对外几乎都该用它 |
| **tcp** | 两端都是自己的 Go 程序、同机房或内网、要最低开销；不需要浏览器、不需要 HTTP 概念 |
| **quic** | 弱网 / 移动网络 / 跨地域，要标准 TLS 与多流复用，且有人维护证书 |
| **kcp** | 高丢包 + 延迟敏感（实时对战、语音信令、跨境链路），且不想引入证书体系 |

拿不准就**先 ws**：生态最好、重连语义最完整、出问题最好排查。等链路特性真成了瓶颈再换——换传输只改 `Listen / Dial` 的 network 与连接回调，业务代码一行不动。

### 决策三问

1. **客户端里有浏览器吗？链路要穿 Nginx / CDN / 公司代理吗？**
   是 → `ws`（过公网则 `wss`）。它是唯一能借 HTTP 基础设施的选择；其余几个都是"自己的协议跑在自己的端口上"，中间设备帮不上忙，反而常拦 UDP。
2. **两端都在内网、都是自己的 Go 服务、要最低延迟与 CPU？**
   是 → `tcp`。没有 TLS、没有 HTTP 头、没有握手，开销最小。
3. **链路会丢包或会切换（移动网络、跨运营商、跨境）？**
   是 → 在 `quic` 与 `kcp` 之间挑：
   - 要标准 TLS、要一条连接开多条流、能维护证书 → `quic`
   - 要尽可能低的延迟、不想碰证书、愿意用带宽换延迟 → `kcp`

### 各协议的适用边界

**ws / wss —— 默认选择**

- 适合：浏览器直连（[examples/ws/web](examples/ws/web) 下有配套的 JS 客户端）、走 Nginx 反代与 80/443、需要 HTTP 概念（mux router / origin 校验 / uri path）、需要"重连即恢复会话"。
- `ws` 是五个里**唯一**带 `KeepAlive` + `runRelogin`（重连后自动重新 Sign）的；`tcp / quic / kcp` 重连只恢复连接、不恢复会话，必须在客户端 `OnReady` 里自己重新 Sign。
- 单 IP 连接限额目前**仅 ws 生效**（它靠 HTTP 请求头取 IP）。挂在反代后面要开 `sloth.WithTrustProxyHeaders(true)` 才认 `X-Forwarded-For`，且只在可信代理之后开——否则这个头可以伪造，限额形同虚设。
- `wss` = `ws` + TLS，配置是 Connect 级的：`sloth.WithTLSConfig(...)` 或 `sloth.WithTLSCertKey(certFile, keyFile)`。这一份配置同时供 QUIC 使用，但**不会**把 ws / tcp 端口也变成 TLS。

**tcp —— 内网同构首选**

- 适合：服务间调用、IM/游戏集群的内部节点、不需要浏览器与 HTTP 的一切场景；支持 `tcp / tcp4 / tcp6`。
- 不适合：需要 TLS 的场景（未内置，要自己包一层 `tls.Conn` 再交给库）、需要会话级重连恢复的场景。
- 别用 curl 探活：没有合法 FN 帧头会被直接断连，这是预期行为。

**quic —— 弱网 + 标准 TLS**

- 适合：移动端 / 跨地域长连接 / 一条连接要跑多条流；加密由 TLS 1.3 承担，没有证书握不了手。
- 代价：证书是必填项、监听的是 UDP 端口（安全组别只放 TCP）、用户态协议栈的 CPU 高于 tcp、每次重拨另受 10s 握手上限约束。

**kcp —— 高丢包 + 延迟敏感**

- 适合：丢包率高且延迟敏感的实时链路；自带 BlockCrypt 做载荷加密，**不需要证书**。
- 代价：FEC 是用带宽换延迟（默认未开）、UDP 没有连接状态（对端掉电只能等 `ReadWait` 超时才发现）、两端配置必须逐项一致否则表现为"连得上、一直没响应"。
- 要保密就别用默认的 `none` 加密。

### 常见误选

- **"为了性能"上 quic / kcp**：内网零丢包时它们只会更慢（UDP + 用户态重传/加密），性价比不如 tcp。
- **要"重连后不用管登录"却选了 tcp / quic / kcp**：这三个只恢复连接，会话得自己在 `OnReady` 里重建；不想写这段就用 ws。
- **把 wss 当"加密的 tcp"**：它仍是 WebSocket，带 HTTP 升级与 mux 路由；纯服务间调用应该要 tcp。
- **在只放通 TCP 的网络里选 quic / kcp**：UDP 不通时表现为超时，排查起来很像"服务端挂了"。
- **KCP 配置只改了一端**：不报错，只静默超时（见下方 KCP 补充说明）。

### 组合用法：一个进程开多条

选型不必二选一——对外与对内可以各用一套：

```go
drpc := sloth.ServerConn(server, sloth.WithTLSConfig(tlsConf)) // wss 与 quic 共用这一份
drpc.Listen(ctx, sloth.WSS,  "0.0.0.0:443",   wsOpts...)      // 对外 / 浏览器
drpc.Listen(ctx, sloth.TCP,  "10.0.0.1:8991", streamOpts...)  // 内网服务间
drpc.Listen(ctx, sloth.QUIC, "0.0.0.0:8992",  streamOpts...)  // 移动端 / 弱网
drpc.Listen(ctx, sloth.KCP,  "0.0.0.0:8993",  kcpOpts...)     // 高丢包 / 延迟敏感
go drpc.Serve()
```

这些链路共用同一份服务注册表，服务端主动推送会覆盖全部链路（见"多协议同时监听"）。连接回调按传输挑名字即可：`option.WithServerHandleMessage`（ws/wss）、`WithTcpHandleMessage`、`WithQuicHandleMessage`（别名）、`WithKcpHandleMessage`（别名）。

## 传输层差异与已知限制

换传输只需改 `Listen / Dial` 的 network 参数，业务代码（codec、dispatch、bucket、房间广播）完全共用 —— 逐行对比 [examples/ws](examples/ws)、[examples/tcp](examples/tcp) 与 [examples/quic](examples/quic) 即可看出差异有多小。目前已知的差异：

| | WebSocket | TCP | QUIC | KCP |
|---|---|---|---|---|
| 底层传输 | TCP | TCP | **UDP** | **UDP** |
| TLS | 可选（`wss`） | 未内置（可自行包 `tls.Conn`） | **强制**：加密由 TLS 1.3 承担，没有证书握不了手 | **不需要**：加密由 KCP 自带的 BlockCrypt 承担 |
| 传输参数 | — | — | `*tls.Config`（必填） | `option.WithKCPConfig`：加密方式 / 密钥 / FEC / 窗口 / nodelay 等 |
| 断线重连 | 有：`KeepAlive` + `runRelogin`（重连后自动重新 Sign） | 有：退避重连（只恢复本地身份） | 有：退避重连（只恢复本地身份，每次重拨另受 10s 握手上限约束） | 有：退避重连（只恢复本地身份） |
| 服务端连接回调 | `option.WithServerHandleMessage`（方法带 `*http.Request`） | `option.WithTcpHandleMessage`（带对端地址，无 HTTP 依赖） | `option.WithQuicHandleMessage`（`WithTcpHandleMessage` 的别名，同一套钩子） | `option.WithKcpHandleMessage`（`WithTcpHandleMessage` 的别名，同一套钩子） |
| 客户端连接回调 | `option.WithClientHandleMessage` | `option.WithTcpClientHandleMessage` | `option.WithQuicClientHandleMessage` | `option.WithKcpClientHandleMessage` |
| HTTP 概念 | mux router / origin / uri path | 无 | 无 | 无 |
| 端口探测 | 可直接 curl（HTTP 升级握手） | 打不通：没有合法 FN 帧头会被直接断连 | 打不通：UDP，且握手的 ALPN 对不上 | 打不通：UDP；两端 KCP 参数不一致时**静默无响应** |
| `Dial` 行为 | 内部跑到连接断开，样例里要 `go` 出去 | 建立连接后立刻返回，可同步调用；之后后台自动重连 | 握手完成后立刻返回（握手有 10s 上限）；之后后台自动重连 | 建立连接后立刻返回；之后后台自动重连 |
| 多路复用 | 一连接 = 一逻辑连接 | 一连接 = 一逻辑连接 | 一个 QUIC 连接可开多条流，每条流 = 一逻辑连接 | 一连接 = 一逻辑连接 |
| 连接限额 | 全局 / `MaxConnsWS` / 单 IP | 全局 / `MaxConnsTCP` | 全局 / `MaxConnsQUIC` | 全局 / `MaxConnsKCP` |

**TCP / QUIC / KCP 的断线重连只恢复"连接"，不恢复"会话"**：三者共用同一个循环（`nrpc.ServeReconnect`，首次拨号同步返回 error，之后后台按 500ms → 30s 退避重拨，连接被断掉则立刻重连），重连时会把已保存的身份补到新连接上，但**不会**自动重新 Sign，也不会重建服务端 bucket 里的 channel、不补发断连期间的房间广播 —— 这些语义仍未定，交回业务决定。

因此使用 TCP / QUIC / KCP 客户端时，**必须在客户端钩子的 `OnReady` 里重新 Sign/Reg**（不能在 `OnReady` 里同步发 RPC：读写泵还没跑起来会等不到回包，应另起 goroutine）。需要"重连即自动恢复会话"的场景请先用 `ws`（`KeepAlive` + `runRelogin`）。

### QUIC 的几点补充说明

- **证书是必填项**：`Listen` / `Dial` 之前都要给一份 `*tls.Config`（`sloth.WithTLSConfig(...)`）。样例用运行时生成的自签证书 + 客户端 `InsecureSkipVerify`，只为能直接跑；生产请用正式证书并正常校验。
- **一条连接可以跑多条流**：服务端把每条 QUIC 流铺平成一条独立连接（见 `nrpc/quic/listener.go`），因此多路复用是天然可用的——当前客户端一条连接只开一条流，需要更多流时自行开即可。
- **空闲与保活**：默认 `MaxIdleTimeout=60s`、`KeepAlivePeriod=15s`。保活周期必须小于 idle 超时，否则中间设备（NAT / 防火墙）会静默丢掉 UDP 映射——移动网络下尤其常见。
- **端口要放 UDP**：QUIC 监听的是 UDP 端口，安全组 / 防火墙别只放 TCP。

### KCP 的几点补充说明

- **两端配置必须一致**：`option.WithKCPConfig` 给的加密方式（`Crypt`）、密钥（`Key`）与 FEC 分片数**在建监听器那一刻就固定了**——它们决定线上包的格式，之后改不了。两端不一致不会报"配置不匹配"，而是解不开对端的包，表现为**连得上、调用一直超时、日志里没有任何错误**。"连得上但没响应"先查这份配置。
- **不需要证书**：与 QUIC 相反，KCP 自带 BlockCrypt 做载荷加密（`none` / `aes` / `salsa20` / `tea` / `xor` / `blowfish` / `sm4`），没有证书也能跑。要保密就别用默认的 `none`。
- **FEC 是用带宽换延迟**：`DataShards=10, ParityShards=3` 表示每 10 个包带 3 个冗余包，丢包时可直接恢复而不必等重传；丢包率高的链路值得开，带宽紧张则不要。
- **默认不做低延迟调优**：零值 `KCPConfig{}` 就是 kcp-go 的原生行为，不会悄悄改默认参数。要低延迟用 `option.FastKCPConfig()`（nodelay + 128 窗口 + 流模式 + 立即 ACK），代价是关闭拥塞控制、ACK 流量翻倍——共享带宽环境慎用 `NC=1`。
- **UDP 没有连接状态**：对端掉电时本端收不到任何通知，只能等读超时（`ReadWait`）才发现连接已死，"半开连接"的检出比 TCP 慢。
- **端口要放 UDP**：与 QUIC 一样，安全组 / 防火墙别只放 TCP。

## 可复用的子包（不起连接也能用）

这些包与传输层解耦，可单独 import：

| 包 | 用途 | 常用入口 |
| --- | --- | --- |
| `utils` | 序列化 / 类型收敛 / 数值 / CRC | `utils.Serialize`、`utils.AnyToBytes`、`utils.Max` |
| `utils/id` | ID 生成 | `id.ShortID()`、`id.NextId(svr)`、`id.RandStr(n)` |
| `utils/array` | 切片小工具 | `array.InArray`、`array.Map` |
| `tools` | 雪花 ID、随机 token、cityhash | `tools.GetSnowflakeId`、`tools.CityHash64` |
| `codec` | 帧编解码接口（自定义协议时实现它） | `codec.Codec`，配合 `option.WithCodec` |
| `transport` | 与协议无关的帧分发（自研传输可复用） | `transport.NewFrameRouter` |
| `ref` | 方法注册 + 反射调用 | `ref.Register`、`ref.CallFuncWithContext` |
| `logger` / `metrics` | 日志门面 / 指标与调试端点 | `logger.SetLogger`、`metrics.NewCounter` |
| `errs` | 哨兵错误，配合 `errors.Is` 判定（根包 `sloth` 已转发） | `errs.ErrTimeout` |

以前它们都在 `internal/` 下，外部项目 import 不到——连 `option.WithCodec` 的形参类型 `codec.Codec` 都拿不到，想自定义编解码器无从下手。v4 已全部提到顶层，`internal/` 不再有内容。

## 编码/协议说明（实用向）

- 业务方法的第一个参数通常是 `ctx context.Context`
- 参数与返回值默认以 `[]byte` 在连接上流转；项目示例里常用 `github.com/w6xian/tlv` 做结构体序列化（如 `tlv.Json(...)` / `tlv.Json2Struct(...)`）
- 诊断接口：调用 `pprof.Info` 可拿到运行时内存信息（`alloc/heap_alloc/next_gc/num_gc`）

## 服务方法签名约定

常见可用的签名（更多见示例）：

- `func (s *Svc) Test(ctx context.Context, req *T) (any, error)`
- `func (s *Svc) Sign(ctx context.Context, data []byte) ([]byte, error)`

## 自省约定：`_.Funcs`

每个 `*Connect`（服务端 `ServerConn`、客户端 `ClientConn` 都一样）在建实例时会自动注册一个名叫 `_` 的元服务，对端可以据此读这一侧注册了哪些服务、每个方法的入参和返回值：

```go
data, err := client.Call(ctx, "_.Funcs")            // 读服务端的方法清单
data, err := server.Call(ctx, userId, "_.Funcs")    // 读某个客户端的方法清单
```

方法名**大小写敏感、精确匹配**：它和别的服务方法一样就是 Go 的导出方法，写成 `"_.funcs"` 只会得到 `ErrMethodNotFound`（不做首字母纠偏，避免调用方误以为大小写无关）。

返回体是 MCP `tools/list` 的形态，可直接喂给 MCP client：

```json
{
  "tools": [
    {
      "name": "v1.Sign",
      "description": "metadata",
      "inputSchema": {
        "type": "object",
        "properties": { "arg0": { "type": "string", "description": "[]uint8" } },
        "required": ["arg0"]
      },
      "outputSchema": {
        "type": "array",
        "prefixItems": [
          { "type": "string", "description": "[]uint8" },
          { "type": "string", "description": "error" }
        ],
        "items": false
      }
    }
  ]
}
```

几点约定：

- **入参是位置参数**（`arg0` / `arg1`…），不是具名对象 —— Go 反射只保留形参类型、不保留形参名，具名信息给不出来就让调用方按位置传。
- **只忽略第一个 `ctx context.Context`**：它由框架注入，不是调用方传的；后面若还有 context.Context 参数，那是业务自己要的，照样列出来（漏了调用方就会少传参数）。
- **`error` 在返回值里如实列出**：它是签名的一部分，自省不该藏起来。
- **`description` 是 Go 类型名**：JSON Schema 只能表达 `string` / `integer` 这类基本类型，真正要用的 `*types.AuthInfo`、`[]uint8` 放在这里。
- **清单不含 `_` 自己**：它的用途是"照着它拼调用"，列上 `_.funcs` 只会引诱调用方递归。
- **`Register` 的第三个参数（服务描述）按服务名存**，以前是单个字段，注册第二个服务就把第一个的描述覆盖了（`meta` 头也是错的）；现在它出现在每个 tool 的 `description` 里。
- 业务自己注册 `_` 会拿到 `service _ already registered` —— 内置实现优先。

## 媒体代理：把内网 HTTP 服务暴露出来

用户走 HTTP 访问公网服务端，服务端把请求经 sloth 长连接转发到内网客户端，由客户端的
**本地 HTTP 服务**应答——也就是"花生壳 / frp 那类"反向代理，但隧道直接复用 sloth 连接。
完整示例见 [examples/media](examples/media)。

```
浏览器 ──HTTP──▶ 服务端网关(:8080)
                  ① 请求报文序列化（req.Write）
                  ② server.Call(ctx, userId, "http.Do", raw)
                                ─────▶ 客户端 http.ReadRequest → 打给本地 http.FileServer
                                ◀────── 响应报文（裸字节）
                  ③ http.ReadResponse → 原样写回
浏览器 ◀──HTTP── 网关
```

不新增帧、不新增协议：**一次 RPC 承载一个完整的 HTTP 报文**。客户端侧只需要一个方法：

```go
func (s *ProxyService) Do(ctx context.Context, rawReq []byte) ([]byte, error)
```

服务端网关按服务名找到那条连接并转发（`userId` 是 `v1.Reg` 分配的负数 ID，见示例）：

```go
userId, ok := smap.Get(svc)                            // 未登记 → 404
resp, err := server.Call(ctx, userId, "http.Do", raw)  // raw = 请求报文
```

于是 Range / 206 / Content-Type / MIME / 304 全部由客户端本地的 `http.FileServer` 提供，
两端都只负责搬运，**一行都不用自己实现**；视频拖动就是浏览器发一个新的 Range 请求。

实测（先起 server 再起 client，浏览器打开 <http://localhost:8080/>）：

| 请求 | 结果 |
|---|---|
| `/m/media/wallpaper.jpg` | 200 `image/jpeg`，`Accept-Ranges: bytes` |
| `-r 0-1023` | **206**，`Content-Range: bytes 0-1023/393630` |
| 未登记的服务名 | 404（不区分"不存在"与"未登记"，避免被探测） |

两个尺寸前提，同时也是这套做法的边界：

| 方向 | 载体 | 上限 | 结论 |
|---|---|---|---|
| 请求报文 | 入参（ag 编码） | 单参数 65535 字节 | 只放请求头 → **只转发不带 body 的 GET** |
| 响应报文 | 返回值（fn 帧裸 payload） | 1GB | 一个 206 分片绰绰有余 |

其余注意点：

- **走 tcp / quic**：fn 帧的 `Length` 是 uint32、上限 1GB，且响应不 base64。ws 那层还有
  `DataSlice` 分片（`T`/`I` 是 byte，≤256 片），大块不划算。
- **无 Range 的大响应会整包进内存**（示例在客户端侧限 32MB）。正常播放靠浏览器发 Range，
  不会触发；实时采集请让客户端本地出 HLS/FLV，走的还是同一条通道（就是一堆小文件）。
- **网关目前没有鉴权**：把 `gatewayAddr` 改成 `:8080` 对外暴露前，务必先加鉴权
  （token 或带 `exp` 的签名），否则等于开放内网目录。
- **路径穿越的最终防线在客户端**：服务端能早筛 `..`，但符号链接只有客户端判得了。

完整设计（错误码映射、参数建议、实时采集的两条路）见 [doc/media-proxy.md](doc/media-proxy.md)。

## 开发与测试

```bash
go test ./...
```