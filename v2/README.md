# Sloth v2

Sloth 是一个面向“长连接 + 实时 RPC”的 Go 框架：既可以像传统 RPC 一样调用远端方法，也可以像 IM/网关一样按 Room 做广播/推送。

目前项目内已落地的传输层：

- WebSocket：`ws / wss`（适合浏览器、跨语言）
- TCP：`tcp / tcp4 / tcp6`
- KCP：`kcp`（基于 `kcp-go`，适合弱网/丢包环境）

> `quic / grpc` 仍是占位符（未实现真正的 QUIC / gRPC 协议栈）。

## 特性

- 多协议统一入口：同一套 `Listen / Serve / Dial / Call` API
- 反射式服务注册：`Register("v1", &Svc{}, "")`，通过 `v1.Method` 直接调用
- Header / Auth：header 透传、登录后可设置 `AuthInfo`
- Bucket / Room：面向海量连接的分桶与房间广播
- 中间件链：`middleware.Log / middleware.Recovery` 等
- 诊断：内置 `pprof.Info` 服务方法，返回内存/连接/room 等信息（含 `next_gc`）

## 安装

```bash
go get github.com/w6xian/sloth/v2
```

## 快速开始

### 启动服务端（WS + TCP + KCP）

示例见 [examples/ws/main.go](file:///d:/var/o4p/github.com/sloth/v2/examples/ws/main.go)：

```go
ctx := context.Background()

server := sloth.DefaultServer()
conn := sloth.ServerConn(server)

_ = conn.Register("v1", &HelloService{}, "")

_ = conn.Listen(ctx, "ws",  "localhost:8990", option.WithServerHandleMessage(&Handler{}))
_ = conn.Listen(ctx, "tcp", "localhost:8991")
_ = conn.Listen(ctx, "kcp", "localhost:8992")

if err := conn.Serve(); err != nil {
	panic(err)
}
```

### 启动客户端并调用

示例见 [examples/ws/client/main.go](file:///d:/var/o4p/github.com/sloth/v2/examples/ws/client/main.go)：

```go
client := sloth.DefaultClient()
conn := sloth.ClientConn(client)

go conn.Dial(ctx, "kcp", "localhost:8992")

time.Sleep(time.Second)
data, err := client.Call(ctx, "v1.Sign", []byte("sign"))
_ = data
_ = err
```

## 运行示例

```bash
go run ./examples/ws
go run ./examples/ws/client
go run ./examples/tcp
```

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

CI：见 [.github/workflows/go.yml](file:///d:/var/o4p/github.com/sloth/v2/.github/workflows/go.yml)。
