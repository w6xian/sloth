package nrpc

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/w6xian/sloth/v4/actions"
	"github.com/w6xian/sloth/v4/decoder/fn"
)

// ---------------------------------------------------------------------------
// SendData（同连接 RPC 调用）：超时与回包匹配语义回归测试
//
// 这一组原本是 CallFuncWithResult 的测试。P1 把"发请求等回包"换成
// RpcChannel.SendData + per-call 分发后，CallFuncWithResult 成了没人调用的
// 死代码；但**这些超时语义本身没有过时**，所以测试整体迁到活代码上来，
// 而不是随死代码一起删掉。
// ---------------------------------------------------------------------------

func TestSendDataSuccess(t *testing.T) {
	cc := newCallChan(1, time.Second, time.Second)
	go func() {
		payload := <-cc.PRpcCaller
		if err := cc.Receive(context.Background(), replyOK(t, 42, "got:"+string(payload))); err != nil {
			t.Errorf("receive: %v", err)
		}
	}()
	got, err := cc.SendData(context.Background(), 42, []byte("ping"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if string(got) != "got:ping" {
		t.Fatalf("got %q", got)
	}
}

// 非本调用 id 的回包不得干扰等待中的调用（并发 RPC 下回包可能乱序到达）。
func TestSendDataIgnoresOtherID(t *testing.T) {
	cc := newCallChan(1, time.Second, 2*time.Second)
	go func() {
		<-cc.PRpcCaller
		// 先投一条别的调用的回包：它应该无人接收（孤儿），不能污染本次调用
		if err := cc.Receive(context.Background(), replyOK(t, 999, "other")); err != nil {
			t.Errorf("receive other: %v", err)
		}
		if err := cc.Receive(context.Background(), replyOK(t, 7, "mine")); err != nil {
			t.Errorf("receive mine: %v", err)
		}
	}()
	got, err := cc.SendData(context.Background(), 7, []byte("p"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if string(got) != "mine" {
		t.Fatalf("got %q", got)
	}
}

func TestSendDataReplyError(t *testing.T) {
	cc := newCallChan(1, time.Second, time.Second)
	go func() {
		<-cc.PRpcCaller
		b, _ := fn.Encode(actions.ACTION_REPLY_ERROR, 5, []byte("boom"))
		if err := cc.Receive(context.Background(), b); err != nil {
			t.Errorf("receive: %v", err)
		}
	}()
	if _, err := cc.SendData(context.Background(), 5, []byte("p")); err == nil {
		t.Fatal("expected error for ACTION_REPLY_ERROR")
	} else if err.Error() != "boom" {
		t.Fatalf("unexpected err: %v", err)
	}
}

// 无人读取 PRpcCaller → 写阶段超时
func TestSendDataWriteTimeout(t *testing.T) {
	cc := newCallChan(0, 50*time.Millisecond, time.Second) // 无缓冲且无读者
	start := time.Now()
	_, err := cc.SendData(context.Background(), 1, []byte("p"))
	if err == nil || !strings.Contains(err.Error(), "call timeout") {
		t.Fatalf("expected call timeout, got %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("write timeout took %v, want ~50ms", d)
	}
}

// 有读取但无响应 → 读阶段超时
func TestSendDataReplyTimeout(t *testing.T) {
	cc := newCallChan(1, time.Second, 60*time.Millisecond)
	go func() { <-cc.PRpcCaller }() // 只取走请求，永不响应
	start := time.Now()
	_, err := cc.SendData(context.Background(), 1, []byte("p"))
	if err == nil || !strings.Contains(err.Error(), "reply timeout") {
		t.Fatalf("expected reply timeout, got %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("reply timeout took %v, want ~60ms", d)
	}
}

// 超时配置为 0 时必须走兜底值（否则 timer 立即触发，把正常调用判成超时）。
func TestSendDataZeroTimeoutFallback(t *testing.T) {
	cc := newCallChan(1, 0, 0)
	go func() {
		<-cc.PRpcCaller
		if err := cc.Receive(context.Background(), replyOK(t, 3, "ok")); err != nil {
			t.Errorf("receive: %v", err)
		}
	}()
	got, err := cc.SendData(context.Background(), 3, []byte("p"))
	if err != nil {
		t.Fatalf("zero timeout should fall back to default, got err: %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("got %q", got)
	}
}

// TestSendDataNoStaleTimeout 回归：写入阶段遗留的到期信号不得污染读取阶段。
//
// 原实现用 time.Ticker：写阶段结束后直接 Reset，Ticker 通道里已到期的信号
// 不会被排空，读取阶段立刻"假超时"（并发 RPC 下偶发 call timeout）。
// 这里让写阶段超时(5ms)小于响应延迟(80ms)，且写入本身瞬时完成：
// 新实现（Timer + 排空）必须成功，旧实现必然返回 reply timeout。
func TestSendDataNoStaleTimeout(t *testing.T) {
	for i := 0; i < 10; i++ {
		cc := newCallChan(0, 5*time.Millisecond, 3*time.Second)
		go func() {
			<-cc.PRpcCaller // 立即取走，写阶段瞬时完成
			time.Sleep(80 * time.Millisecond)
			if err := cc.Receive(context.Background(), replyOK(t, 11, "late-but-ok")); err != nil {
				t.Errorf("receive: %v", err)
			}
		}()
		got, err := cc.SendData(context.Background(), 11, []byte("p"))
		if err != nil {
			t.Fatalf("iter %d: stale timer leaked into read phase: %v", i, err)
		}
		if string(got) != "late-but-ok" {
			t.Fatalf("iter %d: got %q", i, got)
		}
	}
}

// ctx 取消必须立即返回，不等超时。
func TestSendDataContextCancel(t *testing.T) {
	cc := newCallChan(1, time.Second, 5*time.Second)
	go func() { <-cc.PRpcCaller }()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if _, err := cc.SendData(ctx, 1, []byte("p")); err == nil {
		t.Fatal("expected ctx error")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("ctx cancel took %v", d)
	}
}

// TestSendDataWakeupOnClose 连接关闭时必须立刻唤醒等待中的调用。
//
// 没有 CloseCalls 的话，调用方只能干等到 PReadWait 超时（默认 10s）才返回，
// 表现为"服务断连后业务卡死十几秒"。
func TestSendDataWakeupOnClose(t *testing.T) {
	cc := newCallChan(1, time.Second, 10*time.Second)
	go func() {
		<-cc.PRpcCaller
		time.Sleep(30 * time.Millisecond)
		cc.CloseCalls()
	}()
	start := time.Now()
	if _, err := cc.SendData(context.Background(), 1, []byte("p")); err == nil {
		t.Fatal("expected error after connection close")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("close wakeup took %v, want ~30ms（不能干等 10s 超时）", d)
	}
}

// TestSendDataConcurrentSameChannel 同一条连接并发多个调用，各自拿到自己的回包。
//
// 这是 P1 per-call 分发的正确性核心：改造前所有调用共用一个回包队列，
// 并发时调用者会读到别人的回包（只能丢弃再等），因此被迫全程加锁串行。
func TestSendDataConcurrentSameChannel(t *testing.T) {
	cc := newCallChan(256, 5*time.Second, 5*time.Second)
	// 模拟对端：取请求 → 按帧里的 id 回包（乱序投递，验证按 id 匹配）
	go func() {
		for payload := range cc.PRpcCaller {
			id := fn.Id(payload)
			frame, err := fn.Encode(actions.ACTION_REPLY_SUCCESS, id,
				fmt.Appendf(nil, "reply-%d", id))
			if err != nil {
				t.Errorf("encode: %v", err)
				return
			}
			if err := cc.Receive(context.Background(), frame); err != nil {
				t.Errorf("receive: %v", err)
				return
			}
		}
	}()

	const n = 64
	var wg sync.WaitGroup
	var failed atomic.Int64
	for i := 1; i <= n; i++ {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()
			payload, err := fn.Encode(actions.ACTION_CALL, id, []byte("ping"))
			if err != nil {
				t.Errorf("encode request: %v", err)
				failed.Add(1)
				return
			}
			got, err := cc.SendData(context.Background(), id, payload)
			if err != nil {
				t.Errorf("call %d: %v", id, err)
				failed.Add(1)
				return
			}
			if want := fmt.Sprintf("reply-%d", id); string(got) != want {
				t.Errorf("call %d got %q, want %q（拿到别人的回包）", id, got, want)
				failed.Add(1)
			}
		}(uint64(i))
	}
	wg.Wait()
	if failed.Load() != 0 {
		t.Fatalf("%d/%d concurrent calls failed", failed.Load(), n)
	}
}

// ---------------------------------------------------------------------------
// Benchmark
// ---------------------------------------------------------------------------

// 一次完整"发请求等响应"的开销（排除真实网络，只测协议与超时控制部分）
func BenchmarkSendData(b *testing.B) {
	cc := newCallChan(64, 5*time.Second, time.Second)
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-quit:
				return
			case payload := <-cc.PRpcCaller:
				frame, _ := fn.Encode(actions.ACTION_REPLY_SUCCESS, fn.Id(payload), []byte("pong"))
				if err := cc.Receive(context.Background(), frame); err != nil {
					return
				}
			}
		}
	}()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := cc.SendData(context.Background(), uint64(i+1), []byte("ping")); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	close(quit)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newCallChan 构造一条用于测试的 RPC 通道。
// writeCap 为待发请求队列容量（0 表示无缓冲，用于制造写超时）。
func newCallChan(writeCap int, writeWait, readWait time.Duration) *RpcChannel {
	cc := &RpcChannel{
		PRpcCaller: make(chan []byte, writeCap),
		PWriteWait: writeWait,
		PReadWait:  readWait,
	}
	cc.InitCalls()
	return cc
}

// replyOK 构造一条成功响应帧。
func replyOK(t *testing.T, id uint64, data string) []byte {
	t.Helper()
	b, err := fn.Encode(actions.ACTION_REPLY_SUCCESS, id, []byte(data))
	if err != nil {
		t.Fatalf("fn.Encode: %v", err)
	}
	return b
}
