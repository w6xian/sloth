package main

// 一个进程同时开 ws / tcp / quic 三种协议的服务端样例。
//
// 关键在于**这三个 Listen 挂在同一个 Connect 上**：
//
//	drpc.Listen(ctx, sloth.WS,   "localhost:8990", wsOpts...)
//	drpc.Listen(ctx, sloth.TCP,  "localhost:8991", streamOpts...)
//	drpc.Listen(ctx, sloth.QUIC, "localhost:8992", streamOpts...)
//
// 之后一次 Serve() 会把三条监听全部拉起来，业务侧（注册服务、方法实现、
// 主动推送）完全无感——除了 network 字符串与连接回调，代码与单协议样例一致。
//
// 三条链路共用同一份服务注册表与同一个 ClientRpc（server.Call / CallRoom /
// CallBucket / Broadcast），也就是说：
//   - 客户端从哪条协议连上来，都能调到 v1.* 的所有方法；
//   - 服务端一次 CallRoom(1, ...) 会把消息推给**三条协议上**的所有房间成员，
//     而不是只推给其中一条（合成实例负责跨传输扇出，见 multiServer）。
//
// 运行：
//   go run ./examples/multi          # 服务端，同时监听 8990(ws) / 8991(tcp) / 8992(quic)
//   go run ./examples/multi/client   # 客户端，三条协议各连一条，每 2 秒调一次
//
// 端口与 examples/ws、examples/tcp、examples/quic 三个单协议服务端**完全一致**，
// 所以那三个客户端（examples/ws/client、examples/tcp/client、examples/quic/client）
// 不用改一行代码就能连上本服务端——验证"多协议监听"时可以直接拿它们当客户端用。
//
// 注意：QUIC 端口是 UDP，且强制 TLS（样例用运行时生成的自签证书，
// 所以客户端必须跳过校验）；ws / tcp 端口仍是明文——给 QUIC 配的 TLS
// 不会影响到它们。

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/slots"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/sloth/v4/types/auth"
	"github.com/w6xian/tlv"
)

// 三条协议的监听地址（ws / tcp 是 TCP 端口，quic 是 UDP 端口）。
// 与 examples/ws、examples/tcp、examples/quic 保持同一组端口，
// 便于用那三个 example 客户端直接验证本服务端。
const (
	wsAddr   = "localhost:8990"
	tcpAddr  = "localhost:8991"
	quicAddr = "localhost:8992"
	debugAdr = "127.0.0.1:6063"
	roomId   = 1 // 三条协议上的客户端都进同一个房间，便于验证推送覆盖所有协议
)

// 客户端不需要、也不应该自己填协议标识：库在每次调用时都会把
// sloth-protocol（见 sloth.HeaderProtocol）写进固定头，取值就是 Dial 时用的
// 协议名。服务端统一用 sloth.GetProtocol(ctx) 读。
//
// 为什么不能只靠 Sign 的入参：三个单协议 example 客户端传的都是同一个值
// （`[]byte("sign")`），服务端分不出谁是谁，三条链路全被登记成同一个 userId，
// 于是"一次 CallRoom 推给了三条协议"这件事在日志里根本看不出来——
// 看起来像只有一条链路在线。

// userIds 让三条协议的客户端用不同 userId 登录，日志里能一眼看出消息推给了谁。
var userIds = map[string]int64{"ws": 1, "tcp": 2, "quic": 3}

// smap 是"服务提供者"登记表：服务名 -> userId。
//
// 客户端不只是能调用，还能给服务端**扩展**服务：连上来调一次 v1.Reg(name)，
// 服务端给它分配一个 userId 并把连接登记进来；之后任何人调用 "name.Method"，
// 本地查不到就走 proxy 转发到这条连接上（见 proxyService）。
//
// 关键前提：SMap 分配的是**负数** userId（-1、-2…，见 SMap.Reg），
// 不会和 Sign 分给普通客户端的正数 ID（ws=1 / tcp=2 / quic=3）撞车。
// 所以 ws/node 这类连接可以先 Sign 拿正数 ID 收房间推送，再 Reg 拿负数 ID
// 当服务提供者——两个 ID 指向同一条物理连接，互不覆盖。
var smap = sloth.NewSMap()

