package sloth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/w6xian/sloth/v4/logger"
)

// 本文件是 QUIC 传输的端到端验证。
//
// 它验证的不只是"QUIC 能通"，更是**传输层抽象的第三个落点**：
// 上层代码（Connect.Listen / Dial、codec、dispatch、bucket、per-call 回包分发）
// 一行没改就跑起了一种走 UDP、强制 TLS、一次 Accept 拿到"流"而不是"连接"的传输。
// 需要的改动只有两处扩展点：ListenerFactory（让传输自己造监听器）
// 与 Connect.TLSConfig()（把 TLS 配置交给传输）。

// selfSignedTLSForTest 生成测试用的自签证书（QUIC 强制 TLS，测试也绕不开）。
func selfSignedTLSForTest(tb testing.TB) *tls.Config {
	tb.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		tb.Fatalf("generate key: %v", err)
	}
	tpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sloth-quic-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tpl, &tpl, &key.PublicKey, key)
	if err != nil {
		tb.Fatalf("create cert: %v", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
}

// startQuicEnv 启动 QUIC 服务端与已握手的客户端（测试与基准共用）。
func startQuicEnv(tb testing.TB) (ctx context.Context, cli *Connect) {
	tb.Helper()
	// 连接建立/关闭会产生 ERROR 日志，不静音的话会混进测试/基准输出
	logger.SetOutput(io.Discard)
	tb.Cleanup(func() { logger.SetOutput(os.Stderr) })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	tb.Cleanup(cancel)

	svr := ServerConn(DefaultServer(), WithTLSConfig(selfSignedTLSForTest(tb)))
	if err := svr.Register("v1", &echoService{}, "echo service"); err != nil {
		tb.Fatalf("register err: %v", err)
	}
	// 端口 0：让内核分配，避免并行测试抢端口
	if err := svr.Listen(ctx, QUIC, "127.0.0.1:0"); err != nil {
		tb.Fatalf("listen err: %v", err)
	}
	addr := svr.listeners[0].Listener.Addr().String()
	go svr.Serve()
	tb.Cleanup(func() { svr.Close() })

	cli = ClientConn(DefaultClient(), WithTLSConfig(&tls.Config{InsecureSkipVerify: true}))
	if err := cli.Dial(ctx, QUIC, addr); err != nil {
		tb.Fatalf("dial err: %v", err)
	}
	tb.Cleanup(func() { cli.Close() })
	waitClientReady(tb, ctx, cli)
	return ctx, cli
}

func TestQuicTransportRpc(t *testing.T) {
	ctx, cli := startQuicEnv(t)

	resp, err := cli.server.Call(ctx, "v1.Hello", "world")
	if err != nil {
		t.Fatalf("call Hello: %v", err)
	}
	if string(resp) != "hello:world" {
		t.Fatalf("resp = %q, want \"hello:world\"", resp)
	}

	sum, err := cli.server.Call(ctx, "v1.Add", 1, 2)
	if err != nil {
		t.Fatalf("call Add: %v", err)
	}
	if string(sum) != "3" {
		t.Fatalf("Add = %q, want 3", sum)
	}
}

// TestQuicTransportBigPayload 超过单次读缓冲的大包：QUIC 的流同样没有消息
// 边界，必须按 FN 帧的 length 读完整。
func TestQuicTransportBigPayload(t *testing.T) {
	ctx, cli := startQuicEnv(t)
	payload := strings.Repeat("x", 32*1024)
	resp, err := cli.server.Call(ctx, "v1.Echo", payload)
	if err != nil {
		t.Fatalf("call Echo: %v", err)
	}
	if len(resp) != len(payload) {
		t.Fatalf("resp len = %d, want %d", len(resp), len(payload))
	}
}

// TestQuicTransportConcurrentCalls 单条流上并发调用：每个调用必须拿到自己的回包。
func TestQuicTransportConcurrentCalls(t *testing.T) {
	ctx, cli := startQuicEnv(t)

	const workers, perWorker = 8, 20
	var failed atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				want := fmt.Sprintf("hello:w%d-%d", w, i)
				resp, err := cli.server.Call(ctx, "v1.Hello", fmt.Sprintf("w%d-%d", w, i))
				if err != nil {
					t.Errorf("call: %v", err)
					failed.Add(1)
					continue
				}
				if string(resp) != want {
					t.Errorf("resp = %q, want %q（拿到别人的回包？）", resp, want)
					failed.Add(1)
				}
			}
		}(w)
	}
	wg.Wait()
	if failed.Load() != 0 {
		t.Fatalf("%d/%d calls failed", failed.Load(), workers*perWorker)
	}
}

// TestQuicRequiresTLS QUIC 没有证书必须报错，而不是"监听成功但永远握不上手"。
func TestQuicRequiresTLS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	svr := ServerConn(DefaultServer())
	// 不传 WithTLSConfig：监听应直接失败并给出明确原因
	err := svr.Listen(ctx, QUIC, "127.0.0.1:0")
	if err == nil {
		svr.Close()
		t.Fatal("quic listen without TLS should fail")
	}
	if !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("err = %v, want it to mention TLS", err)
	}
}

// BenchmarkQuicCallSerial 与 TCP / WS 同名的基准，用于对比三种传输的往返开销。
func BenchmarkQuicCallSerial(b *testing.B) {
	ctx, cli := startQuicEnv(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := cli.server.Call(ctx, "v1.Hello", "world")
		if err != nil {
			b.Fatalf("call err: %v", err)
		}
		if string(resp) != "hello:world" {
			b.Fatalf("unexpected resp: %q", resp)
		}
	}
}
