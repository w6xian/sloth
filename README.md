# Sloth v3

Sloth 是一个 **基于 WebSocket 的长连接 + 实时 RPC + 分桶广播** Go 框架。浏览器客户端、IoT 设备、Sloth 集群内节点 **共用同一个 WS 端点 /ws**：框架根据首 2 字节是否等于 Fn 帧头 `@F` 自动分流到「节点间 nrpc 路径」或「终端用户网关路径」。

v3 只保留 WebSocket（ws / wss）。TCP / KCP / QUIC / gRPC 不再提供。

## 核心能力一览

- **单端口多路复用**：同一 `ws://host:8990/ws` 上跑 3 种流量（Fn 帧 / 业务 TLV / 纯网关 JSON）。
- **反射式服务注册**：`Register("v1", &HelloService{}, "")`，通过 `v1.Reg`、`v1.Test(...)`、`shop1.Test1(...)` 直接按名字呼叫。
- **SMap + UseProxyHandler 代理调度**：`shop1.Reg(name)` 先登记到 SMap，后续 `shop1.Test1 → UseProxyHandler("shop1.Test1") → 查表 → 路由给对应 userId 的节点`。
- **Bucket / Room 分桶模型**：`IBucket → Buckets[CityHash(userId) % N] → Room(链表) → IChannel`，用户 hash 路由 + 房间广播。
- **Fn 协议帧**：`@F(2) + action(1) + callId(8) + length(4) + data(N)`（对应 nrpc/wsocket/ws_server.go L333-335）。
- **大报文分片**：`decoder/frame` + `slicesBinarySend`，分片头 `name` 固定 2 字节（00~99）。
- **对象池**：`RpcCaller` / `JsonBackObject` / `Header` / `JsonValue` 全链路 sync.Pool 复用。

## 目录结构

```
.
├── connect.go / connect_options.go  # 服务注册表 + CallFunc 反射调用 + ctx Key 注入
├── call_server_func.go              # ServerRpc：SetAuthInfo、Call(userId, "svc.Method", args...)、PushRoom、Broadcast
├── call_client_func.go              # ClientRpc：Call/CallWithHeader/CallAsync、SetAuthInfo、Push
├── const.go                         # Encoder func(any)([]byte,err) / Decoder func([]byte)([]byte,err)
├── smap.go                          # SMap：string→int64 注册映射，Reg/Get/Del 统一 TrimSpace，默认 idx-- 产负号 svrId
├── proto.go / listener.go           # 协议注册、Dial / Listen 工厂
├── actions/                         # ACTION_CALL / REPLY_SUCCESS / REPLY_ERROR / BROADCAST ...
├── bucket/                          # Bucket（分桶 map+rooms）/ Room（链表）/ IChannel 接口
├── types/
│   ├── auth/i.go                    # AuthInfo 接口 {UserId, RoomId, Token}
│   ├── trpc/rpc.go                  # RpcCaller {Header/Method/Args/Data/Channel/Error} + IChannel
│   ├── bucket.go / server.go        # 跨包 IBucket / IServer
├── decoder/
│   ├── fn/fn.go                     # Fn 协议：Magic(@F) / Encode / Decode / IsFn / Action / Id / Data
│   └── frame/                       # Split / Encode / FromType：name 必须固定 2B（getSliceName 用 %02d）
├── message/                         # Msg / Header / CmdReq / JsonCallObject / JsonBackObject
├── nrpc/wsocket/                    # WsServer / WsClient / WsChannelServer / WsChannelClient + pool
├── internal/
│   ├── ref/func.go                  # SuitableMethods 过滤（必须首参 context.Context，末参 error）
│   ├── utils/id/shortuuid.go        # id.NextId(1) snowflake → callId uint64
│   └── tools/cityhash.go            # CityHash32(userIdStr) % bucketIdx 路由
└── examples/ws/
    ├── main.go                      # 服务端：Register(v1) + Listen WS + UseProxyHandler
    ├── client/main.go               # 客户端：Dial → v1.Sign 登录 → v1.Reg("shop1"/"shop2") 登记 → shop1.Test1 代理呼叫
    └── node / node1                 # 节点互调示例
```

## 架构：单端口复用 + Fn 帧首字节分流

