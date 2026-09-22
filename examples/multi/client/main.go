package main

// 多协议客户端样例：同一个进程里用三条协议（ws / tcp / quic）各连一条。
//
// 为什么要建三个 Connect：Dial 是把连接挂在当前 Connect 的 ServerRpc 上，
// 一个 Connect 同时只能持有一条对外连接（第二次 Dial 会报
// "dial not allowed: server already listening"）。所以三条协议 = 三个 Connect，
// 各自注册自己的客户端方法、各自 Sign。
//
// 服务端侧的样例见 examples/multi：它一个进程同时开三条协议，
// 三条链路共用同一份服务注册表（所以这里三条连接调的都是 v1.*）。
//
// 运行：先起 examples/multi 服务端，再跑本程序。

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/w6xian/sloth/v4"
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

// 协议标识不用业务传：库会在固定头里自动带上 sloth-protocol（Dial 时记下的
// 协议名），服务端用 sloth.GetProtocol(ctx) 读取后分配不同的 userId。

// endpoint 一条待连接的目标：协议名 + 地址 + 该协议需要的连接选项。
type endpoint struct {
	name    string
	network string
	addr    string
	opts    []sloth.ConnOption
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sloth.SetLogLevel("debug")

	// ws / tcp 明文即可；quic 必须带 TLS（服务端样例用的是自签证书，
	// 所以这里跳过校验——生产环境请换成正常校验证书链）。
	endpoints := []endpoint{
		{name: "ws", network: sloth.WS, addr: "localhost:8990"},
		{name: "tcp", network: sloth.TCP, addr: "localhost:8991"},
		{
			name: "quic", network: sloth.QUIC, addr: "localhost:8992",
			opts: []sloth.ConnOption{sloth.WithTLSConfig(&tls.Config{InsecureSkipVerify: true})},
		},
	}

	for _, ep := range endpoints {
		// 每条链路一个 goroutine：ws 的 Dial 是阻塞的（内部跑到连接断开），
		// 另两条 Dial 立即返回，统一放 goroutine 里最省心。
		go ep.serve(ctx, ep.name, ep.network, ep.addr, ep.opts...)
	}

	sloth.Infow(ctx, "multi client started", "endpoints", len(endpoints))
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	s := <-sig
	sloth.Infow(ctx, "shutdown signal received", "signal", s.String())
	cancel()
}

// serve 建立一条链路并循环调用（每个 endpoint 在自己的 goroutine 里跑）。
func (e endpoint) serve(ctx context.Context, name, network, addr string, opts ...sloth.ConnOption) {
	client := sloth.DefaultClient()
	newConnect := sloth.ClientConn(client, opts...)

	// 注册客户端侧方法：服务端主动推下来的调用会走到这里（房间广播走的就是这条路）。
	// 不注册的话，服务端 CallRoom 过来会找不到方法。
	if err := newConnect.Register("shop", &ClientService{proto: name}, ""); err != nil {
		sloth.Errorw(ctx, "register service failed", "proto", name, "err", err)
		return
	}
	client.Header.Set("APP_ID", "1")
	client.Header.Set("PROTO", name)

	// ws 的 Dial 会一直跑到连接断开，所以放后台；tcp / quic 立即返回。
	dialErr := make(chan error, 1)
	go func() { dialErr <- newConnect.Dial(ctx, network, addr) }()

	select {
	case err := <-dialErr:
		if err != nil {
			sloth.Errorw(ctx, "dial failed", "proto", name, "addr", addr, "err", err)
			return
		}
	case <-time.After(2 * time.Second):
		// ws：连接已建立、Dial 仍在阻塞（正常）；继续往下走
	}

	defer newConnect.Close()

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}

		// 未登录则先签名：Sign 会把这条连接登记进服务端的 bucket，
		// 登记之后服务端才能按 userId / roomId 主动把消息推给我们。
		if client.UserId == 0 {
			callCtx, _ := sloth.EnsureTrace(ctx)
			data, err := client.Call(callCtx, "v1.Sign", []byte(name))
			if err != nil {
				sloth.Errorw(callCtx, "sign failed", "proto", name, "err", err)
				continue
			}
			ai := &auth.AuthInfo{}
			if err := json.Unmarshal(tlv.Value(data), ai); err != nil {
				sloth.Errorw(callCtx, "sign response decode failed", "proto", name, "err", err)
				continue
			}
			if err := client.SetAuthInfo(ai); err != nil {
				sloth.Errorw(callCtx, "set auth info failed", "proto", name, "err", err)
				continue
			}
			sloth.Infow(callCtx, "sign success", "proto", name, "userId", ai.UserId, "roomId", ai.RoomId)
			continue
		}

		callCtx, _ := sloth.EnsureTrace(ctx)
		hdr := message.Header{"APP_ID": "header_app_id", "PROTO": name}
		data, err := client.CallWithHeader(callCtx, hdr, "v1.Test", &AB{A: 1, B: 2})
		sloth.Infow(callCtx, "v1.Test result", "proto", name, "data", string(data), "err", err)
	}
}

// ClientService 客户端侧方法：被服务端远程调用时走到这里。
type ClientService struct {
	proto string // 记录这条客户端方法属于哪条协议，便于在日志里区分
}

// Test is a sample client-side method：服务端每 5 秒的 CallRoom 会打到这里。
func (h *ClientService) Test(ctx context.Context, b []byte) ([]byte, error) {
	// 非安全断言 ctx.Value(k).(T) 在值缺失时会 panic，把整条连接的读循环打掉；
	// 用带 ok 的形式，缺失时按业务错误处理。
	// 注意：这里**不读** ch.GetAuthInfo()——stream.Channel（tcp / quic）的
	// GetAuthInfo 是服务端语义（身份由服务端 bucket.Put 写入），客户端侧连接
	// 上没有身份，调它只会得到 "user id is 0"。客户端自己的身份看 client.UserId。
	_, ok := ctx.Value(sloth.ChannelKey).(trpc.IChannel)
	if !ok {
		sloth.Warnw(ctx, "channel missing in ctx", "proto", h.proto, "size", len(b))
	}
	// 这条日志的 trace 来自服务端那次 CallRoom，可与服务端日志直接对账。
	sloth.Infow(ctx, "push from server received", "proto", h.proto, "payload", string(b))

	return fmt.Appendf(nil, "%s got it", h.proto), nil
}
