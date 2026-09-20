package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/internal/utils"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/slots"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/sloth/v4/types/auth"
	"github.com/w6xian/tlv"
)

var smap *sloth.SMap

func init() {
	smap = sloth.NewSMap()
}

// main entry point for the WebSocket server
func main() {
	// Create a context with a cancel function
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// ① 日志配置：全局一次，必须在发起任何调用之前。
	//    debug 会额外打印方法注册明细，生产建议 info（默认），避免刷屏。
	sloth.SetLogLevel("debug")
	// 日志进 ELK/Loki 时打开：单行 JSON，可直接按 level/trace/字段检索。
	// sloth.SetLogJSON(true)

	server := sloth.DefaultServer()
	drpc := sloth.ServerConn(server,
		// ② 调试服务独立端口：/debug/metrics（Prometheus）、/debug/pprof/*、/debug/vars。
		//    与业务端口分离，只需内网放行——这些端点没有鉴权，别挂在公网。
		//    若必须共用业务端口，改用 option.WithDebugHandler() 传给 Listen 即可。
		sloth.WithDebugAddr("127.0.0.1:6060"),
		sloth.WithConnectProxy(func(ctx context.Context, service string) (int64, error) {
			node, err := sloth.GetNode(service)
			if err != nil {
				return 0, err
			}
			svrId, ok := smap.Get(node.Service)
			if !ok {
				return 0, fmt.Errorf("service %s not registered", node.Service)
			}
			return svrId, nil
		}))
	r := mux.NewRouter()
	// Register services
	if err := drpc.Register("v1", &HelloService{}, "metadata"); err != nil {
		sloth.Errorw(ctx, "register service failed", "err", err)
		return
	}
	if err := drpc.Listen(ctx, "ws", "localhost:8990",
		option.WithRouter(r, "/ws"),
		option.WithOrigin("*", "localhost:8000"),
		option.WithServerHandleMessage(&Handler{})); err != nil {
		sloth.Errorw(ctx, "listen failed", "err", err)
		return
	}
	// 重复操作，可以sloth.WithConnectProxy()来代替
	drpc.UseProxyHandler(func(ctx context.Context, service string) (int64, error) {
		node, err := sloth.GetNode(service)
		if err != nil {
			return 0, err
		}
		svrId, ok := smap.Get(node.Service)
		if !ok {
			return 0, fmt.Errorf("service %s not registered", node.Service)
		}
		return svrId, nil
	})

	// go func() {
	// 	for {
	// 		time.Sleep(time.Millisecond * 2000)
	// 		// 主动调用也要传 ctx：库会为这次调用生成/沿用 trace id 并随请求发给对端
	// 		callCtx, trace := sloth.EnsureTrace(ctx)
	// 		rst, err := server.CallRoom(callCtx, 1, "shop.Test", nil, []byte{1}, 655360, true, &AB{A: 1, B: 2}, 'a', 12345)
	// 		if err != nil {
	// 			sloth.Errorw(callCtx, "room call failed", "trace", trace, "err", err)
	// 			continue
	// 		}
	// 		sloth.Infow(callCtx, "room call ok", "trace", trace, "result", string(rst))
	// 	}
	// }()

	// ③ Serve() 是阻塞的：它在所有 listener 退出后才返回。
	//    原示例把它写在主流程末尾，导致后面的 "listening" 永远不会打印。
	serveErr := make(chan error, 1)
	go func() { serveErr <- drpc.Serve() }()

	sloth.Infow(ctx, "server started",
		"ws", "localhost:8990", "debug", "127.0.0.1:6060", "logLevel", "debug")

	// ④ 优雅关闭：收到中断信号，或 Serve 自己异常退出时收尾
	//    （Close 会关闭 listener 与调试服务；重复调用安全）
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

// Hello represents a simple message structure
type Hello struct {
	Name string `json:"name"`
}

type Handler struct {
	slots.Server
}

func (h *Handler) OnConnect(ctx context.Context, r *http.Request) error {
	h.Server.OnConnect(ctx, r)
	// 握手阶段库已为这条连接生成 trace id 并放进 ctx：
	// 这里传 ctx，日志会带上 trace=xxx，与后续该连接上的读写日志天然串联。
	sloth.Infow(ctx, "client connected", "remote", r.RemoteAddr, "uri", r.RequestURI)
	return nil
}

// HelloReq represents the request for Hello service
type HelloReq struct {
	Name string `json:"name"`
}

// HelloService implements the RPC service
//
// 注意：服务对象在**所有连接间共享**（Register 传的是同一个实例指针），
// 而 RPC 是并发到达的，因此这里的可变状态必须原子/加锁保护；
// 直接自增普通字段会 data race（-race 能直接抓到）。
type HelloService struct {
	reqCount atomic.Int64
}

// AB is a test struct
type AB struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
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
	// 注意：header 里常带 token 等凭据，生产别整份打印，只打白名单字段。
	sloth.Infow(ctx, "hello test called",
		"id", id, "hasChannel", ch != nil, "headerKeys", len(header), "ab", ab)

	// Simulate error
	if id%5 == 1 {
		return nil, fmt.Errorf("error %d", id)
	}

	return map[string]string{
		"req":  "server 1",
		"time": time.Now().Format("2006-01-02 15:04:05"),
	}, nil
}

