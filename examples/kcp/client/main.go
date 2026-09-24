package main

// KCP 传输的客户端样例（与 examples/tcp/client 对照看）。
//
// 唯一实质差异：Dial 要多带一份 option.WithKCPConfig，且**必须与服务端一致**
// （加密方式、密钥、FEC 参数）。不一致时不会报错，只会一直调用超时——
// 遇到"连得上但没响应"，先核对两端的这份配置。
//
// 运行：先起 examples/kcp 服务端，再跑本程序。

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types/auth"
	"github.com/w6xian/sloth/v4/types/trpc"
	"github.com/w6xian/sloth/v4/utils"
	"github.com/w6xian/tlv"
)

// AB is a test struct
type AB struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

// kcpConfig 必须与服务端 examples/kcp 里的完全一致。
func kcpConfig() option.KCPConfig {
	cfg := option.FastKCPConfig()
	cfg.Crypt = option.KCPCryptAES
	cfg.Key = []byte("0123456789abcdef")
	cfg.DataShards = 10
	cfg.ParityShards = 3
	return cfg
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
	if err := newConnect.Register("shop", &HelloService{}, ""); err != nil {
		sloth.Errorw(ctx, "register service failed", "err", err)
		return
	}

	// Header 是共享 map，建议在启动前一次性设好：
	// 运行期再改会与后台 goroutine 的读取形成数据竞争。
	client.Header.Set("APP_ID", "1")
	client.Header.Set("USER_ID", "1")

	// ③ 建立连接：KCP 的 Dial 建好连接即返回，可以接着直接调用。
	//    配置在这里传给客户端，与服务端那份必须一致。
	if err := newConnect.Dial(ctx, "kcp", "localhost:8993",
		option.WithKCPConfig(kcpConfig())); err != nil {
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
	sloth.Infow(ctx, "client test called",
		"size", len(b), "headerKeys", len(header), "userId", ai.UserId)

	return utils.Serialize(map[string]string{
		"req":  "local test",
		"time": time.Now().Format("2006-01-02 15:04:05"),
	}), nil
}