```
              ┌──────────────────────────────── Sloth 进程 ────────────────────────────────┐
              │                                                                           │
  浏览器/终端 │  ws(s)://host:8990/ws                                                    │
 ───────────► │   │                                                                       │
              │   ├── receiveMessage → tlv.Deserialize(可空) → 首 2B 判定                  │
              │   │                                                                       │
              │   ├─ [首 2B == @F] ───────────► Fn 帧路径（nrpc / 节点互调）                │
              │   │                                 fn.Action(data)                       │
              │   │                                  ├─ ACTION_CALL     → HandleFn → Connect.CallFunc / CallNetFunc
              │   │                                  ├─ REPLY_SUCCESS   → 匹配 callId 写回 channel
              │   │                                  └─ REPLY_ERROR     → 匹配 callId 返回 error
              │   │                                                                       │
              │   └─ [否则] ──────────────────►  网关路径（终端客户端 OnOpen/OnData/OnClose）│
              │                                              │                            │
              │                                              ▼                            │
              │               Auth: {UserId, RoomId, Token} → svr.Bucket(userId).Put      │
              │                                              │                            │
              │                                              ▼                            │
              │                Buckets[ CityHash(userId) % bucketIdx ]                     │
              │                   └─ Room(roomId)：双向链表 IChannel + Push 广播             │
              │                                                                           │
  另一节点     │  ws(s)://host:8990/ws (发 Fn 帧 @F…)                                       │
 ───────────► │   WsChannelClient.Call/CallNet → fn.Encode(ACTION_CALL, callId, payload) │
              │                                                                           │
              └───────────────────────────────────────────────────────────────────────────┘
```

## 快速开始

### 1. 定义服务 + 启动服务端

```go
// examples/ws/main.go
package main

import (
    "context"
    "fmt"
    "strings"

    "github.com/gorilla/mux"
    "github.com/w6xian/sloth/v3"
    "github.com/w6xian/sloth/v3/bucket"
    "github.com/w6xian/sloth/v3/message"
    "github.com/w6xian/sloth/v3/option"
    "github.com/w6xian/sloth/v3/types"
    "github.com/w6xian/sloth/v3/types/auth"
    "github.com/w6xian/tlv"
)

var smap = sloth.NewSMap()

type HelloService struct{ Id int64 }

// v1.Sign：登录 → 返回 AuthInfo{UserId:2, RoomId:1}
func (h *HelloService) Sign(ctx context.Context, data []byte) ([]byte, error) {
    ch, _  := ctx.Value(sloth.ChannelKey).(bucket.IChannel)
    svr, _ := ctx.Value(sloth.BucketKey).(types.IBucket)
    a := auth.AuthInfo{UserId: 2, RoomId: 1, Token: "token_123"}
    svr.Bucket(a.UserId).Put(a.UserId, a.RoomId, a.Token, ch)
    return tlv.Json(a)
}

// v1.Reg：把服务名 name（"shop1"/"shop2"）登记到 SMap → 返回负号 svrId，再 Put 到 Bucket
func (h *HelloService) Reg(ctx context.Context, name string) ([]byte, error) {
    ch, _  := ctx.Value(sloth.ChannelKey).(bucket.IChannel)
    svr, _ := ctx.Value(sloth.BucketKey).(types.IBucket)
    svrId, err := smap.Reg(name, false) // TrimSpace 过，前后空白不产生脏 key
    if err != nil {
        return nil, err
    }
    a := auth.AuthInfo{UserId: svrId, RoomId: -1, Token: fmt.Sprintf("token_%s", name)}
    svr.Bucket(a.UserId).Put(a.UserId, a.RoomId, a.Token, ch)
    return tlv.Json(a)
}

// v1.Test：普通业务调用
func (h *HelloService) Test(ctx context.Context, ab *struct{ A, B int64 }) (any, error) {
    _ = ctx.Value(sloth.HeaderKey).(message.Header)
    return map[string]any{"A": ab.A, "B": ab.B, "reqs": h.Id}, nil
}

// 网关 handler：纯客户端非 Fn 帧会进这里
type Handler struct{}
func (h Handler) OnOpen (ctx context.Context, s types.IBucket, ch bucket.IChannel) error { return nil }
func (h Handler) OnError(ctx context.Context, s types.IBucket, ch bucket.IChannel, err error) error { return nil }
func (h Handler) OnData (ctx context.Context, s types.IBucket, ch bucket.IChannel, _ int, msg []byte) error {
    _ = msg
    // 在这里做：解析鉴权 JSON → userId/roomId → s.Bucket(userId).Put(...)
    return nil
}
func (h Handler) OnClose(ctx context.Context, s types.IBucket, ch bucket.IChannel) error { return ch.Close() }

func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    server := sloth.DefaultServer()
    drpc   := sloth.ServerConn(server)

    drpc.Register("v1", &HelloService{}, "")
    drpc.Listen(ctx, "ws", "localhost:8990",
        option.WithRouter(mux.NewRouter(), "/ws"),
        option.WithServerHandleMessage(Handler{}))

    // 当请求的 service.Method 没在本机 Register 时，走这里查 SMap → 转 userId → 服务端 Call(该userId的连接)
    drpc.UseProxyHandler(func(_ context.Context, service string) (int64, error) {
        sp := strings.Split(service, ".")
        if len(sp) != 2 {
            return 0, fmt.Errorf("service %s format error", service)
        }
        svrId, ok := smap.Get(sp[0]) // TrimSpace 过
        if !ok {
            return 0, fmt.Errorf("service %s not registered", service)
        }
        return svrId, nil
    })

    if err := drpc.Serve(); err != nil {
        panic(err)
    }
}
```

