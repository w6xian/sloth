package main

// 媒体代理的服务端样例（方案③：隧道里跑 HTTP）。
//
// 思路：不定义任何新的帧或协议，也不自己实现 Range / MIME / 304。
// 一次 RPC 承载一个完整的 HTTP 报文——
//
//	浏览器 ──HTTP──▶ 网关（本文件）
//	                  ① 请求报文序列化（req.Write）
//	                  ② server.Call(ctx, userId, "http.Do", raw)
//	                                  ──────▶ 客户端还原请求、打给本地 HTTP 服务
//	                                  ◀────── 响应报文（裸字节，fn 帧直接承载）
//	                  ③ 解析响应报文、回写给浏览器
//	浏览器 ◀──HTTP── 网关
//
// 于是 Range / 206 / Content-Type / MIME / 304 全部由客户端本地的
// http.FileServer 提供，网关只负责搬运，一行都不用自己实现。
//
// 尺寸上的两个前提（决定了这条路走得通）：
//   - 请求报文走**入参**，受 ag 限制单参数 ≤ 65535 字节 → 只放请求头，不放 body；
//   - 响应报文走**返回值**，是 fn 帧裸 payload，上限 1GB → 一个 206 分片绰绰有余。
//
// 运行：
//
//	go run ./examples/media/server   # RPC 8991 + 网关 8080
//	go run ./examples/media/client    # 连上来，把本地目录暴露成 /m/media/
//	curl.exe -r 0-99 http://localhost:8080/m/media/sample.bin -D -

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/sloth/v4/types/auth"
	"github.com/w6xian/tlv"
)

const (
	rpcAddr     = "localhost:8991"
	gatewayAddr = "localhost:8080"
)

// callTimeout 单次转发调用的超时。库默认 defaultTimeout 是 10s，
// 这里放宽：慢链路 + 大分片时更稳，且浏览器断连会经 r.Context() 提前取消。
const callTimeout = 30 * time.Second

// maxReqBytes 请求报文的上限。入参走 ag 编码，单参数上限 65535（ArgumentMaxDataSize），
// 留一点余量给报文头本身。
const maxReqBytes = 60000

// hopHeaders hop-by-hop 头：只对单条连接有效，不能跨跳转发
// （转发了会破坏两端的连接语义，例如 Transfer-Encoding: chunked）。
var hopHeaders = []string{
	"Connection", "Proxy-Connection", "Keep-Alive",
	"Transfer-Encoding", "Upgrade", "Te", "Trailer",
}

//go:embed page.html
var demoPage embed.FS

// smap 服务名 -> userId 的登记表。客户端连上来调 v1.Reg(name) 把自己登记成
// 服务提供者，拿到**负数** userId（SMap 从 -1 递减），与 Sign 分给普通客户端的
// 正数 ID 不冲突。网关据此把 /m/{svc}/... 路由到对应的连接。
var smap = sloth.NewSMap()

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sloth.SetLogLevel("info")

	// DefaultServer 返回 *ClientRpc：调用目标是**客户端**（名字里的 Client 指目标，
	// 见 ClientRpc 注释）。网关正是靠它把请求打到内网客户端上。
	server := sloth.DefaultServer()
	drpc := sloth.ServerConn(server)

	if err := drpc.Register("v1", &HelloService{}, ""); err != nil {
		sloth.Errorw(ctx, "register service failed", "err", err)
		return
	}

	// 媒体走 tcp：fn 帧 Length 是 uint32、上限 1GB，且响应不 base64。
	// ws 那边还有一层 DataSlice 分片（T/I 是 byte，≤256 片），大块不划算。
	if err := drpc.Listen(ctx, sloth.TCP, rpcAddr,
		option.WithTcpHandleMessage(&Handler{})); err != nil {
		sloth.Errorw(ctx, "listen failed", "err", err)
		return
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- drpc.Serve() }()

	// HTTP 网关：独立的 http.Server，不占用 RPC 端口，也不复用调试端口
	// （调试端点没有鉴权，语义不同）。
	mux := http.NewServeMux()
	mux.HandleFunc("/m/{svc}/{path...}", gateway(server))
	// 演示页面（静态资源在服务端本地，媒体全部来自客户端）
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		b, err := demoPage.ReadFile("page.html")
		if err != nil {
			http.Error(w, "page not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
	gw := &http.Server{
		Addr:              gatewayAddr,
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		sloth.Infow(ctx, "media gateway started", "addr", gatewayAddr,
			"hint", "curl -r 0-99 http://"+gatewayAddr+"/m/media/sample.bin")
		if err := gw.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			sloth.Errorw(ctx, "gateway exited", "err", err)
		}
	}()

	sloth.Infow(ctx, "media server started", "rpc", rpcAddr, "gateway", gatewayAddr)

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
	shutdownCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if err := gw.Shutdown(shutdownCtx); err != nil {
		sloth.Errorw(ctx, "shutdown gateway failed", "err", err)
	}
}

