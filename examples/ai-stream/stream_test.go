package main

// 端到端冒烟：一次 Call 拿到 sid，之后增量经 ws 推到客户端，最后收到 done。
//
// 这里刻意用**立刻吐完**的假上游（而不是 mock 的随机延迟），让测试既不慢也不 flaky；
// 它验证的是"发起 → 推流 → 结束"这条链路本身，上游换成真的不改变这段结构。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/logger"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/sloth/v4/utils"
)

const testAddr = "127.0.0.1:18994"

type fakeUp struct{ parts []string }

func (*fakeUp) Name() string { return "fake" }

func (f *fakeUp) Stream(ctx context.Context, prompt string) (<-chan string, error) {
	out := make(chan string, len(f.parts))
	for _, p := range f.parts {
		out <- p
	}
	close(out)
	return out, nil
}

// collector 客户端钩子：把推来的增量按 sid 攒起来，并记住是否收到 done。
type collector struct {
	mu   sync.Mutex
	text strings.Builder
	done chan struct{}
}

func newCollector() *collector { return &collector{done: make(chan struct{}, 1)} }

func (c *collector) OnConnect(ctx context.Context, resp *http.Response) error { return nil }
func (c *collector) OnReady(ctx context.Context, resp *http.Response, r types.IConnRpc, ch types.IConnInfo) error {
	return nil
}
func (c *collector) OnClose(ctx context.Context, resp *http.Response, r types.IConnRpc, ch types.IConnInfo) error {
	return nil
}
func (c *collector) OnError(ctx context.Context, resp *http.Response, r types.IConnRpc, ch types.IConnInfo, err error) error {
	return nil
}

func (c *collector) OnData(ctx context.Context, resp *http.Response, r types.IConnRpc, ch types.IConnInfo, msgType int, msg []byte) error {
	// 推送走的是 message.Msg：{"type":1,"body":"<base64>"}
	var envelope struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(msg, &envelope); err != nil {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(envelope.Body)
	if err != nil {
		return nil
	}
	var ev streamEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil
	}
	switch ev.Type {
	case eventDelta:
		c.mu.Lock()
		c.text.WriteString(ev.Text)
		c.mu.Unlock()
	case eventDone:
		select {
		case c.done <- struct{}{}:
		default:
		}
	}
	return nil
}

func (c *collector) Text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.text.String()
}

func TestChatStreamsOverWS(t *testing.T) {
	// 连接建立/关闭会产生日志，静音以免混进测试输出
	logger.SetOutput(io.Discard)
	defer logger.SetOutput(os.Stderr)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	svc := &ChatService{
		up:     &fakeUp{parts: []string{"你", "好", "，", "这", "是", "流", "式"}},
		cancel: map[string]context.CancelFunc{},
	}

	server := sloth.DefaultServer()
	svr := sloth.ServerConn(server)
	if err := svr.Register("ai", svc, "AI 流式对话"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := svr.Listen(ctx, sloth.WS, testAddr); err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = svr.Serve() }()
	defer svr.Close()

	col := newCollector()
	client := sloth.DefaultClient()
	cli := sloth.ClientConn(client)
	go func() {
		_ = cli.Dial(ctx, sloth.WS, testAddr, option.WithClientHandleMessage(col))
	}()

	// 等连接就绪：直接轮询调用，直到不报"没连上"为止
	ready := false
	for i := 0; i < 100 && !ready; i++ {
		if _, err := client.Call(ctx, "_.Funcs"); err == nil {
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("client not ready")
	}

	// ① 发起：只返回 sid，不等生成
	resp, err := client.Call(ctx, "ai.Chat", utils.Serialize(ChatReq{Prompt: "hi", Sid: "t1"}))
	if err != nil {
		t.Fatalf("ai.Chat: %v", err)
	}
	var cr ChatResp
	if err := json.Unmarshal(resp, &cr); err != nil {
		t.Fatalf("decode chat resp: %v (%s)", err, resp)
	}
	if cr.Sid != "t1" {
		t.Fatalf("sid = %q, want t1", cr.Sid)
	}

	// ② 增量 + 结束标记
	select {
	case <-col.done:
	case <-ctx.Done():
		t.Fatalf("timeout waiting done, got text=%q", col.Text())
	}
	if got := col.Text(); got != "你好，这是流式" {
		t.Fatalf("streamed text = %q, want %q", got, "你好，这是流式")
	}
}

// TestStopInterruptsStream 停止必须是"立刻生效"，而不是等上游吐完。
func TestStopInterruptsStream(t *testing.T) {
	logger.SetOutput(io.Discard)
	defer logger.SetOutput(os.Stderr)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 慢上游：每 200ms 吐一个字，总共要 4 秒
	svc := &ChatService{up: &slowUp{}, cancel: map[string]context.CancelFunc{}}

	server := sloth.DefaultServer()
	svr := sloth.ServerConn(server)
	if err := svr.Register("ai", svc, "AI 流式对话"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := svr.Listen(ctx, sloth.WS, "127.0.0.1:18995"); err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = svr.Serve() }()
	defer svr.Close()

	col := newCollector()
	client := sloth.DefaultClient()
	cli := sloth.ClientConn(client)
	go func() {
		_ = cli.Dial(ctx, sloth.WS, "127.0.0.1:18995", option.WithClientHandleMessage(col))
	}()

	ready := false
	for i := 0; i < 100 && !ready; i++ {
		if _, err := client.Call(ctx, "_.Funcs"); err == nil {
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("client not ready")
	}

	if _, err := client.Call(ctx, "ai.Chat", utils.Serialize(ChatReq{Prompt: "hi", Sid: "t2"})); err != nil {
		t.Fatalf("ai.Chat: %v", err)
	}
	// 收到第一段增量后立刻喊停
	for i := 0; i < 100 && col.Text() == ""; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := client.Call(ctx, "ai.Stop", "t2"); err != nil {
		t.Fatalf("ai.Stop: %v", err)
	}

	select {
	case <-col.done:
	case <-ctx.Done():
		t.Fatal("stop did not end the stream")
	}
	if n := len([]rune(col.Text())); n >= 20 {
		t.Fatalf("stream not interrupted: got %d runes", n)
	}
}

// slowUp 慢上游：每 200ms 一个字，用来验证 Stop 能提前掐断。
type slowUp struct{}

func (*slowUp) Name() string { return "slow" }

func (s *slowUp) Stream(ctx context.Context, prompt string) (<-chan string, error) {
	out := make(chan string, 1)
	go func() {
		defer close(out)
		for i := 0; i < 20; i++ {
			select {
			case <-ctx.Done():
				return
			case out <- "字":
			}
			time.Sleep(200 * time.Millisecond)
		}
	}()
	return out, nil
}
