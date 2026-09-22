# Sloth v4

Sloth 是一个面向“长连接 + 实时 RPC”的 Go 框架：既可以像传统 RPC 一样调用远端方法，也可以像 IM/网关一样按 Room 做广播/推送。

目前项目内已落地的传输层：

- WebSocket：`ws / wss`（适合浏览器、跨语言）
- TCP：`tcp / tcp4 / tcp6`（FN 帧分帧的裸字节流，适合两端同构、不需要浏览器的场景；无断线重连，见下）
- QUIC：`quic`（UDP + TLS 1.3，弱网与网络切换场景友好；强制 TLS，见下）
- KCP：`kcp`（基于 `kcp-go`，适合弱网/丢包环境）—— 未实现，仅占位

> `grpc` 仍是占位符（未实现真正的 gRPC 协议栈）。

network 参数可以直接用包级常量（无类型字符串常量，传给 `Listen / Dial` 无需转换）：

| 常量 | 值 | 说明 |
|---|---|---|
| `sloth.WEBSOCKET`（简写 `sloth.WS`） | `ws` | WebSocket |
| `sloth.WSS` | `wss` | WebSocket over TLS |
| `sloth.TCP` | `tcp` | 裸 TCP |
| `sloth.QUIC` | `quic` | QUIC（`sloth.QUIK` 是拼写兼容别名，正确写法是 QUIC） |

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

# 多协议同时监听：一个进程同时开 ws / tcp / quic，三条链路共用同一份服务注册表
go run ./examples/multi
go run ./examples/multi/client
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

## 传输层差异与已知限制

换传输只需改 `Listen / Dial` 的 network 参数，业务代码（codec、dispatch、bucket、房间广播）完全共用 —— 逐行对比 [examples/ws](examples/ws)、[examples/tcp](examples/tcp) 与 [examples/quic](examples/quic) 即可看出差异有多小。目前已知的差异：

| | WebSocket | TCP | QUIC |
|---|---|---|---|
| 底层传输 | TCP | TCP | **UDP** |
| TLS | 可选（`wss`） | 未内置（可自行包 `tls.Conn`） | **强制**：加密由 TLS 1.3 承担，没有证书握不了手 |
| 断线重连 | 有：`KeepAlive` + `runRelogin`（重连后自动重新 Sign） | **无** | **无** |
| 服务端连接回调 | `option.WithServerHandleMessage`（方法带 `*http.Request`） | `option.WithTcpHandleMessage`（带对端地址，无 HTTP 依赖） | `option.WithTcpHandleMessage`（与 TCP 同一套钩子） |
| 客户端连接回调 | `option.WithClientHandleMessage` | `option.WithTcpClientHandleMessage` | `option.WithTcpClientHandleMessage` |
| HTTP 概念 | mux router / origin / uri path | 无 | 无 |
| 端口探测 | 可直接 curl（HTTP 升级握手） | 打不通：没有合法 FN 帧头会被直接断连 | 打不通：UDP，且握手的 ALPN 对不上 |
| `Dial` 行为 | 内部跑到连接断开，样例里要 `go` 出去 | 建立连接后立刻返回，可同步调用 | 握手完成后立刻返回（握手有 10s 上限） |
| 多路复用 | 一连接 = 一逻辑连接 | 一连接 = 一逻辑连接 | 一个 QUIC 连接可开多条流，每条流 = 一逻辑连接 |
| 连接限额 | 全局 / `MaxConnsWS` / 单 IP | 全局 / `MaxConnsTCP` | 全局 / `MaxConnsQUIC` |

**TCP / QUIC 无断线重连不是遗漏，而是未定的语义问题**：重连后要不要自动重新 Sign、连接身份是否重建、断连期间的房间广播要不要补发 —— 这些都得先定义清楚。在语义确定前不做，是避免埋一个"看起来能自动恢复、实际身份是错的"的坑。需要自动重连的场景请先用 `ws`，或在应用层自行包装"重连 + 重新 Sign"。

### QUIC 的几点补充说明

- **证书是必填项**：`Listen` / `Dial` 之前都要给一份 `*tls.Config`（`sloth.WithTLSConfig(...)`）。样例用运行时生成的自签证书 + 客户端 `InsecureSkipVerify`，只为能直接跑；生产请用正式证书并正常校验。
- **一条连接可以跑多条流**：服务端把每条 QUIC 流铺平成一条独立连接（见 `nrpc/quic/listener.go`），因此多路复用是天然可用的——当前客户端一条连接只开一条流，需要更多流时自行开即可。
- **空闲与保活**：默认 `MaxIdleTimeout=60s`、`KeepAlivePeriod=15s`。保活周期必须小于 idle 超时，否则中间设备（NAT / 防火墙）会静默丢掉 UDP 映射——移动网络下尤其常见。
- **端口要放 UDP**：QUIC 监听的是 UDP 端口，安全组 / 防火墙别只放 TCP。

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

## 开发与测试

```bash
go test ./...
```