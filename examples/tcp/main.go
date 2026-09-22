package main

// TCP 传输的服务端样例（与 examples/ws 对照看）。
//
// 把它和 examples/ws/main.go 逐行对比，会发现**业务代码一模一样**，差异只有三处：
//   1. Listen(ctx, "tcp", addr) —— network 从 "ws" 换成 "tcp"；
//   2. 连接回调换成 option.WithTcpHandleMessage：ws 的回调每个方法都带
//      *http.Request，TCP 没有 HTTP 握手可传，用的是无 HTTP 依赖的那套钩子；
//   3. 没有 mux router / origin / uri path（HTTP 概念，TCP 用不上）。
//
// 这三处差异就是传输层抽象的全部内容：codec、dispatch、bucket、per-call 回包分发
// 全部共用，业务侧无感。
//
// 运行：
//   go run ./examples/tcp          # 服务端，监听 localhost:8991
//   go run ./examples/tcp/client   # 客户端，每 2 秒调一次
//
// 注意：TCP 端口上没有 HTTP 端点，curl 打不通（会因为没有合法的 FN 帧头被直接断连），
// 只能用 RPC 客户端连。

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
	"github.com/w6xian/sloth/v4/utils"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/sloth/v4/types/auth"
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

	// ① 日志配置：全局一次，必须在发起任何调用之前。
	//    debug 会额外打印连接建立/关闭与方法注册明细，生产建议 info（默认）。
	sloth.SetLogLevel("debug")
	// 日志进 ELK/Loki 时打开：单行 JSON，可直接按 level/trace/字段检索。
	// sloth.SetLogJSON(true)

	server := sloth.DefaultServer()
	drpc := sloth.ServerConn(server,
		// ② 调试服务独立端口：/debug/metrics（Prometheus）、/debug/pprof/*。
		//    与业务端口分离——这些端点没有鉴权，别挂在公网。
		sloth.WithDebugAddr("127.0.0.1:6061"))

	// ③ 注册服务：与 ws 完全一致。
	//    服务对象在所有连接间共享（Register 传的是同一个实例指针），而 RPC 是并发
	//    到达的，因此这里的计数必须用 atomic（直接自增普通字段会 data race）。
	if err := drpc.Register("v1", &HelloService{}, "metadata"); err != nil {
		sloth.Errorw(ctx, "register service failed", "err", err)
		return
	}

	// ④ 监听：只改 network 字符串即可换传输。
	//    底层是 FN 帧（15 字节定长头 + length 字节载荷）的字节流分帧，
	//    因此一次 Read 没读满一帧时能正确续读——大包不会被拆坏。
	if err := drpc.Listen(ctx, "tcp", "localhost:8991",
		option.WithTcpHandleMessage(&Handler{})); err != nil {
		sloth.Errorw(ctx, "listen failed", "err", err)
		return
	}

	// ⑤ Serve() 是阻塞的：它在所有 listener 退出后才返回，
	//    所以要放 goroutine，否则后面的 "started" 永远不会打印。
	serveErr := make(chan error, 1)
	go func() { serveErr <- drpc.Serve() }()

	sloth.Infow(ctx, "tcp server started",
		"tcp", "localhost:8991", "debug", "127.0.0.1:6061")

	// ⑥ 服务端主动推（房间广播）：客户端 Sign 进 room 后即可收到。
	//    需要时打开（客户端样例注册了 "shop" 服务，对应 shop.Test）：
	//
	// go func() {
	// 	ticker := time.NewTicker(5 * time.Second)
	// 	defer ticker.Stop()
	// 	for {
	// 		select {
	// 		case <-ctx.Done():
	// 			return
	// 		case <-ticker.C:
	// 			// CallRoom 的参数表较长，见 slots.Server.CallRoom 的签名
	// 			sloth.Infow(ctx, "broadcast tick", "hint", "use server.CallRoom(...) here")
	// 		}
	// 	}
	// }()

	// ⑦ 优雅关闭：收到中断信号，或 Serve 自己异常退出时收尾
	//    （Close 会关 listener 与调试服务，并等待监听 goroutine 退出；重复调用安全）
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

// Handler 实现 handler.TcpHandleMessage：TCP 的连接生命周期回调。
// 与 ws 版的区别：没有 *http.Request，取而代之的是对端地址。
// 通过 option.WithTcpHandleMessage(&Handler{}) 传给 Listen 才会生效。
type Handler struct{}

func (h *Handler) OnConnect(ctx context.Context, addr string) error {
	sloth.Infow(ctx, "client connected", "remote", addr)
	return nil
}

func (h *Handler) OnReady(ctx context.Context, s types.IBucket, ch bucket.IChannel) error {
	// 连接刚建立、还没 Sign：UserId() 此时是 0（Sign 之后才有值）
	sloth.Infow(ctx, "connection ready", "userId", ch.UserId())
	return nil
}

func (h *Handler) OnData(ctx context.Context, s types.IBucket, ch bucket.IChannel, msg []byte) error {
	// 大报文别整份打进日志：只打长度
	sloth.Infow(ctx, "data received", "size", len(msg))
	return nil
}

func (h *Handler) OnClose(ctx context.Context, s types.IBucket, ch bucket.IChannel) error {
	sloth.Infow(ctx, "connection closed", "userId", ch.UserId())
	return nil
}

func (h *Handler) OnError(ctx context.Context, s types.IBucket, ch bucket.IChannel, err error) error {
	// err 走 kv 而不是拼进 format：错误文本里的 % 不会被当成占位符
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

	// 结构化日志：传 ctx 才会带上 trace=xxx（库在 RPC 入口已注入），
	// 可与发起方客户端、同一条连接上的其它日志按 trace 直接对账。
	sloth.Infow(ctx, "hello test called",
		"id", id, "hasChannel", ch != nil, "headerKeys", len(header), "ab", ab)

	// 模拟偶发错误：让客户端看到错误回包的形态
	if id%5 == 1 {
		return nil, fmt.Errorf("error %d", id)
	}

	return map[string]string{
		"req":  "tcp server",
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
