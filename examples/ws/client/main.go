package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/internal/utils"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/sloth/v4/types/auth"
	"github.com/w6xian/sloth/v4/types/trpc"
	"github.com/w6xian/tlv"

	"github.com/gorilla/websocket"
)

// AB is a test struct
type AB struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

// main entry point for the WebSocket client
func main() {
	runtime := context.Background()
	ctx, cancel := context.WithCancel(runtime)
	defer cancel()

	// ① 日志配置：debug 会打印连接/重连/方法注册明细，生产用 info（默认）
	sloth.SetLogLevel("debug")
	// 日志进 ELK/Loki 时打开：单行 JSON，可直接解析 trace/level/字段
	// sloth.SetLogJSON(true)

	client := sloth.DefaultClient()
	newConnect := sloth.ClientConn(client)
	if err := newConnect.Register("shop", &HelloService{}, ""); err != nil {
		sloth.Errorw(ctx, "register service failed", "err", err)
		return
	}

	// Header 是共享 map，建议在启动前一次性设好：
	// 运行期再改会与后台拨号 goroutine 的读取形成数据竞争（需要时自行加锁）。
	client.Header.Set("APP_ID", "1")
	client.Header.Set("USER_ID", "1")

	// ② 启动连接：Dial 自带重连（KeepAlive），失败/重连次数见指标
	//    sloth_client_dial_errors_total / sloth_client_reconnects_total
	go func() {
		if err := newConnect.Dial(ctx, "ws", "localhost:8990",
			option.WithClientHandleMessage(&Handler{})); err != nil {
			sloth.Errorw(ctx, "dial exited", "err", err)
		}
	}()

	// ③ 调用循环
	for {
		// 用 select 而不是 time.Sleep：ctx 取消时能立刻返回，不留悬挂 goroutine
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}

		// 未登录则先签名
		if client.UserId == 0 {
			// 调用前派生一条 trace：库会把它写进 X-Trace-Id 头发给服务端，
			// 服务端日志沿用同一个 id，两端可直接对账。
			// trace 已挂在 callCtx 上，日志不必再手动写一遍；
			// 若要透传给外部系统（如回执、网关），用 sloth.TraceID(callCtx) 取回。
			callCtx, _ := sloth.EnsureTrace(ctx)
			data, err := client.Call(callCtx, "v1.Sign", []byte("sign"))
			if err != nil {
				sloth.Errorw(callCtx, "sign failed", "err", err)
				continue
			}
			ai := &auth.AuthInfo{}
			if err := json.Unmarshal(tlv.Value(data), ai); err != nil {
				sloth.Errorw(callCtx, "sign response decode failed", "err", err)
				continue
			}
			client.SetAuthInfo(ai)
			sloth.Infow(callCtx, "sign success", "userId", ai.UserId, "roomId", ai.RoomId)
		}

		// 带 header 的调用：header 复用同一个 map 即可，库内部会 Clone，
		// 不必每次调用重建一份（原示例每次新建 3 个 map）。
		// 这一轮业务共用一个 trace，便于把同批发起的调用聚合起来看；
		// 想逐调用区分，就给每次调用各调一次 sloth.EnsureTrace。
		callCtx, _ := sloth.EnsureTrace(ctx)
		hdr := message.Header{
			"APP_ID":  "header_app_id",
			"USER_ID": "1",
		}

		data, err := client.CallWithHeader(callCtx, hdr, "v1.Test", &AB{A: 1, B: 2})
		sloth.Infow(callCtx, "v1.Test result", "data", string(data), "err", err)

		data1, err := client.CallWithHeader(callCtx, hdr, "shop1.Test1", []byte("abc"))
		sloth.Infow(callCtx, "shop1.Test1 result", "data", string(data1), "err", err)

		data2, err := client.CallWithHeader(callCtx, hdr, "shop2.Test1", []byte("abc"))
		sloth.Infow(callCtx, "shop2.Test1 result", "data", string(data2), "err", err)
	}
}

// IotSignReq represents IoT signing request
type IotSignReq struct {
	Code  string `json:"code"`
	Token string `json:"token"`
}

// HelloReq represents hello request
type HelloReq struct {
	Name string `json:"name"`
}

// Handler 实现 handler.IClientHandleMessage：客户端连接生命周期回调。
// 通过 option.WithClientHandleMessage(&Handler{}) 传给 Dial 才会生效
// （原示例定义了这套回调却从未挂载，属于死代码）。
//
// 每个回调都收到带连接级 trace 的 ctx，日志可直接与该连接上的其它日志串联。
type Handler struct{}

// OnConnect 收到握手响应后触发：可在这里校验鉴权，返回 error 会断开连接。
func (h *Handler) OnConnect(ctx context.Context, resp *http.Response) error {
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	sloth.Infow(ctx, "handshake response", "status", status)
	return nil
}

// OnReady 连接就绪（已可收发）时触发。
func (h *Handler) OnReady(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo) error {
	sloth.Infow(ctx, "connection ready", "userId", ch.GetUserId())
	return nil
}

// OnClose is called when connection is closed
func (h *Handler) OnClose(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo) error {
	sloth.Infow(ctx, "connection closed", "userId", ch.GetUserId())
	return nil
}

// OnData handles received messages
func (h *Handler) OnData(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo, msgType int, message []byte) error {
	if msgType == websocket.TextMessage {
		// 大报文别整份打进日志：只打长度，内容按需截断
		sloth.Infow(ctx, "text message received", "size", len(message))
	}
	return nil
}

// OnError handles errors
func (h *Handler) OnError(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo, err error) error {
	// err 走 kv 而不是拼进 format：错误文本里的 % 不会被当成占位符
	sloth.Errorw(ctx, "connection error", "userId", ch.GetUserId(), "err", err)
	return nil
}

// HelloService implements client-side service methods
type HelloService struct {
}

// Test is a sample client-side method
func (h *HelloService) Test(ctx context.Context, b []byte) ([]byte, error) {
	// 非安全断言 ctx.Value(k).(T) 在值缺失时会 panic，把整条连接的读循环打掉；
	// 用带 ok 的形式，缺失时按业务错误处理。
	ch, ok := ctx.Value(sloth.ChannelKey).(trpc.IChannel)
	if !ok || ch == nil {
		sloth.Errorw(ctx, "channel missing in ctx", "size", len(b))
		return nil, errors.New("channel not found")
	}
	header, _ := ctx.Value(sloth.HeaderKey).(message.Header)

	ai, err := ch.GetAuthInfo()
	if err != nil {
		sloth.Errorw(ctx, "get auth info failed", "err", err)
		return nil, err
	}
	// 这里被服务端远程调用：ctx 上的 trace id 来自调用方（X-Trace-Id），
	// 因此这条日志与发起方日志同 trace，可跨进程直接串联。
	sloth.Infow(ctx, "client test called",
		"size", len(b), "headerKeys", len(header), "userId", ai.UserId)

	return utils.Serialize(map[string]string{"req": "local test", "time": time.Now().Format("2006-01-02 15:04:05")}), nil
}

// Hello struct
type Hello struct {
	Name string `json:"name"`
}