// signIds 给"header 里没见过的协议值"分配 userId：不同值必须拿到不同 ID，
// 否则又会出现三条链路共用一个 userId、日志里分不出谁是谁的情况。
//
// 并发调用 Sign 会同时读改这个 map，所以带锁；从 100 起是为了和上面的
// 固定 ID（1/2/3）区分开，一眼能看出它走了兜底分支。
var (
	signMu   sync.Mutex
	signIds  = map[string]int64{}
	nextSign = int64(100)
)

// userIdForProtocol 按协议值分配 userId：已知协议用固定 ID（1/2/3），
// 未知值按需分配，保证不同的值一定拿到不同的 ID。
func userIdForProtocol(name string) int64 {
	if name == "" {
		return 0
	}
	signMu.Lock()
	defer signMu.Unlock()
	if id, ok := userIds[name]; ok {
		return id
	}
	id, ok := signIds[name]
	if !ok {
		id = nextSign
		nextSign++
		signIds[name] = id
	}
	return id
}

// AB is a test struct
type AB struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ① 日志配置：全局一次，必须在发起任何调用之前。
	//    debug 会额外打印连接建立/关闭与方法调用明细，生产建议 info（默认）。
	sloth.SetLogLevel("debug")

	// ② TLS：QUIC 的必填项（QUIC 的加密由 TLS 1.3 承担，没有证书握不了手）。
	//    这份配置只作用于 QUIC 与 wss，不会把 ws / tcp 端口变成 TLS。
	tlsConf, err := selfSignedTLS()
	if err != nil {
		sloth.Errorw(ctx, "generate self signed cert failed", "err", err)
		return
	}

	server := sloth.DefaultServer()
	drpc := sloth.ServerConn(server,
		sloth.WithTLSConfig(tlsConf),
		// ③ 调试服务独立端口：/debug/metrics（Prometheus）、/debug/pprof/*
		sloth.WithDebugAddr(debugAdr),
		// ③bis proxy 路由：本地没注册的服务（比如 ws/node 注册上来的 shop1）
		//     按服务名找到它所在连接的 userId，把调用转过去。
		sloth.WithConnectProxy(proxyService))

	// ④ 注册服务：三条协议共用同一份注册表，注册一次即可。
	if err := drpc.Register("v1", &HelloService{}, "metadata"); err != nil {
		sloth.Errorw(ctx, "register service failed", "err", err)
		return
	}

	// ⑤ 三条协议同时监听。每条 Listen 只登记传输实例，真正启动在 Serve()。
	//    ws 用 HTTP 版连接回调（带 *http.Request），tcp / quic 用无 HTTP 版回调。
	if err := drpc.Listen(ctx, sloth.WS, wsAddr,
		option.WithOrigin("*"),
		option.WithServerHandleMessage(&WsHandler{})); err != nil {
		sloth.Errorw(ctx, "listen ws failed", "err", err)
		return
	}
	// tcp 与 quic 共用同一套回调：钩子描述的是"一条连接的生命周期"，
	// 与它跑在 TCP 还是 QUIC 上无关。
	streamHook := option.WithTcpHandleMessage(&StreamHandler{})
	if err := drpc.Listen(ctx, sloth.TCP, tcpAddr, streamHook); err != nil {
		sloth.Errorw(ctx, "listen tcp failed", "err", err)
		return
	}
	if err := drpc.Listen(ctx, sloth.QUIC, quicAddr, streamHook); err != nil {
		sloth.Errorw(ctx, "listen quic failed", "err", err)
		return
	}

	// ⑥ Serve() 是阻塞的：它在所有 listener 退出后才返回，所以要放 goroutine。
	serveErr := make(chan error, 1)
	go func() { serveErr <- drpc.Serve() }()

	sloth.Infow(ctx, "multi-protocol server started",
		"ws", wsAddr, "tcp", tcpAddr, "quic", quicAddr, "debug", debugAdr)

	// ⑦ 服务端主动推：每 5 秒向房间推一次。
	//    这里是本样例的重点——同一条 CallRoom 会打到 ws / tcp / quic
	//    三条链路上的所有房间成员（客户端样例会打印各自收到的内容）。
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for i := 1; ; i++ {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				callCtx, _ := sloth.EnsureTrace(ctx)
				if _, err := server.CallRoom(callCtx, roomId, "shop.Test",
					fmt.Sprintf("push #%d from multi server", i)); err != nil {
					sloth.Errorw(callCtx, "room call failed", "room", roomId, "err", err)
					continue
				}
				sloth.Infow(callCtx, "room call dispatched", "room", roomId, "seq", i)
			}
		}
	}()

	// ⑧ 优雅关闭：收到中断信号，或 Serve 自己异常退出时收尾
	//    （Close 会关掉三条 listener 与调试服务，并等待监听 goroutine 退出）
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

