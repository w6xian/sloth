package main

// KCP 传输的服务端样例（与 examples/tcp 对照看）。
//
// 与 TCP 样例的差异只有两处：
//   1. Listen(ctx, "kcp", addr, option.WithKCPConfig(cfg))——底层是 UDP，
//      不再经过 net.Listen("tcp")；
//   2. 多了一个 option.WithKCPConfig：KCP 把加密做在传输层，
//      加密方式与 FEC 参数在这里定，**两端必须完全一致**。
//
// 业务代码、回调钩子、bucket、per-call 回包分发与 TCP 完全一致——
// 传输层抽象到这里才算真的通用：换传输不用改业务。
//
// 运行：
//   go run ./examples/kcp          # 服务端，监听 localhost:8993（UDP）
//   go run ./examples/kcp/client   # 客户端，每 2 秒调一次
//
// 注意：这是 UDP 端口，curl 打不通，只能用 RPC 客户端连。

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/sloth/v4/types/auth"
	"github.com/w6xian/sloth/v4/utils"
	"github.com/w6xian/tlv"
)

// AB is a test struct
type AB struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

// kcpConfig 两端**必须一致**的 KCP 参数。
//
// 不一致不会报"配置不匹配"，而是解不开对端的包：连接建得起来、
// 调用一直超时，日志上什么错都没有。改这里就要同步改客户端。
func kcpConfig() option.KCPConfig {
	cfg := option.FastKCPConfig() // 低延迟预设：nodelay + 大窗口 + 流模式
	cfg.Crypt = option.KCPCryptAES
	cfg.Key = []byte("0123456789abcdef") // AES 要求 16/24/32 字节
	cfg.DataShards = 10                  // FEC：每 10 个包带 3 个冗余，
	cfg.ParityShards = 3                 // 用带宽换丢包下的延迟
	return cfg
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ① 日志配置：全局一次，必须在发起任何调用之前。
	sloth.SetLogLevel("debug")
	// sloth.SetLogJSON(true)

	server := sloth.DefaultServer()
	drpc := sloth.ServerConn(server,
		// ② 调试服务独立端口：/debug/metrics（Prometheus）、/debug/pprof/*
		sloth.WithDebugAddr("127.0.0.1:6063"))

	// ③ 注册服务：与 ws / tcp / quic 完全一致。
	//    服务对象在所有连接间共享，RPC 并发到达，计数要用 atomic。
	if err := drpc.Register("v1", &HelloService{}, "metadata"); err != nil {
		sloth.Errorw(ctx, "register service failed", "err", err)
		return
	}

	// ④ 监听 KCP：参数在建监听器时固定，之后改不了。
	if err := drpc.Listen(ctx, "kcp", "localhost:8993",
		option.WithKCPConfig(kcpConfig()),
		option.WithTcpHandleMessage(&Handler{})); err != nil {
		sloth.Errorw(ctx, "listen failed", "err", err)
		return
	}

	// ⑤ Serve() 阻塞，放 goroutine。
	serveErr := make(chan error, 1)
	go func() { serveErr <- drpc.Serve() }()

	sloth.Infow(ctx, "kcp server started", "kcp", "localhost:8993", "debug", "127.0.0.1:6063")

	// ⑥ 优雅关闭
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-serveErr:
		if err != nil {
			sloth.Errorw(ctx, "serve exited with error", "err", err)
		}
	case s := <-sig:
		sloth.Infow(ctx, "shutdown signal received", "signal", s.String())
	}
	if err := drpc.Close(); err != nil {
		sloth.Errorw(ctx, "close server failed", "err", err)
	}
}

// Handler 实现 handler.TcpHandleMessage：非 HTTP 传输的连接生命周期回调。
// KCP 与 TCP / QUIC 共用同一套钩子——它描述的是连接的生命周期，
// 与底下跑的是什么传输无关。
type Handler struct{}

func (h *Handler) OnConnect(ctx context.Context, addr string) error {
	sloth.Infow(ctx, "client connected", "remote", addr)
	return nil
}

func (h *Handler) OnReady(ctx context.Context, s types.IBucket, ch bucket.IChannel) error {
	sloth.Infow(ctx, "connection ready", "userId", ch.UserId())
	return nil
}

func (h *Handler) OnData(ctx context.Context, s types.IBucket, ch bucket.IChannel, msg []byte) error {
	sloth.Infow(ctx, "data received", "size", len(msg))
	return nil
}

func (h *Handler) OnClose(ctx context.Context, s types.IBucket, ch bucket.IChannel) error {
	sloth.Infow(ctx, "connection closed", "userId", ch.UserId())
	return nil
}

func (h *Handler) OnError(ctx context.Context, s types.IBucket, ch bucket.IChannel, err error) error {
	sloth.Errorw(ctx, "connection error", "userId", ch.UserId(), "err", err)
	return nil
}

// HelloService 业务服务对象：所有连接共享同一个实例，可变状态需原子/加锁保护。
type HelloService struct {
	reqCount atomic.Int64
}

// Test is a sample RPC method
func (h *HelloService) Test(ctx context.Context, ab *AB) (any, error) {
	id := h.reqCount.Add(1)

	// 从 ctx 取值一律用 GetXxx（返回值 + error）：
	// 直接 ctx.Value(key).(T) 断言在值缺失时会 panic，会把整条连接的读循环打掉。
	ch, err := sloth.GetChannel(ctx)
	if err != nil {
		sloth.Warnw(ctx, "channel missing in ctx", "err", err)
	}
	header, err := sloth.GetHeader(ctx)
	if err != nil {
		sloth.Warnw(ctx, "header missing in ctx", "err", err)
	}

	sloth.Infow(ctx, "hello test called",
		"id", id, "hasChannel", ch != nil, "headerKeys", len(header), "ab", ab)

	// 模拟偶发错误：让客户端看到错误回包的形态
	if id%5 == 1 {
		return nil, fmt.Errorf("error %d", id)
	}

	return map[string]string{
		"req":  "kcp server",
		"time": time.Now().Format("2006-01-02 15:04:05"),
	}, nil
}

// Sign 把这条连接登记进 bucket：登记之后服务端才能按 userId / roomId 主动推消息。
func (h *HelloService) Sign(ctx context.Context, data []byte) ([]byte, error) {
	h.reqCount.Add(1)

	ch, ok := ctx.Value(sloth.ChannelKey).(bucket.IChannel)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}
	svr, ok := ctx.Value(sloth.BucketKey).(types.IBucket)
	if !ok {
		return nil, fmt.Errorf("bucket not found")
	}

	// 真实场景里这些信息来自鉴权结果，这里用固定值示意
	auth := auth.AuthInfo{
		UserId: 2,
		RoomId: 1,
		Token:  "token_123",
		Ts:     time.Now().Unix(),
	}
	svr.Bucket(auth.UserId).Put(auth.UserId, auth.RoomId, auth.Token, ch)
	return tlv.Json(auth), nil
}

// Login handles login requests
func (h *HelloService) Login(ctx context.Context, data []byte) ([]byte, error) {
	return utils.Serialize(map[string]string{
		"user_id": "2",
		"time":    time.Now().Format("2006-01-02 15:04:05"),
	}), nil
}
