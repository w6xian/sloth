package main

// AI 对话流式的服务端样例：把"一次提问 + 持续增量"映射成 sloth 的
// 「一次 RPC 立即返回 + 多次定向推送」，前端只做一件事——往气泡里追加文本。
//
// 为什么不用 SSE：
//   1. 增量只是"服务端 → 浏览器"这一半，另一半（停止生成、续接上下文）SSE 得
//      另开一个 HTTP 请求，ws 上就是同一条连接上的一次普通 RPC；
//   2. 不受 HTTP/1.1 每域 6 连接限制（多开几个标签页就看得出来）；
//   3. 不用额外起 HTTP 端点、不用 Flusher，连接配额 / 指标 / 重连全是现成的。
//
// 结构（这是本样例唯一必须照搬的部分）：
//
//	浏览器 ──Call("ai.Chat", {prompt})──▶ 服务端
//	                                       │ 立即返回 {sid}，【不等待生成结束】
//	                                       ▼
//	                              goroutine 读上游（mock / OpenAI）
//	浏览器 ◀──Push 增量 {sid,t:"delta"}───┘
//	浏览器 ◀──Push 结束 {sid,t:"done"}────┘
//
// 关键约束：**绝不能在 RPC 方法里阻塞等整段生成完**。一次 Call 是"发一帧、等一个
// 回包"，等回包有 PReadWait（默认 10s）兜底，生成半分钟必然超时断连，而这期间前端
// 一个字都收不到。所以方法只负责开一个 goroutine 并立刻返回 sid。
//
// 运行：
//   go run ./examples/ai-stream          # 默认 mock 上游，无需任何 key
//   go run ./examples/ai-stream -upstream=openai -key=$OPENAI_API_KEY
//
// 然后浏览器打开 http://localhost:8012 ，输入任意内容点发送。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/utils"
)

const (
	rpcAddr   = "localhost:8994" // sloth ws 监听地址
	webAddr   = "localhost:8012" // 静态页面地址（与 RPC 端口分开）
	debugAddr = "127.0.0.1:6064"

	// 攒批窗口与阈值：token 流每秒几十条，逐条推会把连接队列打满并丢帧
	// （丢了就是前端少一段字，极难排查）。攒一小批再推，观感一致、帧数少一个量级。
	flushEvery  = 60 * time.Millisecond
	flushMinLen = 256

	// pushTimeout 单条推送的超时。它只是防止 goroutine 永久卡在写队列上，
	// 不是"生成超时"——生成耗时由前端自行决定要不要 Stop。
	pushTimeout = 3 * time.Second
)

func main() {
	up := flag.String("upstream", "mock", "上游生成源：mock 或 openai")
	base := flag.String("base", "https://api.openai.com", "OpenAI 兼容端点（含 /v1/chat/completions）")
	model := flag.String("model", "gpt-4o-mini", "模型名")
	key := flag.String("key", os.Getenv("OPENAI_API_KEY"), "上游 API Key（默认取环境变量 OPENAI_API_KEY）")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sloth.SetLogLevel("info")

	svc, err := newChatService(*up, upstreamConfig{Base: *base, Model: *model, Key: *key})
	if err != nil {
		sloth.Errorw(ctx, "init chat service failed", "err", err)
		return
	}

	server := sloth.DefaultServer()
	drpc := sloth.ServerConn(server, sloth.WithDebugAddr(debugAddr))
	if err := drpc.Register("ai", svc, "AI 流式对话"); err != nil {
		sloth.Errorw(ctx, "register service failed", "err", err)
		return
	}

	if err := drpc.Listen(ctx, sloth.WS, rpcAddr,
		// 只换 router、不额外挂 DefaultServeMux：静态页面走另一个 http.Server。
		option.WithRouterWithoutHandle(mux.NewRouter()),
		option.WithOrigin("*"),
		// 队列默认 10，攒批后仍可能突发（长回答 + 多会话并发），放宽到 64。
		option.WithChannelQueueSize(64)); err != nil {
		sloth.Errorw(ctx, "listen failed", "err", err)
		return
	}

	web := serveWeb(ctx)
	defer func() { _ = web.Close() }()

	serveErr := make(chan error, 1)
	go func() { serveErr <- drpc.Serve() }()

	sloth.Infow(ctx, "ai-stream server started",
		"ws", "ws://"+rpcAddr+"/ws", "web", "http://"+webAddr, "upstream", svc.upstreamName())

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
	// 关闭服务会连带取消所有在途生成（svc.Close）。
	svc.Close()
	if err := drpc.Close(); err != nil {
		sloth.Errorw(ctx, "close server failed", "err", err)
	}
}

