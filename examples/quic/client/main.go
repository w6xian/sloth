package main

// QUIC 传输的客户端样例（与 examples/tcp/client 对照看）。
//
// 差异只有一处：**QUIC 必须配置 TLS**（服务端同理）。
// 这里跳过证书校验是因为服务端样例用的是运行时生成的自签证书，
// 客户端拿不到可信链——生产环境请删掉 InsecureSkipVerify，
// 换成正常校验证书（或用内网 CA 的根证书做 RootCAs）。
//
// 其余与 TCP 客户端一致：Dial 建立连接后立刻返回，可以同步调用；
// 断线会自动重连（500ms → 30s 退避），但重连只恢复本地身份——服务端
// bucket 里的 channel 要业务在 OnReady 里重新 Sign/Reg 才会更新。
//
// 运行：先起 examples/quic 服务端，再跑本程序。

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"time"

	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/utils"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/types/auth"
	"github.com/w6xian/sloth/v4/types/trpc"
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
	newConnect := sloth.ClientConn(client,
		// ② TLS：QUIC 的必填项。
		//    InsecureSkipVerify 仅为配合服务端的自签证书，生产环境不要这么写：
		//    那等于放弃对服务端身份的校验，中间人可以随便冒充。
		//    正确做法是校验证书链，或指定内网 CA（RootCAs）。
		sloth.WithTLSConfig(&tls.Config{InsecureSkipVerify: true}))

	// ③ 注册客户端侧方法：服务端可以反过来调用它们（房间广播走的就是这条路）。
	//    不注册的话，服务端主动推下来的调用会找不到方法。
	if err := newConnect.Register("shop", &HelloService{}, ""); err != nil {
		sloth.Errorw(ctx, "register service failed", "err", err)
		return
	}

	// Header 是共享 map，建议在启动前一次性设好：
	// 运行期再改会与后台 goroutine 的读取形成数据竞争（需要时自行加锁）。
	client.Header.Set("APP_ID", "1")
	client.Header.Set("USER_ID", "1")

	// ④ 建立连接：QUIC 的 Dial 不阻塞，返回即已握手完成，可以直接发起调用。
	if err := newConnect.Dial(ctx, sloth.QUIC, "localhost:8992"); err != nil {
		sloth.Errorw(ctx, "dial failed", "err", err)
		return
	}
	defer newConnect.Close()

	// ⑤ 调用循环
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
