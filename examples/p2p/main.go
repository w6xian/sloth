package main

// p2p 直连的服务端样例（被连端）：examples/p2p/client 是与之配对的客户端。
//
// 业务代码与 examples/quic 完全一样（同一个 HelloService、同一套连接回调），
// 差异只有一处：Listen 多带了两个 p2p 选项——WithCoordinate（coordinator
// 信令面）与 WithWorkID（本端身份）。带上它们后 Listen 不再直接监听，而是
// 先经 coordinator 牵线打洞，把 QUIC 跑在打洞的那条 UDP socket 上（NAT
// 映射与 socket 绑定，见 sloth/p2p 包注释）。对上层完全同构：注册服务、
// 连接回调、RPC 与直连云端的写法一字不差。
//
// 运行（coordinator 部署与 ID 生成见 tools/tunnel/examples/p2p/README.md）：
//   go run .        -coord <云端IP>:9100 -id work-xxxx -token <token>
//   go run ./client -target work-xxxx@<云端IP>:9100
//
// 说明：
//   - bind 地址必须是 0.0.0.0 或内网 IP——绑 127.0.0.1 的 socket 出不了
//     公网，REG/探针发不到 coordinator（仅单机三端联调可用 127.0.0.1）；
//   - 这条 socket 一路用到底（REG 心跳 → 探针 → QUIC 监听），不能再
//     另开端口监听，否则 NAT 映射对不上，对方的包永远进不来；
//   - QUIC 端口是 UDP，curl / nc 都打不通（连握手的 ALPN 都对不上）。

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"flag"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
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

func main() {
	// p2p 模式需要知道 coordinator 在哪：默认线上地址，本地联调时 -coord localhost:9100
	coordAddr := flag.String("coord", "coord.graybook.com.cn:9100", "coordinator 的 ws 信令地址")
	workID := flag.String("id", "work-id-abde", "本端 work-id（coordinator -create 生成）")
	workToken := flag.String("token", "", "work-id 的接管凭据（台账里的 id 必须携带）")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ① 日志配置：全局一次，必须在发起任何调用之前。
	//    debug 会额外打印连接建立/关闭与方法注册明细，生产建议 info（默认）。
	sloth.SetLogLevel("debug")
	// 日志进 ELK/Loki 时打开：单行 JSON，可直接按 level/trace/字段检索。
	// sloth.SetLogJSON(true)

	// ② TLS：QUIC 的必填项，不是可选项。
	//    真实部署请换成正式证书，并让客户端正常校验（别用自签 + 跳过校验）。
	tlsConf, err := selfSignedTLS()
	if err != nil {
		sloth.Errorw(ctx, "generate self signed cert failed", "err", err)
		return
	}

	server := sloth.DefaultServer()
	drpc := sloth.ServerConn(server,
		sloth.WithTLSConfig(tlsConf),
		// ③ 调试服务独立端口：/debug/metrics（Prometheus）、/debug/pprof/*。
		//    与业务端口分离——这些端点没有鉴权，别挂在公网。
		sloth.WithDebugAddr("127.0.0.1:6062"))

	// ④ 注册服务：与 ws / tcp 完全一致。
	//    服务对象在所有连接间共享（Register 传的是同一个实例指针），而 RPC 是并发
	//    到达的，因此这里的计数必须用 atomic（直接自增普通字段会 data race）。
	if err := drpc.Register("v1", &HelloService{}, "metadata"); err != nil {
		sloth.Errorw(ctx, "register service failed", "err", err)
		return
	}

	// ⑤ 监听：p2p 模式——带 WithCoordinate 时不再直接监听，而是先经
	//    coordinator 牵线打洞，把 QUIC 跑在打洞的那条 UDP socket 上
	//    （NAT 映射与 socket 绑定，见 sloth/p2p 包注释）。
	//    bind 地址必须是 0.0.0.0（或内网 IP）：绑 127.0.0.1 的话 socket
	//    出不了公网，REG/探针发不到 coordinator（本地联调三端都在回环
	//    才能用 127.0.0.1）。work-id + token 由 coordinator -create 生成；
	//    客户端凭 id 找到本端。
	if err := drpc.Listen(ctx, sloth.QUIC, "0.0.0.0:8992",
		option.WithCoordinate(sloth.Coordinator(sloth.WS, *coordAddr)),
		option.WithCoordinate(sloth.Coordinator(sloth.UDP, p2pUDPFromWS(*coordAddr))),
		option.WithWorkID(*workID, *workToken),
		option.WithTcpHandleMessage(&Handler{})); err != nil {
		sloth.Errorw(ctx, "listen failed", "err", err)
		return
	}

	// ⑥ Serve() 是阻塞的：它在所有 listener 退出后才返回，
	//    所以要放 goroutine，否则后面的 "started" 永远不会打印。
	serveErr := make(chan error, 1)
	go func() { serveErr <- drpc.Serve() }()

	sloth.Infow(ctx, "quic server started",
		"quic", "0.0.0.0:8992", "debug", "127.0.0.1:6062")

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

// p2pUDPFromWS 由 ws 信令地址推 UDP 观测地址（同主机端口+1，本地约定）。
// 仅为示例便利：实际上 Register 响应里会带回准确地址，框架优先用它。
func p2pUDPFromWS(addr string) string {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return addr
	}
	port, err := strconv.Atoi(addr[i+1:])
	if err != nil {
		return addr
	}
	return addr[:i] + ":" + strconv.Itoa(port+1)
}

// selfSignedTLS 生成一张自签证书，只为了让样例能直接跑起来。
//
// QUIC 没有 TLS 就握不了手，所以样例必须先有证书；
// 而证书文件（cert.pem/key.pem）会带来"先跑个脚本生成"的前置步骤，
// 因此这里运行时生成——代价是客户端必须跳过校验（见 client/main.go）。
//
// 生产环境不要用这套：用正式证书 + 客户端正常校验证书链；
// 或者让 coordinator 作信任锚（登记时交换证书指纹，Dial 时校验对端
// 指纹一致，Tailscale 的做法）。
func selfSignedTLS() (*tls.Config, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	// 样例用固定序列号；真实场景请用 crypto/rand 生成，重复序列号会让证书被拒
	tpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sloth-quic-example"},
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

// Handler 连接生命周期回调：QUIC 与 TCP 用的是同一套钩子（无 *http.Request）。
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
		"req":  "quic server",
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