// ───────────────────────────────────────────────────────────────────────────
// 服务实现
// ───────────────────────────────────────────────────────────────────────────

// streamEvent 是推送给浏览器的增量帧，JSON 放在 Push 的 body 里。
//
// 为什么类型写在 body 里而不是用 CmdReq 的 action：前端 JS 客户端对非标准 action
// 只把整帧原样抛给 onmessage，拿不到 action 字段；把 sid 与类型都放进 body，
// 前端一次 JSON.parse 就能同时知道"这帧属于哪个会话"和"是增量还是结束"。
type streamEvent struct {
	Sid   string `json:"sid"`
	Type  string `json:"t"`              // delta | done | err
	Text  string `json:"text,omitempty"` // 本次增量文本（delta）
	Error string `json:"err,omitempty"`  // 错误信息（err）
}

const (
	eventDelta = "delta"
	eventDone  = "done"
	eventErr   = "err"
)

// ChatReq ai.Chat 的入参。
type ChatReq struct {
	Prompt string `json:"prompt"`
	Sid    string `json:"sid,omitempty"` // 前端生成，用于多路会话并行；留空则由服务端分配
}

// ChatResp ai.Chat 的回包：立刻返回，不等生成。
type ChatResp struct {
	Sid string `json:"sid"`
}

// ChatService 服务对象在所有连接间共享（Register 传的是同一个实例指针），
// 而 RPC 并发到达，所以这里的会话表必须加锁。
type ChatService struct {
	up upstream

	seq    atomic.Uint64
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	closed bool
}

func newChatService(kind string, cfg upstreamConfig) (*ChatService, error) {
	var up upstream
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "mock":
		up = &mockUpstream{}
	case "openai":
		if cfg.Key == "" {
			return nil, errors.New("openai upstream requires -key or OPENAI_API_KEY")
		}
		up = &openaiUpstream{cfg: cfg, hc: &http.Client{Timeout: 0}}
	default:
		return nil, fmt.Errorf("unknown upstream: %s (want mock|openai)", kind)
	}
	return &ChatService{up: up, cancel: map[string]context.CancelFunc{}}, nil
}

func (s *ChatService) upstreamName() string { return s.up.Name() }

// Chat 发起一次生成：登记会话、起 goroutine、立刻返回 sid。
func (s *ChatService) Chat(ctx context.Context, req *ChatReq) ([]byte, error) {
	// 定向推送用的是"发起调用的那条连接"，不需要 userId，也就不需要先 Sign：
	// 谁的提问就推给谁，天然不会串到别的标签页。
	ch, err := sloth.GetChannel(ctx)
	if err != nil {
		return nil, fmt.Errorf("channel not found: %w", err)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("empty prompt")
	}

	sid := strings.TrimSpace(req.Sid)
	if sid == "" {
		sid = fmt.Sprintf("s%d", s.seq.Add(1))
	}

	runCtx, cancel := context.WithCancel(context.Background())
	if !s.track(sid, cancel) {
		cancel()
		return nil, errors.New("server is shutting down")
	}
	go s.run(runCtx, ch, sid, req.Prompt)

	return utils.Serialize(ChatResp{Sid: sid}), nil
}

