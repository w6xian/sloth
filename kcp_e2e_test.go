package sloth

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/w6xian/sloth/v4/logger"
	"github.com/w6xian/sloth/v4/option"
)

// 本文件验证 KCP（UDP）链路的端到端可用性。
//
// KCP 与另三种传输有两处实质差异，这里各用一条测试守住：
//
//  1. 加密方式与 FEC 参数必须在**建监听器那一刻**就确定——它们决定线上包的
//     格式，之后改不了。所以配置要从 Listen 的选项里解析
//     （ListenerFactoryWithOptions），拿不到就退化成默认配置：两端一旦不一致，
//     表现是"连不上、也不报错"，最难排查的那种故障。
//
//  2. UDP 没有连接状态：对端消失不会有任何通知，只能等读超时。
//     因此这里不测"对端掉线立刻感知"（KCP 做不到），只测正常往返。

// startKcpServer 起一个 KCP 服务端，返回它的监听地址与实例。
func startKcpServer(tb testing.TB, ctx context.Context, cfg option.KCPConfig) (*Connect, string) {
	tb.Helper()
	push := DefaultServer()
	svr := ServerConn(push)
	if err := svr.Register("v1", &echoService{}, "echo service"); err != nil {
		tb.Fatalf("register err: %v", err)
	}
	if err := svr.Listen(ctx, KCP, "127.0.0.1:0", option.WithKCPConfig(cfg)); err != nil {
		tb.Fatalf("listen kcp err: %v", err)
	}
	addr := ""
	for _, l := range svr.listeners {
		if l.Network == KCP {
			addr = l.Listener.Addr().String()
		}
	}
	if addr == "" {
		tb.Fatal("kcp listener not registered")
	}
	go svr.Serve()
	tb.Cleanup(func() { svr.Close() })
	return svr, addr
}

// dialKcp 拨一条 KCP 客户端连接并等到就绪。
func dialKcp(tb testing.TB, ctx context.Context, addr string, cfg option.KCPConfig) *Connect {
	tb.Helper()
	cli := ClientConn(DefaultClient())
	tb.Cleanup(func() { cli.Close() })
	go func() { _ = cli.Dial(ctx, KCP, addr, option.WithKCPConfig(cfg)) }()
	waitClientReady(tb, ctx, cli)
	return cli
}

// TestKcpCallRoundTrip 默认配置（不加密、无 FEC）下的 RPC 往返。
func TestKcpCallRoundTrip(t *testing.T) {
	logger.SetOutput(io.Discard)
	t.Cleanup(func() { logger.SetOutput(os.Stderr) })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, addr := startKcpServer(t, ctx, option.KCPConfig{})
	cli := dialKcp(t, ctx, addr, option.KCPConfig{})

	resp, err := cli.server.Call(ctx, "v1.Add", 2, 3)
	if err != nil {
		t.Fatalf("call v1.Add err: %v", err)
	}
	if got := string(resp); got != "5" {
		t.Fatalf("v1.Add = %q, want 5", got)
	}
}

// TestKcpCallWithCryptAndTuning 加密 + 低延迟调优下的 RPC 往返。
//
// 两端必须使用相同的 crypt / key / FEC：KCP 不会报"配置不匹配"，
// 解不开对端的包就一直静默重试，直到调用超时。这条测试同时也是
// "配置能从选项一路传到监听器与连接上"的验证——传不到的话，
// 两端会用不同参数建连接，这里就会超时失败。
func TestKcpCallWithCryptAndTuning(t *testing.T) {
	logger.SetOutput(io.Discard)
	t.Cleanup(func() { logger.SetOutput(os.Stderr) })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg := option.FastKCPConfig()
	cfg.Crypt = option.KCPCryptAES
	cfg.Key = []byte("0123456789abcdef") // AES 要求 16/24/32 字节
	cfg.DataShards = 10
	cfg.ParityShards = 3

	_, addr := startKcpServer(t, ctx, cfg)
	cli := dialKcp(t, ctx, addr, cfg)

	resp, err := cli.server.Call(ctx, "v1.Add", 20, 22)
	if err != nil {
		t.Fatalf("call v1.Add err: %v", err)
	}
	if got := string(resp); got != "42" {
		t.Fatalf("v1.Add = %q, want 42", got)
	}
}

// TestKcpListenRejectsBadCrypt 加密方式写错必须在 Listen 阶段就报错，
// 而不是建出一个"能起但连不上"的监听器。
func TestKcpListenRejectsBadCrypt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	svr := ServerConn(DefaultServer())
	defer svr.Close()
	bad := option.KCPConfig{Crypt: "rot13"}
	if err := svr.Listen(ctx, KCP, "127.0.0.1:0", option.WithKCPConfig(bad)); err == nil {
		t.Fatal("Listen with unknown kcp crypt should error")
	}
}
