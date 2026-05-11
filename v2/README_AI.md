# Sloth v2（给 AI/代码助手看的）

目标：让代码助手能快速理解 Sloth v2 的结构、协议与改动入口，便于持续迭代与排障。

## 一句话概览

Sloth v2 是一个“长连接 + 实时 RPC + Bucket/Room 广播”的 Go 框架。对外暴露统一的 `Connect.Listen/Serve/Dial` 与 `ClientRpc.Call` 接口，底层按不同传输层（WebSocket/TCP/KCP）实现连接与帧协议。

## 本地验证命令

```bash
go test ./...
go run ./examples/ws
go run ./examples/ws/client
```

## 目录速览（从入口到实现）

- 入口与核心反射调用
  - [connect.go](file:///d:/var/o4p/github.com/sloth/v2/connect.go)：`Listen/Serve/Dial` 的协议分发、`CallFunc` 反射调用核心
  - [call_server_func.go](file:///d:/var/o4p/github.com/sloth/v2/call_server_func.go)：服务端调用封装
  - [call_client_func.go](file:///d:/var/o4p/github.com/sloth/v2/call_client_func.go)：客户端调用封装
- 连接组织（海量连接相关）
  - [bucket/bucket.go](file:///d:/var/o4p/github.com/sloth/v2/bucket/bucket.go)、[bucket/room.go](file:///d:/var/o4p/github.com/sloth/v2/bucket/room.go)：Bucket/Room、Push/广播队列
- 传输层实现（每个协议一套 server/channel/client）
  - WebSocket：`nrpc/wsocket/*`
  - TCP：`nrpc/tcpsock/*`
  - KCP：`nrpc/kcpsock/*`
- 诊断与服务发现
  - [pprof/pprof.go](file:///d:/var/o4p/github.com/sloth/v2/pprof/pprof.go)：`pprof.Info`、`MemStats.NextGC` 等
- 消息模型
  - [message/message.go](file:///d:/var/o4p/github.com/sloth/v2/message/message.go)、[message/header.go](file:///d:/var/o4p/github.com/sloth/v2/message/header.go)：`Msg`/`Header`（Header 已用 pool 复用）

## 协议与数据流（非常重要）

### TCP/KCP：自定义 TLV 帧（不是 github.com/w6xian/tlv）

- 帧格式在：
  - TCP：[nrpc/tcpsock/frame.go](file:///d:/var/o4p/github.com/sloth/v2/nrpc/tcpsock/frame.go)
  - KCP：[nrpc/kcpsock/frame.go](file:///d:/var/o4p/github.com/sloth/v2/nrpc/kcpsock/frame.go)
- 格式固定：`[Type:1B][Length:4B BigEndian][Payload:LengthB]`
- `Payload` 常见是 JSON（`trpc.RpcCaller` / ReplyMessage 等），而业务参数 `Data` 往往是另一层业务编码（示例里多用 `github.com/w6xian/tlv` 的 `tlv.Json(...)`）。

### 业务参数组织规则（避免反射参数缺失）

对于 RPC 调用，务必保持：

- `Data = args[0]`
- `Args = args[1:]`

理由：`Connect.CallFunc` 在构造反射入参时，依赖 `msgReq.Data` 作为“第一个业务参数”的来源；`Args` 用于支持更多附加参数。

### WebSocket：内部队列以 `[]byte` 流转

`nrpc/wsocket` 内部已将 `rpcCaller/rpcBacker` 等通道改为 `chan []byte`，并用对象池复用 call/back 对象，避免在高频调用下产生持续堆分配。

## KCP 现状与注意事项

- KCP 使用 `kcp-go/v5`：
  - 监听：[nrpc/kcpsock/server.go](file:///d:/var/o4p/github.com/sloth/v2/nrpc/kcpsock/server.go)
  - 拨号：[nrpc/kcpsock/client.go](file:///d:/var/o4p/github.com/sloth/v2/nrpc/kcpsock/client.go)
- 当前加密 key/salt 是 demo 常量（`demo pass/demo salt`），如果要用于生产应抽到 option 里配置，并避免硬编码。

## 常见故障定位指引

- `reply timeout`
  - 优先看：reply 是否被丢弃、是否进入等待通道、是否存在通道积压（生产快于消费）
  - 用 `pprof.Info` 观察 `NumGC/NextGC/HeapAlloc`，并结合 goroutine profile 查阻塞点
- `tlv_decode_with_len value length is too long`
  - 常见是“协议/编码不匹配”：服务端按 Text/JSON 返回，但客户端按 TLV 去解；需要统一 `Protocol` 语义与业务编码策略

## 连接黑名单与限制（已落地）

- 实现文件：[conn_guard.go](file:///d:/var/o4p/github.com/sloth/v2/internal/tools/conn_guard.go)
- Options 字段：[options.go](file:///d:/var/o4p/github.com/sloth/v2/option/options.go)
- ConnOption：`WithMaxConnsGlobal/WithMaxConnsPerIP/WithMaxConnsWS/WithMaxConnsTCP/WithMaxConnsKCP/WithTrustProxyHeaders`
  - 位置：[connect_options.go](file:///d:/var/o4p/github.com/sloth/v2/connect_options.go)
- 接入点：
  - WS 握手阶段拒绝（403/429）：[ws_server.go](file:///d:/var/o4p/github.com/sloth/v2/nrpc/wsocket/ws_server.go#L204-L235)
  - TCP/KCP accept 后拒绝（close conn）：[tcpsock/server.go](file:///d:/var/o4p/github.com/sloth/v2/nrpc/tcpsock/server.go) / [kcpsock/server.go](file:///d:/var/o4p/github.com/sloth/v2/nrpc/kcpsock/server.go)
  - 计数释放在 Channel.Close：WS/TCP/KCP channel 文件内 `releaseConn()`

## 变更入口（AI 做需求/修 bug 的常用落点）

- 增加新传输层：在 `nrpc/<newsock>` 实现 `server/client/channel/frame`，再在 [connect.go](file:///d:/var/o4p/github.com/sloth/v2/connect.go) 的 `Listen/Serve/Dial/Close` 加 `case` 分发
- 优化分配：优先把热点通道改为 `chan []byte`，并用 `sync.Pool` 复用临时对象/Map（Header 已做）