// Stop 中断一次生成。这是 ws 相对 SSE 最实在的好处：停止就是一次普通 RPC，
// 不用另开 HTTP 请求，也不用等上游超时。
func (s *ChatService) Stop(ctx context.Context, sid string) ([]byte, error) {
	if s.stop(strings.TrimSpace(sid)) {
		return utils.Serialize(map[string]any{"sid": sid, "stopped": true}), nil
	}
	return utils.Serialize(map[string]any{"sid": sid, "stopped": false}), nil
}

// Close 结束服务时取消所有在途生成，避免 goroutine 挂在已关闭的连接上。
func (s *ChatService) Close() {
	s.mu.Lock()
	s.closed = true
	cancels := make([]context.CancelFunc, 0, len(s.cancel))
	for sid, c := range s.cancel {
		delete(s.cancel, sid)
		cancels = append(cancels, c)
	}
	s.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

func (s *ChatService) track(sid string, cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.cancel[sid] = cancel
	return true
}

func (s *ChatService) stop(sid string) bool {
	s.mu.Lock()
	c, ok := s.cancel[sid]
	if ok {
		delete(s.cancel, sid)
	}
	s.mu.Unlock()
	if ok {
		c()
	}
	return ok
}

func (s *ChatService) forget(sid string) {
	s.mu.Lock()
	if c, ok := s.cancel[sid]; ok {
		delete(s.cancel, sid)
		c()
	}
	s.mu.Unlock()
}

// run 读上游并把增量推给浏览器。三条退出路径：上游读完、被 Stop、连接已断。
func (s *ChatService) run(ctx context.Context, ch bucket.IChannel, sid, prompt string) {
	defer s.forget(sid)

	tokens, err := s.up.Stream(ctx, prompt)
	if err != nil {
		_ = s.push(ch, streamEvent{Sid: sid, Type: eventErr, Error: err.Error()})
		return
	}

	var buf strings.Builder
	flush := func() bool {
		if buf.Len() == 0 {
			return true
		}
		ev := streamEvent{Sid: sid, Type: eventDelta, Text: buf.String()}
		buf.Reset()
		return s.push(ch, ev) == nil
	}

	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()

	stopped := false
loop:
	for {
		select {
		case <-ctx.Done():
			stopped = true
			break loop
		case tk, ok := <-tokens:
			if !ok {
				break loop
			}
			buf.WriteString(tk)
			if buf.Len() >= flushMinLen && !flush() {
				return // 连接已断：剩余内容没有接收方了，直接收摊
			}
		case <-ticker.C:
			if !flush() {
				return
			}
		}
	}

	// 收尾：把残留的一段推完，再给一个结束标记。
	// 被 Stop 时也发 done——前端据此收起"生成中"状态，语义上是"这一轮结束了"。
	if buf.Len() > 0 {
		_ = s.push(ch, streamEvent{Sid: sid, Type: eventDelta, Text: buf.String()})
	}
	_ = s.push(ch, streamEvent{Sid: sid, Type: eventDone})
	if stopped {
		sloth.Infow(ctx, "stream stopped", "sid", sid)
	}
}

// push 推一条增量。返回 error 表示这条连接已经不可用（关闭或队列持续打满），
// 调用方应停止生成而不是继续往里写。
func (s *ChatService) push(ch bucket.IChannel, ev streamEvent) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	pctx, cancel := context.WithTimeout(context.Background(), pushTimeout)
	defer cancel()
	return ch.Push(pctx, message.NewTextMessage(b))
}

// ───────────────────────────────────────────────────────────────────────────
// 上游：mock 与 OpenAI 转发
// ───────────────────────────────────────────────────────────────────────────

// upstream 一个"会吐增量文本"的源。换成 Claude / 本地 vLLM 之类的，只需再实现它。
type upstream interface {
	Name() string
	Stream(ctx context.Context, prompt string) (<-chan string, error)
}

type upstreamConfig struct {
	Base  string
	Model string
	Key   string
}

