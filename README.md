# Sloth v3

Sloth 是一个面向“长连接 + 实时 RPC”的 Go 框架：既可以像传统 RPC 一样调用远端方法，也可以像 IM/网关一样按 Room 做广播/推送。

目前项目内已落地的传输层：

- WebSocket：`ws / wss`（适合浏览器、跨语言）
- TCP：`tcp / tcp4 / tcp6`（FN 帧分帧的裸字节流，适合两端同构、不需要浏览器的场景；无断线重连，见下）
- KCP：`kcp`（基于 `kcp-go`，适合弱网/丢包环境） (v3暂不支持)

> `quic / grpc` 仍是占位符（未实现真正的 QUIC / gRPC 协议栈）。

## 特性

- 多协议统一入口：同一套 `Listen / Serve / Dial / Call` API
- 反射式服务注册：`Register("v1", &Svc{}, "")`，通过 `v1.Method` 直接调用
- Header / Auth：header 透传、登录后可设置 `AuthInfo`
- Bucket / Room：面向海量连接的分桶与房间广播
- 安全：服务端 IP 黑名单 + 连接数限制（全局 / 分协议 / 单 IP）
- 诊断：内置 `pprof.Info` 服务方法，返回内存/连接/room 等信息（含 `next_gc`）

## 安装

```bash
go get github.com/w6xian/sloth/v3
```

## 快速开始

### 启动服务端（WS）

示例见 [examples/ws/main.go](file:///d:/var/o4p/github.com/sloth/v3/examples/ws/main.go)：

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

示例见 [examples/ws/client/main.go](file:///d:/var/o4p/github.com/sloth/v2/examples/ws/client/main.go)：

```go
client := sloth.DefaultClient()
conn := sloth.ClientConn(client)

go conn.Dial(ctx, "ws", "localhost:8992")

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
```

## 传输层差异与已知限制

换传输只需改 `Listen / Dial` 的 network 参数，业务代码（codec、dispatch、bucket、房间广播）完全共用 —— 逐行对比 [examples/ws](examples/ws) 与 [examples/tcp](examples/tcp) 即可看出差异有多小。目前已知的差异：

| | WebSocket | TCP |
|---|---|---|
| 断线重连 | 有：`KeepAlive` + `runRelogin`（重连后自动重新 Sign） | **无**：连接断开后需调用方自行重建连接并重新 Sign |
| 服务端连接回调 | `option.WithServerHandleMessage`（方法带 `*http.Request`） | `option.WithTcpHandleMessage`（方法带对端地址，无 HTTP 依赖） |
| 客户端连接回调 | `option.WithClientHandleMessage` | 无效：接口方法绑死 `*http.Response`，TCP 没有 HTTP 握手，传了会被忽略 |
| HTTP 概念 | mux router / origin / uri path | 无 |
| 端口探测 | 可直接 curl（HTTP 升级握手） | 打不通：没有合法 FN 帧头会被直接断连 |
| `Dial` 行为 | 内部跑到连接断开，样例里要 `go` 出去 | 建立连接后立刻返回，可同步调用 |

**TCP 无断线重连不是遗漏，而是未定的语义问题**：重连后要不要自动重新 Sign、连接身份是否重建、断连期间的房间广播要不要补发 —— 这些都得先定义清楚。在语义确定前不做，是避免埋一个"看起来能自动恢复、实际身份是错的"的坑。需要自动重连的场景请先用 `ws`，或在应用层自行包装"重连 + 重新 Sign"。

## 编码/协议说明（实用向）

- 业务方法的第一个参数通常是 `ctx context.Context`
- 参数与返回值默认以 `[]byte` 在连接上流转；项目示例里常用 `github.com/w6xian/tlv` 做结构体序列化（如 `tlv.Json(...)` / `tlv.Json2Struct(...)`）
- 诊断接口：调用 `pprof.Info` 可拿到运行时内存信息（`alloc/heap_alloc/next_gc/num_gc`）

## 服务方法签名约定

常见可用的签名（更多见示例）：

- `func (s *Svc) Test(ctx context.Context, req *T) (any, error)`
- `func (s *Svc) Sign(ctx context.Context, data []byte) ([]byte, error)`

## 开发与测试

```bash
go test ./...
```