package nrpc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/w6xian/sloth/v3/actions"
	"github.com/w6xian/sloth/v3/decoder/fn"
)

// ---------------------------------------------------------------------------
// CallFuncWithResult：超时语义回归测试
// ---------------------------------------------------------------------------

// replyOK 构造一条成功响应帧。
func replyOK(t *testing.T, id uint64, data string) []byte {
	t.Helper()
	b, err := fn.Encode(actions.ACTION_REPLY_SUCCESS, id, []byte(data))
	if err != nil {
		t.Fatalf("fn.Encode: %v", err)
	}
	return b
}

func TestCallFuncWithResultSuccess(t *testing.T) {
	sender := DataChannel{Read: make(chan []byte, 1), Write: make(chan []byte, 1)}
	go func() {
		payload := <-sender.Write
		sender.Read <- replyOK(t, 42, "got:"+string(payload))
	}()
	got, err := CallFuncWithResult(context.Background(), 42, []byte("ping"), sender, TimeOut{Read: time.Second, Write: time.Second})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if string(got) != "got:ping" {
		t.Fatalf("got %q", got)
	}
}

// 非本调用 id 的响应必须被跳过（并发 RPC 下响应可能乱序到达）。
func TestCallFuncWithResultSkipsOtherID(t *testing.T) {
	sender := DataChannel{Read: make(chan []byte, 4), Write: make(chan []byte, 1)}
	go func() {
		<-sender.Write
		sender.Read <- replyOK(t, 999, "other")
		sender.Read <- replyOK(t, 7, "mine")
	}()
	got, err := CallFuncWithResult(context.Background(), 7, []byte("p"), sender, TimeOut{Read: 2 * time.Second, Write: time.Second})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if string(got) != "mine" {
		t.Fatalf("got %q", got)
	}
}

func TestCallFuncWithResultReplyError(t *testing.T) {
	sender := DataChannel{Read: make(chan []byte, 1), Write: make(chan []byte, 1)}
	go func() {
		<-sender.Write
		b, _ := fn.Encode(actions.ACTION_REPLY_ERROR, 5, []byte("boom"))
		sender.Read <- b
	}()
	if _, err := CallFuncWithResult(context.Background(), 5, []byte("p"), sender, TimeOut{Read: time.Second, Write: time.Second}); err == nil {
		t.Fatal("expected error for ACTION_REPLY_ERROR")
	} else if err.Error() != "boom" {
		t.Fatalf("unexpected err: %v", err)
	}
}

// 无人读取 Write → 写阶段超时
func TestCallFuncWithResultWriteTimeout(t *testing.T) {
	sender := DataChannel{Read: make(chan []byte, 1), Write: make(chan []byte)} // 无缓冲且无读者
	start := time.Now()
	_, err := CallFuncWithResult(context.Background(), 1, []byte("p"), sender, TimeOut{Read: time.Second, Write: 50 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "call timeout") {
		t.Fatalf("expected call timeout, got %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("write timeout took %v, want ~50ms", d)
	}
}

// 有读取但无响应 → 读阶段超时
func TestCallFuncWithResultReplyTimeout(t *testing.T) {
	sender := DataChannel{Read: make(chan []byte), Write: make(chan []byte, 1)}
	go func() { <-sender.Write }() // 只取走请求，永不响应
	start := time.Now()
	_, err := CallFuncWithResult(context.Background(), 1, []byte("p"), sender, TimeOut{Read: 60 * time.Millisecond, Write: time.Second})
	if err == nil || !strings.Contains(err.Error(), "reply timeout") {
		t.Fatalf("expected reply timeout, got %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("reply timeout took %v, want ~60ms", d)
	}
}

// 超时配置为 0 时必须走兜底值（原实现会立刻触发，把正常调用判成超时）。
func TestCallFuncWithResultZeroTimeoutFallback(t *testing.T) {
	sender := DataChannel{Read: make(chan []byte, 1), Write: make(chan []byte, 1)}
	go func() {
		<-sender.Write
		sender.Read <- replyOK(t, 3, "ok")
	}()
	got, err := CallFuncWithResult(context.Background(), 3, []byte("p"), sender, TimeOut{}) // Read/Write 均为 0
	if err != nil {
		t.Fatalf("zero timeout should fall back to default, got err: %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("got %q", got)
	}
}

// TestCallFuncWithResultNoStaleTimeout 回归测试：写入阶段遗留的到期信号不得污染读取阶段。
//
// 原实现用 time.Ticker：写阶段结束后直接 Reset，Ticker 通道里已到期的信号不会被排空，
// 于是读取阶段立刻"假超时"（并发 RPC 下偶发 call timeout）。
// 这里让写阶段超时(5ms)小于响应延迟(80ms)，且写入本身瞬时完成：
// 新实现（Timer + 排空）必须成功，旧实现必然返回 reply timeout。
func TestCallFuncWithResultNoStaleTimeout(t *testing.T) {
	for i := 0; i < 10; i++ {
		sender := DataChannel{Read: make(chan []byte, 1), Write: make(chan []byte)}
		go func() {
			<-sender.Write // 立即取走，写阶段瞬时完成
			time.Sleep(80 * time.Millisecond)
			sender.Read <- replyOK(t, 11, "late-but-ok")
		}()
		got, err := CallFuncWithResult(context.Background(), 11, []byte("p"), sender,
			TimeOut{Write: 5 * time.Millisecond, Read: 3 * time.Second})
		if err != nil {
			t.Fatalf("iter %d: stale tick leaked into read phase: %v", i, err)
		}
		if string(got) != "late-but-ok" {
			t.Fatalf("iter %d: got %q", i, got)
		}
	}
}

// ctx 取消必须立即返回，不等超时。
func TestCallFuncWithResultContextCancel(t *testing.T) {
	sender := DataChannel{Read: make(chan []byte), Write: make(chan []byte, 1)}
	go func() { <-sender.Write }()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if _, err := CallFuncWithResult(ctx, 1, []byte("p"), sender, TimeOut{Read: 5 * time.Second, Write: time.Second}); err == nil {
		t.Fatal("expected ctx error")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("ctx cancel took %v", d)
	}
}

// ---------------------------------------------------------------------------
// Benchmark
// ---------------------------------------------------------------------------

// 一次完整"发请求等响应"的开销（排除真实网络，只测协议与超时控制部分）
func BenchmarkCallFuncWithResult(b *testing.B) {
	sender := DataChannel{Read: make(chan []byte, 64), Write: make(chan []byte, 64)}
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-quit:
				return
			case <-sender.Write:
				frame, _ := fn.Encode(actions.ACTION_REPLY_SUCCESS, 1, []byte("pong"))
				select {
				case sender.Read <- frame:
				case <-quit:
					return
				}
			}
		}
	}()
	to := TimeOut{Read: 5 * time.Second, Write: time.Second}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := CallFuncWithResult(context.Background(), 1, []byte("ping"), sender, to); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	close(quit)
}