// mockUpstream 本地假上游：把一段回答按标点切成小块，随机间隔吐出。
// 用它可以在没有任何 Key 的情况下验证"堆内容"的整条链路。
type mockUpstream struct{}

func (*mockUpstream) Name() string { return "mock" }

var mockAnswer = []string{
	"可以把流式回答拆成两半来看：", "一半是「怎么把增量送到浏览器」，", "另一半是「上游怎么把增量吐出来」。",
	"前者用一条长连接就够——", "一次调用发起，之后服务端按帧往下推，", "前端每收到一帧就往气泡里追加一段。",
	"后者取决于你的上游：", "OpenAI / Claude / 本地模型现在都提供流式接口，", "只是封帧格式不同（SSE、gRPC stream、WebSocket）。",
	"中间这一层只做三件事：", "把上游的帧翻译成自己的帧、", "按会话 ID 分发、", "以及给前端一个能随时喊停的入口。",
	"剩下的诸如重试、限速、计费，", "都挂在这一层两侧，与流式本身无关。",
}

func (m *mockUpstream) Stream(ctx context.Context, prompt string) (<-chan string, error) {
	out := make(chan string, 8)
	go func() {
		defer close(out)
		head := fmt.Sprintf("（mock 上游）你问的是：%s。", strings.TrimSpace(prompt))
		for _, piece := range append([]string{head}, mockAnswer...) {
			for _, r := range piece {
				select {
				case <-ctx.Done():
					return
				case out <- string(r):
				}
				// 20~70ms 的抖动，贴近真实 token 到达节奏
				time.Sleep(time.Duration(20+rand.Intn(50)) * time.Millisecond)
			}
		}
	}()
	return out, nil
}

// openaiUpstream 转发 OpenAI 兼容的 /v1/chat/completions（stream=true），
// 把 SSE 的 `data: {...}` 逐条转成增量文本。
type openaiUpstream struct {
	cfg upstreamConfig
	hc  *http.Client
}

func (*openaiUpstream) Name() string { return "openai" }

func (u *openaiUpstream) Stream(ctx context.Context, prompt string) (<-chan string, error) {
	body, err := json.Marshal(map[string]any{
		"model":    u.cfg.Model,
		"stream":   true,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(u.cfg.Base, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+u.cfg.Key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// 不用 hc.Timeout：生成多久由调用方决定（可以随时 Stop），超时会掐断正常回答。
	resp, err := u.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close()
		msg := make([]byte, 256)
		n, _ := resp.Body.Read(msg)
		return nil, fmt.Errorf("upstream http %d: %s", resp.StatusCode, strings.TrimSpace(string(msg[:n])))
	}

	out := make(chan string, 64)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				if data == "[DONE]" {
					return
				}
				continue
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil || len(chunk.Choices) == 0 {
				continue
			}
			if c := chunk.Choices[0].Delta.Content; c != "" {
				select {
				case out <- c:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// ───────────────────────────────────────────────────────────────────────────
// 静态页面
// ───────────────────────────────────────────────────────────────────────────

func serveWeb(ctx context.Context) *http.Server {
	m := http.NewServeMux()
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(pickDir("examples/ai-stream/web", "web", "."), "index.html"))
	})
	// JS 客户端复用 examples/ws/web 下现成的 bundle，避免样例里再拷一份。
	jsDir := pickDir("examples/ws/web", "../ws/web")
	m.Handle("/web/", http.StripPrefix("/web/", http.FileServer(http.Dir(jsDir))))

	srv := &http.Server{Addr: webAddr, Handler: m, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			sloth.Errorw(ctx, "web server exited", "err", err)
		}
	}()
	return srv
}

// pickDir 返回第一个存在的目录：样例既可能从仓库根跑（go run ./examples/ai-stream），
// 也可能在样例目录里跑（go run .），两个相对路径都要能work。
func pickDir(cands ...string) string {
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	return cands[0]
}