// selfSignedTLS 生成一张自签证书，只为了让样例能直接跑起来（与 examples/quic 相同）。
//
// 生产环境不要用这套：用正式证书 + 客户端正常校验证书链。
func selfSignedTLS() (*tls.Config, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sloth-multi-example"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tpl, &tpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	}, nil
}

// WsHandler ws 的连接生命周期回调（HTTP 版：每个方法都带 *http.Request）。
// 嵌入 slots.Server 补齐未用到的方法，只重写关心的那个。
type WsHandler struct {
	slots.Server
}

func (h *WsHandler) OnConnect(ctx context.Context, r *http.Request) error {
	sloth.Infow(ctx, "ws client connected", "remote", r.RemoteAddr, "uri", r.RequestURI)
	return nil
}

// StreamHandler tcp / quic 的连接生命周期回调（无 HTTP 版）。
type StreamHandler struct{}

func (h *StreamHandler) OnConnect(ctx context.Context, addr string) error {
	sloth.Infow(ctx, "stream client connected", "remote", addr)
	return nil
}

func (h *StreamHandler) OnReady(ctx context.Context, s types.IBucket, ch bucket.IChannel) error {
	// 连接刚建立、还没 Sign：UserId() 此时是 0（Sign 之后才有值）
	sloth.Infow(ctx, "stream connection ready", "userId", ch.UserId())
	return nil
}

func (h *StreamHandler) OnData(ctx context.Context, s types.IBucket, ch bucket.IChannel, msg []byte) error {
	sloth.Infow(ctx, "stream data received", "size", len(msg))
	return nil
}

func (h *StreamHandler) OnClose(ctx context.Context, s types.IBucket, ch bucket.IChannel) error {
	sloth.Infow(ctx, "stream connection closed", "userId", ch.UserId())
	return nil
}

func (h *StreamHandler) OnError(ctx context.Context, s types.IBucket, ch bucket.IChannel, err error) error {
	sloth.Errorw(ctx, "stream connection error", "userId", ch.UserId(), "err", err)
	return nil
}

// HelloService 业务服务对象：三条协议的所有连接共享同一个实例，
// 可变状态必须原子/加锁保护（直接自增普通字段会 data race）。
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
		"req":  "multi server",
		"time": time.Now().Format("2006-01-02 15:04:05"),
	}, nil
}

