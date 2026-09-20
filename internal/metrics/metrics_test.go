package metrics

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCounterAndGauge(t *testing.T) {
	c := NewCounter("test_counter_total", "test counter")
	c.Inc()
	c.Add(4)
	if got := c.Value(); got != 5 {
		t.Fatalf("counter = %d, want 5", got)
	}

	g := NewGauge("test_gauge", "test gauge")
	g.Set(10)
	g.Dec()
	if got := g.Value(); got != 9 {
		t.Fatalf("gauge = %d, want 9", got)
	}
}

// TestRegisterReuse 同名同类指标应复用（多实例共享一个时间序列），
// 避免每次构造都往注册表里塞新项。
func TestRegisterReuse(t *testing.T) {
	a := NewCounter("test_reuse_total", "reuse")
	b := NewCounter("test_reuse_total", "reuse")
	if a != b {
		t.Fatal("same name+type must return the same instance")
	}
	a.Inc()
	if b.Value() != 1 {
		t.Fatalf("reused counter must share state, got %d", b.Value())
	}
}

func TestGaugeFunc(t *testing.T) {
	var v int64 = 42
	g := NewGaugeFunc("test_gauge_func", "gauge func", func() float64 { return float64(v) })
	var buf bytes.Buffer
	if err := g.WriteMetrics(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "test_gauge_func 42") {
		t.Fatalf("gauge func output wrong: %s", buf.String())
	}
}

func TestHistogram(t *testing.T) {
	h := NewHistogram("test_hist", "test histogram", []float64{0.001, 0.01, 0.1})
	h.Observe(500 * time.Microsecond)  // 落 0.001 桶
	h.Observe(5 * time.Millisecond)    // 落 0.01 桶
	h.Observe(2 * time.Second)         // 落 +Inf 溢出桶

	counts, count, sum := h.Snapshot()
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
	if want := []uint64{1, 1, 0, 1}; !equalCounts(counts, want) {
		t.Fatalf("buckets = %v, want %v", counts, want)
	}
	if sum < 2.005 || sum > 2.006 {
		t.Fatalf("sum = %v, want ~2.0055", sum)
	}
}

func equalCounts(got, want []uint64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestHistogramOutput 校验 Prometheus histogram 文本格式：
// 桶计数必须累计（le 递增时计数不递减）。
func TestHistogramOutput(t *testing.T) {
	h := NewHistogram("test_hist_out", "hist out", []float64{0.001, 0.01})
	h.Observe(2 * time.Millisecond) // 只落 0.01 桶：le=0.001 计数 0，le=0.01 计数 1

	var buf bytes.Buffer
	if err := h.WriteMetrics(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		`# TYPE test_hist_out histogram`,
		`test_hist_out_bucket{le="0.001"} 0`,
		`test_hist_out_bucket{le="0.01"} 1`,
		`test_hist_out_bucket{le="+Inf"} 1`,
		`test_hist_out_count 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWritePrometheus(t *testing.T) {
	NewCounter("test_prom_counter", "prom counter").Inc()
	var buf bytes.Buffer
	if err := WritePrometheus(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"# TYPE test_prom_counter counter",
		"test_prom_counter 1",
		"# TYPE sloth_goroutines gauge",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestDebugHandler 验证三个端点都能访问（pprof 与 expvar 接线的关键点）。
func TestDebugHandler(t *testing.T) {
	NewCounter("test_http_counter", "http counter").Inc()
	h := Handler()

	for _, path := range []string{"/debug/metrics", "/debug/pprof/", "/debug/vars", "/debug/"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200", path, rec.Code)
			continue
		}
		if path == "/debug/metrics" && !strings.Contains(rec.Body.String(), "test_http_counter") {
			t.Errorf("/debug/metrics missing registered metric: %s", rec.Body.String())
		}
	}
}

// TestConcurrentObserve 并发埋点，配合 -race 检查竞争与计数准确性。
func TestConcurrentObserve(t *testing.T) {
	c := NewCounter("test_concurrent_counter", "concurrent")
	h := NewHistogram("test_concurrent_hist", "concurrent", nil)

	var wg sync.WaitGroup
	const workers, per = 8, 100
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < per; j++ {
				c.Inc()
				h.Observe(time.Millisecond)
			}
		}()
	}
	wg.Wait()

	if got := c.Value(); got != workers*per {
		t.Fatalf("counter = %d, want %d", got, workers*per)
	}
	if _, count, _ := h.Snapshot(); count != uint64(workers*per) {
		t.Fatalf("histogram count = %d, want %d", count, workers*per)
	}
}

// TestServeAndShutdown 验证独立端口能起来并优雅关闭。
func TestServeAndShutdown(t *testing.T) {
	srv, err := Serve("127.0.0.1:0")
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	defer func() {
		if err := srv.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()
	if srv.Addr == "" {
		t.Fatal("server addr empty")
	}
}
