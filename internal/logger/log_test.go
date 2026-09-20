package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuf 是并发安全的输出目标：日志写入在 logger 内部已串行，
// 但测试 goroutine 也会读它，故两端都加锁。
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (s *syncBuf) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.b.Reset()
}

// setup 把输出重定向到 buf 并复位全局状态，测试结束自动还原。
func setup(t *testing.T) *syncBuf {
	t.Helper()
	buf := &syncBuf{}
	oldLevel := Level()
	SetOutput(buf)
	SetLevel(Debug)
	SetFormat(FormatText)
	SetWriter(nil)
	t.Cleanup(func() {
		SetOutput(nil) // nil -> io.Discard，避免测试间互相污染 stderr
		SetLevel(oldLevel)
		SetFormat(FormatText)
		SetWriter(nil)
	})
	return buf
}

func TestLevelFilter(t *testing.T) {
	buf := setup(t)

	SetLevel(Error)
	Debugf(nil, "dbg")
	Infof(nil, "info")
	Warnw(nil, "warn")
	Errorf(nil, "err %d", 1)
	got := buf.String()
	if !strings.Contains(got, "ERR") {
		t.Fatalf("error log missing: %s", got)
	}
	if strings.Contains(got, "DBG") || strings.Contains(got, "INF") || strings.Contains(got, "WRN") {
		t.Fatalf("lower levels must be filtered out: %s", got)
	}

	buf.Reset()
	SetLevel(Debug)
	Debugf(nil, "dbg")
	if got := buf.String(); !strings.Contains(got, "DBG") {
		t.Fatalf("debug log missing after SetLevel(Debug): %s", got)
	}
}