### 2. 客户端：登录 → 登记服务名 → 代理调用

```go
// examples/ws/client/main.go
client := sloth.DefaultClient()
conn := sloth.ClientConn(client)
conn.Register("shop", &HelloService{}, "")   // 本机作为 shop 服务被服务端反向 Call

go conn.Dial(ctx, "ws", "localhost:8990")
time.Sleep(200 * time.Millisecond)

// (1) 普通登录（拿到本机用户身份 UserId=2）
data, _ := client.Call(context.Background(), "v1.Sign", []byte("sign"))
var a auth.AuthInfo
_ = tlv.Json2Struct(data, &a)
client.SetAuthInfo(&a) // AuthInfo.UserId=2

// (2) 把本机对外暴露的服务名登记到 SMap：
//     smap["shop1"] = -1, Bucket[-1] 指向我这条连接
_, _ = client.Call(context.Background(), "v1.Reg", []byte("shop1"))
_, _ = client.Call(context.Background(), "v1.Reg", []byte("shop2"))

// (3) 代理调用本机 shop1.Test1：
//     service=shop1.Test1 不在 v1 中 → UseProxyHandler 查 smap["shop1"]=-1
//     → 把这条请求路由给 Bucket(-1) 里的连接（也就是自己）→ 落到 conn.Register("shop", ...) 的方法
r1, e1 := client.CallWithHeader(ctx, message.Header{"APP_ID": "1"}, "shop1.Test1", []byte("abc"))
r2, e2 := client.CallWithHeader(ctx, message.Header{"APP_ID": "1"}, "shop2.Test1", []byte("abc"))
```

### 3. 运行

```bash
go run ./examples/ws            # 服务端（:8990）
go run ./examples/ws/client     # 客户端
go run ./examples/ws/node       # 节点 A
go run ./examples/ws/node1      # 节点 B
```

## 服务方法签名约定（internal/ref.SuitableMethods 过滤条件）

| 条件 | 说明 |
|---|---|
| 导出方法 | 方法名首字母大写，PkgPath 为空 |
| 入参 ≥ 2 | `In(0)=receiver`，`In(1)` 必须是 `context.Context` |
| 返回值 1~2 个 | 最后一个必须是 `error` |

合法示例：
```go
func (h *HelloService) Ping (ctx context.Context)                      error
func (h *HelloService) Sign (ctx context.Context, data []byte)         ([]byte, error)
func (h *HelloService) Test (ctx context.Context, a *AB)               (any, error)
func (h *HelloService) Multi(ctx context.Context, b []byte, i int, r Req) (any, error)
```

⚠️ 代码里 `reflect.Method.Type.NumIn()` 包含 receiver，所以：
- 打印参数数量：`mtd.Type.NumIn() - 1`
- 第 i 个业务参数的类型是 `mtd.Type.In(i + 2)`（In0=recv, In1=ctx, In2=args[0]）
- 调用前必须校验 `len(msgReq.Args) ≤ NumIn()-2`，防止恶意多传导致 In 越界 panic

## ctx 里的 3 个固定 Key

| Key | 类型 | 服务端方法里 | 客户端方法里 |
|---|---|---|---|
| `sloth.BucketKey`  | `types.IBucket`       | ✅ 注入 | ❌ |
| `sloth.ChannelKey` | `bucket.IChannel` / `trpc.IChannel` | ✅ bucket 版（连入的 WS 句柄） | ✅ trpc 版（HandleCall 的本机 WS 句柄） |
| `sloth.HeaderKey`  | `message.Header`      | ✅ msgReq.Header 注入 | ✅ |