// Sign 把这条连接登记进 bucket：登记之后服务端才能按 userId / roomId 主动推消息。
//
// 协议标识优先取 header 里的 sloth-protocol，取不到才退回入参 data。
// 这个顺序是必须的：examples/ws|tcp|quic/client 三个客户端传给 Sign 的都是
// 同一个值（"sign"），只看入参的话三条链路会被登记成同一个 userId，
// 推送日志里就再也分不出"推给了谁"——看起来像只有一条链路在线。
func (h *HelloService) Sign(ctx context.Context, data []byte) ([]byte, error) {
	h.reqCount.Add(1)

	ch, ok := ctx.Value(sloth.ChannelKey).(bucket.IChannel)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}
	// BucketKey 上的 IBucket 是**当前这条连接所属传输**的实例：
	// 每个传输各自持有自己的分桶，登记进来的连接也就只在该传输内可见。
	svr, ok := ctx.Value(sloth.BucketKey).(types.IBucket)
	if !ok {
		return nil, fmt.Errorf("bucket not found")
	}

	name, from := protocolFromHeader(ctx)
	if name == "" {
		// 老客户端（库还没开始写这个头）：退回入参，行为与之前一致
		name = strings.TrimSpace(string(data))
		from = "arg"
	}
	userId := userIdForProtocol(name)
	if userId == 0 {
		return nil, fmt.Errorf("sign: protocol not specified (header %q or arg)", sloth.HeaderProtocol)
	}

	ai := auth.AuthInfo{
		UserId: userId,
		RoomId: roomId,
		Token:  "token_" + name,
		Ts:     time.Now().Unix(),
	}
	svr.Bucket(ai.UserId).Put(ai.UserId, ai.RoomId, ai.Token, ch)
	sloth.Infow(ctx, "client signed",
		"proto", name, "from", from, "userId", ai.UserId, "roomId", ai.RoomId)
	return tlv.Json(ai), nil
}

// Reg 让一条连接把自己登记成"服务提供者"：分配一个**负数** userId 并放进
// bucket，之后 proxy 就能把 name.* 的调用转过来（examples/ws/node 就是这么
// 给服务端扩展出 shop1 的）。
//
// 与 Sign 的分工：Sign 是普通客户端进房间（正数 userId，收房间推送）；
// Reg 是服务型连接（负数 userId，收转发来的调用）。一条连接可以先 Sign 再
// Reg——两个 ID 并存，互不覆盖。
func (h *HelloService) Reg(ctx context.Context, name string) ([]byte, error) {
	h.reqCount.Add(1)

	ch, ok := ctx.Value(sloth.ChannelKey).(bucket.IChannel)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}
	svr, ok := ctx.Value(sloth.BucketKey).(types.IBucket)
	if !ok {
		return nil, fmt.Errorf("bucket not found")
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("reg: service name required")
	}
	// check=false：同一服务名重连时复用旧 ID（真实场景按需决定是否允许抢占）
	svrId, err := smap.Reg(name, false)
	if err != nil {
		return nil, err
	}
	// RoomId 用 -1：服务型连接不进房间，不会被 CallRoom 的广播误伤
	ai := auth.AuthInfo{
		UserId: svrId,
		RoomId: -1,
		Token:  "token_" + name,
		Ts:     time.Now().Unix(),
	}
	svr.Bucket(ai.UserId).Put(ai.UserId, ai.RoomId, ai.Token, ch)

	proto, _ := sloth.GetProtocol(ctx)
	sloth.Infow(ctx, "service registered",
		"service", name, "proto", proto, "userId", svrId)
	return tlv.Json(ai), nil
}

// proxyService 把"本地没注册的服务"路由到提供它的那条连接，返回它的 userId。
//
// 调用链：nrpc 发现 service 未注册 -> Connect.CallNetFunc -> 本函数 ->
// ClientRpc.CallNet -> ch.SendData，转发的是**客户端发来的原始报文整包**，
// 不重新组装 header，所以调用方带的 sloth-protocol / trace 都原样透传。
func proxyService(ctx context.Context, service string) (int64, error) {
	node, err := sloth.GetNode(service)
	if err != nil {
		return 0, err
	}
	svrId, ok := smap.Get(node.Service)
	if !ok {
		return 0, fmt.Errorf("service %s not registered", node.Service)
	}
	sloth.Infow(ctx, "proxy route", "service", service, "userId", svrId)
	return svrId, nil
}

// protocolFromHeader 读库自动写入的协议标识（sloth.GetProtocol）。
// 返回空串表示对端没这个头——由调用方决定是退回入参还是直接报错。
func protocolFromHeader(ctx context.Context) (name string, from string) {
	name, err := sloth.GetProtocol(ctx)
	if err != nil || name == "" {
		return "", ""
	}
	return strings.ToLower(name), "header"
}