// TestFormatArgs 覆盖此前最常出错的地方：占位符与参数必须匹配。
// 原代码里 log.Println(logger.Error, "...%v", err) 会把级别当普通参数打印。
func TestFormatArgs(t *testing.T) {
	buf := setup(t)

	Errorf(nil, "connect %s err: %v (failures:%d)", "ws://x", context.DeadlineExceeded, 7)
	got := buf.String()
	for _, want := range []string{"ws://x", "context deadline exceeded", "failures:7", "ERR"} {
		if !strings.Contains(got, want) {
			t.Errorf("log %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "%!") || strings.Contains(got, "MISSING") {
		t.Errorf("format placeholder leaked: %q", got)
	}
}

// TestDynamicMessageNotAFormat 动态文本（如 err.Error()）必须走结构化 kv，
// 不能直接当 format：错误文本里的 % 会被当成占位符。
func TestDynamicMessageNotAFormat(t *testing.T) {
	buf := setup(t)
	Errorw(nil, "read failed", "err", "bad %s input")
	got := buf.String()
	if strings.Contains(got, "%!") {
		t.Fatalf("percent in value must not be interpreted: %q", got)
	}
	if !strings.Contains(got, "bad %s input") {
		t.Fatalf("value lost: %q", got)
	}
}

func TestTracePropagation(t *testing.T) {
	buf := setup(t)

	ctx, id := EnsureTrace(context.Background())
	if id == "" {
		t.Fatal("EnsureTrace should generate a trace id")
	}
	if _, id2 := EnsureTrace(ctx); id2 != id {
		t.Fatalf("EnsureTrace must reuse existing id: %q vs %q", id2, id)
	}

	Infow(ctx, "hello")
	if got := buf.String(); !strings.Contains(got, "trace="+id) {
		t.Fatalf("trace id not in log: %s", got)
	}

	if a, b := NewTraceID(), NewTraceID(); a == b {
		t.Fatalf("trace id collision: %q", a)
	}
}

// TestFieldsCopyOnWrite 保证子 ctx 追加字段不会污染父 ctx（共享底层数组会串字段）。
func TestFieldsCopyOnWrite(t *testing.T) {
	buf := setup(t)

	parent := WithFields(context.Background(), "uid", 1)
	child := WithFields(parent, "ip", "1.2.3.4")

	Infow(parent, "parent")
	if got := buf.String(); strings.Contains(got, "ip=") {
		t.Fatalf("parent ctx polluted by child fields: %s", got)
	}

	buf.Reset()
	Infow(child, "child")
	got := buf.String()
	if !strings.Contains(got, "uid=1") || !strings.Contains(got, "ip=1.2.3.4") {
		t.Fatalf("child should carry both fields: %s", got)
	}
}

// TestOddFields 奇数个 kv 时补 MISSING，避免字段静默错位。
func TestOddFields(t *testing.T) {
	buf := setup(t)
	Infow(nil, "msg", "room", 1, "dangling")
	if got := buf.String(); !strings.Contains(got, "dangling=MISSING") {
		t.Fatalf("odd kv should be marked MISSING: %s", got)
	}
}

// TestQuoting 值中含空格/引号时必须加引号，否则单行日志无法被切分。
func TestQuoting(t *testing.T) {
	buf := setup(t)
	Infow(nil, "msg", "text", "hello world", "dur", 3*time.Millisecond, "err", context.Canceled)
	got := buf.String()
	// 含空格的值一律加引号（logfmt 风格），保证单行可被机械切分
	for _, want := range []string{`text="hello world"`, "dur=3ms", `err="context canceled"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestJSONFormat(t *testing.T) {
	buf := setup(t)
	SetFormat(FormatJSON)

	ctx := WithTraceID(context.Background(), "trace-1")
	Infow(ctx, "hello world", "room", 12, "ok", true)
	line := strings.TrimSpace(buf.String())

	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("log line is not valid json (%v): %s", err, line)
	}
	if m["level"] != "INF" || m["trace"] != "trace-1" || m["msg"] != "hello world" {
		t.Errorf("unexpected fields: %v", m)
	}
	if m["room"] != float64(12) || m["ok"] != true {
		t.Errorf("typed fields lost: %v", m)
	}
	if m["ts"] == "" || m["ts"] == nil {
		t.Errorf("ts missing: %v", m)
	}
}

// TestWriterHook 自定义后端应完全接管输出，且能拿到结构化字段。
func TestWriterHook(t *testing.T) {
	buf := setup(t)

	var (
		mu       sync.Mutex
		gotTrace string
		gotLevel LogLevel
		gotMsg   string
		gotKV    []any
	)
	SetWriter(testWriter(func(level LogLevel, trace, msg string, fields []any) {
		mu.Lock()
		defer mu.Unlock()
		gotLevel, gotTrace, gotMsg, gotKV = level, trace, msg, fields
	}))

	ctx := WithTraceID(context.Background(), "t-9")
	Errorw(ctx, "boom", "code", 500)
	if len(buf.String()) != 0 {
		t.Fatalf("builtin output must be bypassed, got: %s", buf.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if gotLevel != Error || gotTrace != "t-9" || gotMsg != "boom" || len(gotKV) != 2 {
		t.Fatalf("writer got: %v %q %q %v", gotLevel, gotTrace, gotMsg, gotKV)
	}
}

type testWriter func(level LogLevel, trace, msg string, fields []any)

func (f testWriter) Write(level LogLevel, trace, msg string, fields []any) { f(level, trace, msg, fields) }

// TestConcurrent 并发写日志，配合 -race 检查数据竞争。
func TestConcurrent(t *testing.T) {
	buf := setup(t)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := WithFields(context.Background(), "worker", i)
			for j := 0; j < 50; j++ {
				Infof(ctx, "tick %d", j)
			}
		}(i)
	}
	wg.Wait()

	// 每行必须是完整的一条：以时间戳开头、以换行结尾，且级别位置固定
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 16*50 {
		t.Fatalf("expected %d lines, got %d", 16*50, len(lines))
	}
	for _, l := range lines {
		if len(l) < 28 || !strings.Contains(l, " INF ") {
			t.Fatalf("interleaved/broken line: %q", l)
		}
	}
}

// TestTimestampFormat 校验手写的零填充时间格式（月份/毫秒补零最易写错）。
func TestTimestampFormat(t *testing.T) {
	ts := time.Date(2026, 9, 1, 2, 3, 4, 567891000, time.UTC)
	got := string(appendTime(nil, ts))
	want := "2026-09-01 02:03:04.567"
	if got != want {
		t.Fatalf("appendTime = %q, want %q", got, want)
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]LogLevel{
		"debug": Debug, "DEBUG": Debug,
		"info": Info, "": Info,
		"warn": Warning, "warning": Warning, "WRN": Warning,
		"error": Error, "ERR": Error,
		"unknown": Info,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}