## SMap 设计与使用守则

[smap.go](file:///d:/var/o4p/github.com/sloth/v3/smap.go)

```go
smap := sloth.NewSMap()
id, err := smap.Reg("shop1", false)       // key 自动 TrimSpace；Reg(" shop1 ") 和 Reg("shop1") 同一键
id, ok  := smap.Get("shop1")              // 同上 TrimSpace 后查，不会因为前后空格或 \r\n 导致假 negative
smap.Del("\tshop1\r\n")                   // 同上 TrimSpace，实际删 "shop1"
for k, v := range smap.Range() { ... }    // iter.Seq2，内部持 RLock 遍历完再释放
```

⚠️ **默认 `idx--` 产负号 svrId（-1, -2, ...）**：目的是和 `id.NextId()` 生成的正号 UserId（普通终端）在 Bucket 里互斥，便于一眼区分「终端用户」和「注册服务节点」。如要正号改 `idx++`。

⚠️ **Get 里严禁重复嵌套调用 `Range()`**：外层 RLock 已持 + 内层 Range() 再 RLock 会重入死锁；新版本 Get 已做纯 O(1) 读 s.m[key]。

## Bucket / Room 分桶模型

```go
// 路由：userId → Buckets[CityHash32(fmt.Sprintf("%d",userId)) % N]
b := s.Bucket(userId)  // 返回 *bucket.Bucket；bucketIdx=0 时返回 nil，调用方必须判空

// 放入：userId(0=终端; <0=服务节点)；roomId = bucket.NoRoom(-1) 表示不进房间
err := b.Put(userId, roomId, token, ch)
// Put 的语义：
//   (1) ch 的旧连接 ch0 同 userId + 同房间（含双方都无房间）→ 仅更新 token，快返回
//   (2) ch0 已在不同房间 → 先 Room.DeleteChannel(ch0)
//   (3) userId 改变且旧值>0 → Close+从 chs 删除
//   (4) 写 ch.Room/ch.UserId/ch.Token → chs[userId] = ch → 目标 room.Put(ch)
```

## Fn 协议（decoder/fn）

帧结构（总 Header = 15 B）：

| Offset | Size | 字段 | 说明 |
|---|---|---|---|
| 0  | 2 | Magic  | `0x40 0x46` = ASCII `@F`。**不是 FN**。 |
| 2  | 1 | Action | `actions.ACTION_CALL` / `REPLY_SUCCESS` / `REPLY_ERROR` / `BROADCAST` |
| 3  | 8 | ID     | uint64 BigEndian，callId（由 `id.NextId(1)` 产生，snowflake 64bit） |
| 11 | 4 | Length | uint32 BigEndian，data 字节数；上限 1GB (`1<<30`) |
| 15 | N | Data   | 业务负载（`utils.Serialize(trpc.RpcCaller{Header,Method,Args,...})` 等） |

API：
```go
import fn "github.com/w6xian/sloth/v3/decoder/fn"

fn.IsFn(b)                       // O(1) 识别首 2B
buf,  err := fn.Encode(actions.ACTION_CALL, callId, payload) // 编码
f,    err := fn.Decode(buf)      // 解码；返回的 f.Data 是独立副本
err       := fn.Validate(buf)    // 纯验证
action, _ := fn.Action(buf)      // 读 action；len<15 返回 ErrFnInvalidFrame
id, _     := fn.Id(buf)          // 读 callId；len<11 返回 0
data      := fn.Data(buf)        // 读 payload；len≤15 返回 nil
```

哨兵错误（errors.Is 可识别）：
`ErrFnTooShort / ErrFnBadMagic / ErrFnLengthMismatch / ErrFnDataTooLarge / ErrFnNilFrame / ErrFnInvalidAction`

## 大报文分片（decoder/frame + nrpc/wsocket/utils.go）

```go
// 发送侧：超过 sliceSize 会拆成多帧，每帧带 2B 的流水号（00..99）
err := slicesBinarySend(getSliceName(), conn, bigPayload, sliceSize)

// 接收侧：readPump 收到分片帧（首字节是分片标志）→ accumulate 后调用 receiveMessage 合并
```

⚠️ **分片头 name 必须固定 2 字节**（`getSliceName()` 用 `fmt.Sprintf("%02d", ids)`），否则在 `[]byte(s.N)[:2]` 会越界：`slice bounds out of range [:2] with capacity 1`（当 ids 为 0-9 时字符串长度=1）。

## 约定与踩坑清单（修 bug 前必看）

| # | 约定 | 位置 |
|---|---|---|
| 1 | **Fn magic = `@F` = `[0x40, 0x46]`，不是 FN** | nrpc/wsocket/ws_server.go L333 / decoder/fn/fn.go |
| 2 | `mtd.Type.NumIn()` **含 receiver**，展示 `-1`；`msgReq.Args[i]` → `mtd.Type.In(i+2)`；调用前必须 `rArgsLen ≤ NumIn()-2` | connect.go |
| 3 | `callId = uint64(id.NextId(1))`（snowflake），**别用 decoder.NextId**；回包匹配 `fn.Id(raw)==callId` | nrpc/wsocket/channel_client.go |
| 4 | pool：`getCallObj / putCallObj / getBackObj / putBackObj` 是 **包级函数**，不是 `ch.xxx` | nrpc/wsocket/pool.go |
| 5 | `RpcCaller` 字段只有 `Header / Method / Data / Args / Channel / Error`；**没有 Id / Protocol / Action / Type** | types/trpc/rpc.go |
| 6 | `message.JsonBackObject` 字段只有 `Data / Error`；没有 Id/Action/Type | message/message.go |
| 7 | `actions.ACTION_REPLY` **常量不存在**；只有 `REPLY_SUCCESS=0x80` 和 `REPLY_ERROR=0x81` | actions/actions.go |
| 8 | nrpc 上回包必须是 Fn 帧：写 `rpcResult` 前要用 `fn.Encode(REPLY_*, callId, ...)` | ws_client.go / ws_server.go |
| 9 | **Connect.CallFunc 返回值编码用 `c.Encoder(data)`，不要 `any2byte(data)`**（不存在） | connect.go |
| 10 | `Bucket.Put`：写 map 必须 `Lock()`（旧版 RLock+delete 会 concurrent map writes）；`ch0.Room()` 可能 nil，读 `.Id` 要判空 | bucket/bucket.go |
| 11 | `WsServer.Bucket(userId)` 当 `bucketIdx=0` 返回 nil；`Channel/Room/Broadcast` 遍历要 nil-safe | nrpc/wsocket/ws_server.go |
| 12 | `Header / JsonCallObject / JsonBackObject / JsonValue` 是 pool；用完必须 Put（HandleCall 的 defer 已处理） | nrpc/wsocket/pool.go / message/header.go |
| 13 | `getSliceName()` 必须 `%02d`，保证 name=2 字节；否则 `[]byte(s.N)[:2]` 越界 | nrpc/wsocket/utils.go |
| 14 | `SMap.Reg/Get/Del` **都自动 TrimSpace(key)**；脏字符串（前缀 tab/UTF-8 NBSP `\xc2\xa0` / 回车换行）不会产生"存了找不到" | smap.go |
| 15 | SMap `idx--` 产负号 svrId；与 snowflake 正号 UserId 区分；如需正号改 `idx++` | smap.go L76 |
| 16 | WritePump / readPump **必须 defer recover()**，捕获下游 `[]byte(x)[:N]` 越界等 panic | nrpc/wsocket/ws_server.go / ws_client.go |
| 17 | `reflect.Method.Func.Call(...)` 调用参数表 **首元素是 receiver**；`funcArgs = [receiver, ctx, arg0, arg1...]`，顺序错了立刻 reflect panic | connect.go L411-L435 |
| 18 | UseProxyHandler 的调度前提：被代理的服务名必须在调用前先通过 `v1.Reg(name)` 登记到 SMap，否则一定返回 "service not registered" | examples/ws/main.go + examples/ws/client/main.go |

## 开发与测试

```bash
go build ./...                         # 全项目编译
go test  ./...                         # 跑所有用例（fn 协议 / pool / id rand）
go test  ./decoder/fn    -v -bench=.   # Fn 编解码 + 基准
go test  ./pool/...       -v           # 独立连接池
```

CI：`.github/workflows/go.yml`（fmt → vet → test ./...）。

## 版本

- Module：`github.com/w6xian/sloth/v3`
- Go 要求：`>= 1.25.3`（使用 `for i := range N`、iter.Seq2、现代 reflect）
