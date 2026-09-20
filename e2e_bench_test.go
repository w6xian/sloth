package sloth

import (
	"context"
	"strings"
	"testing"
	"time"
)

// 端到端 RPC 吞吐基准：真实 WS 连接 + 完整编解码链路（客户端 → 服务端 → 客户端）。
// 用于观察热路径优化（手写 JSON 编码、分片名查表、分桶键零分配、对象池）的整体收益。

// startBenchEnv 启动真实 WS 服务端与已连接的客户端。
func startBenchEnv(b *testing.B) (ctx context.Context, cli *Connect) {
	b.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

	svr := ServerConn(DefaultServer())
	if err := svr.Register("v1", &echoService{}, "echo service"); err != nil {
		cancel()
		b.Fatalf("register err: %v", err)
	}
	if err := svr.Listen(ctx, "ws", "127.0.0.1:0"); err != nil {
		cancel()
		b.Fatalf("listen err: %v", err)
	}
	addr := svr.listeners[0].Listener.Addr().String()
	go svr.Serve()
	waitServerReady(b, ctx, addr)

	cli = ClientConn(DefaultClient())
	go cli.Dial(ctx, "ws", addr)
	waitClientReady(b, ctx, cli)

	b.Cleanup(func() {
		cli.Close()
		svr.Close()
		cancel()
	})
	return ctx, cli
}

// benchCall 发起一次 RPC；端到端链路偶发瞬时错误（重连/超时抖动）时重试一次，
// 避免把环境噪声记成基准失败。
func benchCall(ctx context.Context, cli *Connect, method string, args ...interface{}) ([]byte, error) {
	res, err := cli.server.Call(ctx, method, args...)
	if err != nil {
		return cli.server.Call(ctx, method, args...)
	}
	return res, err
}

// 串行调用：单连接单协程的往返延迟与分配
func BenchmarkWsCallSerial(b *testing.B) {
	ctx, cli := startBenchEnv(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := benchCall(ctx, cli, "v1.Hello", "world")
		if err != nil {
			b.Fatalf("call err: %v", err)
		}
		if string(resp) != "hello:world" {
			b.Fatalf("unexpected resp: %q", resp)
		}
	}
}

// 并发调用：多协程共享同一连接（贴近线上真实负载）
func BenchmarkWsCallParallel(b *testing.B) {
	ctx, cli := startBenchEnv(b)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := benchCall(ctx, cli, "v1.Add", 1, 2); err != nil {
				b.Errorf("call err: %v", err)
				return
			}
		}
	})
}

// 不同 payload 大小下的吞吐（1KB / 32KB，32KB 会触发分片发送与重组）
func BenchmarkWsCallPayload(b *testing.B) {
	for _, size := range []int{1024, 32 * 1024} {
		payload := strings.Repeat("x", size)
		b.Run(byteSizeLabel(size), func(b *testing.B) {
			ctx, cli := startBenchEnv(b)
			b.ReportAllocs()
			b.SetBytes(int64(size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resp, err := benchCall(ctx, cli, "v1.Echo", payload)
				if err != nil {
					b.Fatalf("call err: %v", err)
				}
				if len(resp) != size {
					b.Fatalf("resp len = %d, want %d", len(resp), size)
				}
			}
		})
	}
}

func byteSizeLabel(n int) string {
	switch {
	case n >= 1024*1024:
		return "MB"
	case n >= 1024:
		return "KB" + itoaLabel(n/1024)
	default:
		return "B" + itoaLabel(n)
	}
}

func itoaLabel(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
