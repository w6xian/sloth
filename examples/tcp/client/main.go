package main

// TCP 传输的客户端样例（与 examples/ws/client 对照看）。
//
// 与 ws 客户端的差异只有一处可见行为：
//   - ws 的 Dial 内部会一直跑到连接断开，所以 ws 样例必须 `go newConnect.Dial(...)`；
//   - TCP 的 Dial 建立连接后立刻返回（读写在后台 goroutine），可以同步调用。
//
// 另外两点现状（P3 遗留，样例里因此不演示）：
//   1. TCP 还没有断线重连——ws 有 KeepAlive 与 runRelogin，而"重连后是否要重新
//      Sign、身份是否重建"是个语义问题，未定不动；
//   2. 客户端侧的连接回调（option.WithClientHandleMessage）每个方法都带
//      *http.Response，TCP 没有 HTTP 握手，传了会被忽略——所以这里不传。
//
// 运行：先起 examples/tcp 服务端，再跑本程序。

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/w6xian/sloth/v3"
	"github.com/w6xian/sloth/v3/internal/utils"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/types/auth"
	"github.com/w6xian/sloth/v3/types/trpc"
	"github.com/w6xian/tlv"
)

// AB is a test struct
type AB struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ① 日志配置：debug 会打印连接/重连/方法注册明细，生产用 info（默认）
	sloth.SetLogLevel("debug")
	// sloth.SetLogJSON(true)

	client := sloth.DefaultClient()
	newConnect := sloth.ClientConn(client)

	// ② 注册客户端侧方法：服务端可以反过来调用它们（房间广播走的就是这条路）。
	//    不注册的话，服务端主动推下来的调用会找不到方法。
	if err := newConnect.Register("shop", &HelloService{}, ""); err != nil {
		sloth.Errorw(ctx, "register service failed", "err", err)
		return
	}

	// Header 是共享 map，建议在启动前一次性设好：
	// 运行期再改会与后台 goroutine 的读取形成数据竞争（需要时自行加锁）。
	client.Header.Set("APP_ID", "1")
	client.Header.Set("USER_ID", "1")

	// ③ 建立连接：TCP 的 Dial 不阻塞，返回即已连上，可以直接发起调用。
	if err := newConnect.Dial(ctx, "tcp", "localhost:8991"); err != nil {
		sloth.Errorw(ctx, "dial failed", "err", err)
		return
	}
	defer newConnect.Close()

	// ④ 调用循环
	for {
		// 用 select 而不是 time.Sleep：ctx 取消时能立刻返回，不留悬挂 goroutine
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}

		// 未登录则先签名：Sign 会把这条连接登记进服务端的 bucket，
		// 登记之后服务端才能按 userId / roomId 主动把消息推给我们。
		if client.UserId == 0 {
			// 调用前派生一条 trace：库会把它随请求发给服务端，
			// 服务端日志沿用同一个 id，两端可直接对账。
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
			continue
		}

		// 带 header 的调用：header 复用同一个 map 即可，库内部会 Clone，
		// 不必每次调用重建一份。
		callCtx, _ := sloth.EnsureTrace(ctx)
		hdr := message.Header{
			"APP_ID":  "header_app_id",
			"USER_ID": "1",
		}
		data, err := client.CallWithHeader(callCtx, hdr, "v1.Test", &AB{A: 1, B: 2})
		sloth.Infow(callCtx, "v1.Test result", "data", string(data), "err", err)
	}
}

// HelloService 客户端侧方法：被服务端远程调用时走到这里。
type HelloService struct{}

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
	// 这里被服务端远程调用：ctx 上的 trace id 来自调用方，
	// 因此这条日志与发起方日志同 trace，可跨进程直接串联。
	sloth.Infow(ctx, "client test called",
		"size", len(b), "headerKeys", len(header), "userId", ai.UserId)

	return utils.Serialize(map[string]string{
		"req":  "local test",
		"time": time.Now().Format("2006-01-02 15:04:05"),
	}), nil
}