// gateway 把一次 HTTP 请求翻译成一次 RPC 调用。
//
// 只转发**无 body 的 GET**：入参上限 65535 字节只够放请求头，
// 带大 body 的请求要另想办法（扩 ag 长度字段或改走流式）。
func gateway(server *sloth.ClientRpc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		svc := r.PathValue("svc")
		path := r.PathValue("path")

		// 未登记一律 404：不区分"不存在"与"未登记"，避免被拿来探测内网。
		userId, ok := smap.Get(svc)
		if !ok {
			sloth.Infow(ctx, "service not registered", "svc", svc)
			http.NotFound(w, r)
			return
		}

		raw, err := dumpRequest(r, "/"+path)
		if err != nil {
			sloth.Errorw(ctx, "dump request failed", "err", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if len(raw) > maxReqBytes {
			// 431：请求头过大，转发过去也会撞 ag 的参数上限
			http.Error(w, "request header too large", http.StatusRequestHeaderFieldsTooLarge)
			return
		}

		callCtx, stop := context.WithTimeout(ctx, callTimeout)
		defer stop()

		start := time.Now()
		resp, err := server.Call(callCtx, userId, svcRpcMethod, raw)
		if err != nil {
			// 区分超时与连接问题：前者 504，后者 502
			code := http.StatusBadGateway
			if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
				code = http.StatusGatewayTimeout
			}
			sloth.Errorw(ctx, "rpc forward failed", "svc", svc, "path", path,
				"userId", userId, "err", err, "cost", time.Since(start).String())
			http.Error(w, "upstream error", code)
			return
		}
		if err := writeResponse(w, r, resp); err != nil {
			sloth.Errorw(ctx, "write response failed", "svc", svc, "path", path, "err", err)
			return
		}
		sloth.Infow(ctx, "forwarded", "svc", svc, "path", path,
			"reqBytes", len(raw), "respBytes", len(resp), "cost", time.Since(start).String())
	}
}

// svcRpcMethod 客户端侧暴露的转发方法（服务名 "http" + 方法 "Do"）。
const svcRpcMethod = "http.Do"

// dumpRequest 把收到的请求序列化成 HTTP 报文，路径改写为客户端本地路径。
//
// 不能直接 r.Write()：它是服务端请求（RequestURI 是外部路径、Host 是网关地址），
// 转发过去客户端会拿着错误的目标去请求。
func dumpRequest(r *http.Request, path string) ([]byte, error) {
	u := *r.URL
	u.Scheme = "http"
	u.Host = "media.local" // 占位：客户端会改写成自己的本地地址
	u.Path = path
	u.RawPath = ""

	req := &http.Request{
		Method:        r.Method,
		URL:           &u,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        r.Header.Clone(),
		Host:          "media.local",
		ContentLength: 0,
	}
	for _, h := range hopHeaders {
		req.Header.Del(h)
	}
	var buf bytes.Buffer
	if err := req.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeResponse 解析客户端回传的响应报文并原样写给浏览器。
// 状态码、响应头、body 都来自客户端，网关不做任何改写。
func writeResponse(w http.ResponseWriter, r *http.Request, raw []byte) error {
	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(raw)), r)
	if err != nil {
		return fmt.Errorf("read upstream response: %w", err)
	}
	defer resp.Body.Close()

	for _, h := range hopHeaders {
		resp.Header.Del(h)
	}
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		// body 写了一半浏览器就断开是常态（播放器 seek），只记不报
		return fmt.Errorf("copy body: %w", err)
	}
	return nil
}

// Handler TCP 的连接生命周期回调。
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

// HelloService 服务端侧的 RPC 服务：Sign（普通客户端）与 Reg（服务提供者）。
type HelloService struct{}

// Sign 把连接登记进 bucket（正数 userId），之后服务端可按 userId 主动推消息。
func (h *HelloService) Sign(ctx context.Context, data []byte) ([]byte, error) {
	ch, err := sloth.GetChannel(ctx)
	if err != nil {
		return nil, err
	}
	svr, err := sloth.GetBucket(ctx)
	if err != nil {
		return nil, err
	}
	ai := auth.AuthInfo{
		UserId: 1,
		RoomId: 1,
		Token:  "token_media",
		Ts:     time.Now().Unix(),
	}
	svr.Bucket(ai.UserId).Put(ai.UserId, ai.RoomId, ai.Token, ch)
	return tlv.Json(ai), nil
}

// Reg 把连接登记成服务提供者：分配**负数** userId，之后网关按服务名找到它。
// RoomId 用 -1：服务型连接不进房间，不会被 CallRoom 的广播误伤。
func (h *HelloService) Reg(ctx context.Context, name string) ([]byte, error) {
	ch, err := sloth.GetChannel(ctx)
	if err != nil {
		return nil, err
	}
	svr, err := sloth.GetBucket(ctx)
	if err != nil {
		return nil, err
	}
	svrId, err := smap.Reg(name, false)
	if err != nil {
		return nil, err
	}
	ai := auth.AuthInfo{
		UserId: svrId,
		RoomId: -1,
		Token:  "token_" + name,
		Ts:     time.Now().Unix(),
	}
	svr.Bucket(ai.UserId).Put(ai.UserId, ai.RoomId, ai.Token, ch)
	sloth.Infow(ctx, "service registered", "service", name, "userId", svrId)
	return tlv.Json(ai), nil
}