// Sign handles user signing/authentication
func (h *HelloService) WebSign(ctx context.Context, data []byte) ([]byte, error) {
	h.reqCount.Add(1)

	// Get channel from context
	ch, ok := ctx.Value(sloth.ChannelKey).(bucket.IChannel)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}

	// Get bucket server from context
	svr, ok := ctx.Value(sloth.BucketKey).(types.IBucket)
	if !ok {
		return nil, fmt.Errorf("bucket not found")
	}

	// Simulate auth info extraction
	auth := auth.AuthInfo{
		UserId: 2,
		RoomId: 1,
		Token:  "token_123", // Added fake token
		Ts:     time.Now().Unix(),
	}

	// Register session in bucket
	svr.Bucket(auth.UserId).Put(auth.UserId, auth.RoomId, auth.Token, ch)
	return tlv.Json(auth), nil
}

// Sign handles user signing/authentication
func (h *HelloService) Sign(ctx context.Context, data []byte) ([]byte, error) {
	h.reqCount.Add(1)

	// Get channel from context
	ch, ok := ctx.Value(sloth.ChannelKey).(bucket.IChannel)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}

	// Get bucket server from context
	svr, ok := ctx.Value(sloth.BucketKey).(types.IBucket)
	if !ok {
		return nil, fmt.Errorf("bucket not found")
	}

	// Simulate auth info extraction
	auth := auth.AuthInfo{
		UserId: 2,
		RoomId: 1,
		Token:  "token_123", // Added fake token
		Ts:     time.Now().Unix(),
	}

	// Register session in bucket
	svr.Bucket(auth.UserId).Put(auth.UserId, auth.RoomId, auth.Token, ch)
	return tlv.Json(auth), nil
}

func (h *HelloService) Reg(ctx context.Context, name string) ([]byte, error) {
	h.reqCount.Add(1)

	// Get channel from context
	ch, ok := ctx.Value(sloth.ChannelKey).(bucket.IChannel)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}
	// Get bucket server from context
	svr, ok := ctx.Value(sloth.BucketKey).(types.IBucket)
	if !ok {
		return nil, fmt.Errorf("bucket not found")
	}

	svrId, err := smap.Reg(name, false)
	if err != nil {
		return nil, err
	}

	// Simulate auth info extraction
	auth := auth.AuthInfo{
		UserId: svrId,
		RoomId: -1,
		Token:  "token_123", // Added fake token
	}
	// Register session in bucket
	svr.Bucket(auth.UserId).Put(auth.UserId, auth.RoomId, auth.Token, ch)
	return tlv.Json(auth), nil
}

// TestByte tests various parameter types
func (h *HelloService) TestByte(ctx context.Context, b []byte, i int, req HelloReq, resp *Hello, str *string, bytes *[]byte, strs []string, strsptr *[]string) (any, error) {
	id := h.reqCount.Add(1)

	// 指针入参由库在解码阶段分配，正常不为 nil；
	// 业务代码对外部可控的指针入参仍建议先判空，避免畸形报文打成 panic。
	sloth.Infow(ctx, "test byte called",
		"id", id, "b", string(b), "i", i, "req", req, "resp", resp,
		"str", *str, "bytesLen", len(*bytes), "strs", strs, "strsptr", *strsptr)

	if id%5 == 1 {
		return nil, fmt.Errorf("error %d", id)
	}

	return map[string]string{
		"req":  "server 1",
		"time": time.Now().Format("2006-01-02 15:04:05"),
	}, nil
}

// Login handles login requests
func (h *HelloService) Login(ctx context.Context, data []byte) ([]byte, error) {
	return utils.Serialize(map[string]string{
		"user_id": "2",
		"time":    time.Now().Format("2006-01-02 15:04:05"),
	}), nil
}
