package sloth

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/w6xian/sloth/v4/logger"
)

// 本文件是 TCP 传输的端到端验证。
//
// 它的意义不在"多测一种协议"，而在于**检验传输层抽象**：如果能只换成
// network="tcp" 就跑通完整 RPC 链路（注册 → 调用 → 回包 → 分发），说明
// codec / dispatch / bucket / per-call 分发确实与传输无关。
// 改造前这是不可能的：Connect.Listen/Dial 里只有 ws 分支，其它协议直接报错。

// startTcpEnv 启动 TCP 服务端与已连接的客户端（测试与基准共用）。
func startTcpEnv(tb testing.TB) (ctx context.Context, cli *Connect) {
	tb.Helper()
	// 连接建立/关闭会产生 ERROR 日志，不静音的话会混进测试/基准输出
	logger.SetOutput(io.Discard)
	tb.Cleanup(func() { logger.SetOutput(os.Stderr) })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	tb.Cleanup(cancel)

	svr := ServerConn(DefaultServer())
	if err := svr.Register("v1", &echoService{}, "echo service"); err != nil {
		tb.Fatalf("register err: %v", err)
	}
	if err := svr.Listen(ctx, "tcp", "127.0.0.1:0"); err != nil {
		tb.Fatalf("listen err: %v", err)
	}
	addr := svr.listeners[0].Listener.Addr().String()
	go svr.Serve()
	tb.Cleanup(func() { svr.Close() })
	waitTcpServerReady(tb, ctx, addr)

	cli = ClientConn(DefaultClient())
	// TCP 客户端的 Dial 不阻塞（连接由后台 goroutine 服务）
	if err := cli.Dial(ctx, "tcp", addr); err != nil {
		tb.Fatalf("dial err: %v", err)
	}
	tb.Cleanup(func() { cli.Close() })
	waitClientReady(tb, ctx, cli)
	return ctx, cli
}

// 与 WS 同名的两个基准，用于直接对比两种传输的往返开销
// （TCP 少了 HTTP 升级与 WebSocket 帧掩码/分片）。
func BenchmarkTcpCallSerial(b *testing.B) {
	ctx, cli := startTcpEnv(b)
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

func BenchmarkTcpCallParallel(b *testing.B) {
	ctx, cli := startTcpEnv(b)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := cli.server.Call(ctx, "v1.Add", 1, 2); err != nil {
				b.Errorf("call err: %v", err)
				return
			}
		}
	})
}

// waitTcpServerReady 轮询直到 TCP 端口可连接。
// 现有的 waitServerReady 用 gorilla 拨 "ws://addr/ws" 探测，对纯 TCP 服务不适用
// （helper 也把 WebSocket 的细节写死了，这是抽象的又一处泄漏）。
func waitTcpServerReady(tb testing.TB, ctx context.Context, addr string) {
	tb.Helper()
	for {
		select {
		case <-ctx.Done():
			tb.Fatalf("tcp server %s not ready: %v", addr, ctx.Err())
		default:
		}
		conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}
func TestTcpTransportRpc(t *testing.T) {
	ctx, cli := startTcpEnv(t)

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

// TestTcpTransportBigPayload 超过单次读缓冲的大包：验证分帧按 length 读完整，
// 不会因为一次 Read 没读满而把一帧拆坏（TCP 是字节流，这是最容易写错的地方）。
func TestTcpTransportBigPayload(t *testing.T) {
	ctx, cli := startTcpEnv(t)
	// 32KB 远超读循环的单次读缓冲（4KB），足以验证"按 length 读完整帧"；
	// 再大就会撞上 ag 编码器的 65535 上限（框架既有约束，与传输无关）。
	payload := strings.Repeat("x", 32*1024)
	resp, err := cli.server.Call(ctx, "v1.Echo", payload)
	if err != nil {
		t.Fatalf("call Echo: %v", err)
	}
	if len(resp) != len(payload) {
		t.Fatalf("resp len = %d, want %d", len(resp), len(payload))
	}
}

// TestTcpTransportConcurrentCalls 单连接并发调用：每个调用必须拿到自己的回包。
//
// P1 把回包分发改成了 per-call，这里验证它在 TCP 上同样成立（回包按 id 匹配，
// 与 WebSocket 走的是同一套 RpcChannel/SendData 代码）。
func TestTcpTransportConcurrentCalls(t *testing.T) {
	ctx, cli := startTcpEnv(t)

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
