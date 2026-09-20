// Package metrics 提供零第三方依赖的基础指标与调试端点。
//
// 覆盖三件事：
//   - 指标：Counter / Gauge / GaugeFunc / Histogram，全部基于原子操作，无锁；
//   - 暴露：Prometheus 文本格式（/debug/metrics），可被 Prometheus 直接抓取；
//   - 调试：net/http/pprof（/debug/pprof/*）与 expvar（/debug/vars）。
//
// 用法一（推荐生产）：独立端口监听，调试端点不对外暴露在业务端口：
//
//	go metrics.ListenAndServe("127.0.0.1:6060")
//
// 用法二：挂到已有 WebSocket 路由上（如内网环境）：
//
//	router.PathPrefix("/debug/").Handler(metrics.Handler())
//
// 注意：/debug/pprof/profile 会阻塞采集 CPU（默认 30s），
// 暴露在公网前必须加鉴权或仅监听内网地址。
package metrics

import (
	"fmt"
	"io"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Metric 是注册表项：名称、说明与 Prometheus 文本输出。
type Metric interface {
	Name() string
	Help() string
	WriteMetrics(w io.Writer) error
}

// ---------------------------------------------------------------------------
// 注册表
// ---------------------------------------------------------------------------

var (
	regMu    sync.RWMutex
	regOrder []string
	regMap   = make(map[string]Metric, 32)
)

// register 登记指标。同名的同类指标直接复用（包级变量在多实例场景下共享），
// 同名但类型不同说明调用方写错了名字，直接 panic（发生在启动阶段，便于早暴露）。
func register(m Metric) Metric {
	regMu.Lock()
	defer regMu.Unlock()
	if m.Name() == "" {
		panic("metrics: empty metric name")
	}
	if old, ok := regMap[m.Name()]; ok {
		if fmt.Sprintf("%T", old) == fmt.Sprintf("%T", m) {
			return old
		}
		panic("metrics: duplicate name with different type: " + m.Name())
	}
	regMap[m.Name()] = m
	regOrder = append(regOrder, m.Name())
	return m
}

// ---------------------------------------------------------------------------
// Counter：单调递增计数（错误数、调用数、累计广播数）
// ---------------------------------------------------------------------------

type Counter struct {
	name string
	help string
	v    atomic.Int64
}

// NewCounter 创建（或复用同名）计数器。name 可带 label：room_total{room="1"}。
func NewCounter(name, help string) *Counter {
	c := &Counter{name: name, help: help}
	if old, ok := register(c).(*Counter); ok {
		return old
	}
	return c
}

func (c *Counter) Name() string { return c.name }
func (c *Counter) Help() string { return c.help }

// Inc 加 1。
func (c *Counter) Inc() { c.v.Add(1) }

// Add 增加 delta。
func (c *Counter) Add(delta int64) { c.v.Add(delta) }

// Value 返回当前值。
func (c *Counter) Value() int64 { return c.v.Load() }

func (c *Counter) WriteMetrics(w io.Writer) error {
	return writeSample(w, c.name, c.help, "counter", c.name, float64(c.v.Load()))
}

// ---------------------------------------------------------------------------
// Gauge：可增可减的瞬时值（在线数、队列长度）
// ---------------------------------------------------------------------------

type Gauge struct {
	name string
	help string
	v    atomic.Int64
}

func NewGauge(name, help string) *Gauge {
	g := &Gauge{name: name, help: help}
	if old, ok := register(g).(*Gauge); ok {
		return old
	}
	return g
}

func (g *Gauge) Name() string { return g.name }
func (g *Gauge) Help() string { return g.help }
func (g *Gauge) Set(v int64)  { g.v.Store(v) }
func (g *Gauge) Add(delta int64) { g.v.Add(delta) }
func (g *Gauge) Inc()         { g.v.Add(1) }
func (g *Gauge) Dec()         { g.v.Add(-1) }
func (g *Gauge) Value() int64 { return g.v.Load() }

func (g *Gauge) WriteMetrics(w io.Writer) error {
	return writeSample(w, g.name, g.help, "gauge", g.name, float64(g.v.Load()))
}

// ---------------------------------------------------------------------------
// GaugeFunc：拉模式取值（每次被抓取时调用 fn）
//
// 适合"当前 map 长度"这类值：埋点式维护要在 Put/Delete 两处对称记账，
// 一旦漏记就会永久漂移；回调只在采集时执行一次，且不会侵入热路径。
// fn 必须并发安全，且不可反向访问注册表（会死锁）。
// ---------------------------------------------------------------------------

type GaugeFunc struct {
	name string
	help string
	fn   func() float64
}

func NewGaugeFunc(name, help string, fn func() float64) *GaugeFunc {
	g := &GaugeFunc{name: name, help: help, fn: fn}
	if old, ok := register(g).(*GaugeFunc); ok {
		return old
	}
	return g
}

func (g *GaugeFunc) Name() string { return g.name }
func (g *GaugeFunc) Help() string { return g.help }

func (g *GaugeFunc) WriteMetrics(w io.Writer) error {
	return writeSample(w, g.name, g.help, "gauge", g.name, g.fn())
}

// ---------------------------------------------------------------------------
// Histogram：分布（RPC 耗时、广播耗时）
// ---------------------------------------------------------------------------

// DefaultBounds 默认耗时分桶（单位秒）：0.5ms 起步覆盖到 5s，
// 兼顾 RPC（毫秒级）与广播/重连（秒级）两类场景。
var DefaultBounds = []float64{
	0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5,
}

type Histogram struct {
	name   string
	help   string
	bounds []float64
	counts []atomic.Uint64 // len(bounds) 个桶 + 末尾 +Inf 溢出桶
	count  atomic.Uint64
	sum    atomic.Uint64 // float64 bits
}

// NewHistogram 创建直方图；bounds 必须升序，传 nil 使用 DefaultBounds。
func NewHistogram(name, help string, bounds []float64) *Histogram {
	if len(bounds) == 0 {
		bounds = DefaultBounds
	}
	cp := make([]float64, len(bounds))
	copy(cp, bounds)
	sort.Float64s(cp)
	h := &Histogram{name: name, help: help, bounds: cp, counts: make([]atomic.Uint64, len(cp)+1)}
	if old, ok := register(h).(*Histogram); ok {
		return old
	}
	return h
}

func (h *Histogram) Name() string { return h.name }
func (h *Histogram) Help() string { return h.help }

// Observe 记录一次耗时。
func (h *Histogram) Observe(d time.Duration) { h.ObserveFloat(d.Seconds()) }

// ObserveFloat 记录一个浮点观测值（单位由调用方约定）。
func (h *Histogram) ObserveFloat(v float64) {
	// 二分定位第一个上界 >= v 的桶；超出所有上界时落入 +Inf 溢出桶
	i := sort.SearchFloat64s(h.bounds, v)
	if i > len(h.bounds) {
		i = len(h.bounds)
	}
	h.counts[i].Add(1)
	h.count.Add(1)
	// float64 无法直接原子加，用 CAS 累加
	for {
		old := h.sum.Load()
		next := math.Float64bits(math.Float64frombits(old) + v)
		if h.sum.CompareAndSwap(old, next) {
			return
		}
	}
}

// Snapshot 返回快照：counts 为各桶累计（含 +Inf 溢出桶），以及总次数与总和。
func (h *Histogram) Snapshot() (counts []uint64, count uint64, sum float64) {
	counts = make([]uint64, len(h.counts))
	for i := range h.counts {
		counts[i] = h.counts[i].Load()
	}
	return counts, h.count.Load(), math.Float64frombits(h.sum.Load())
}

func (h *Histogram) WriteMetrics(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s histogram\n", h.name, h.help, h.name); err != nil {
		return err
	}
	counts, count, sum := h.Snapshot()
	var cum uint64
	for i, b := range h.bounds {
		cum += counts[i]
		if _, err := fmt.Fprintf(w, "%s_bucket{le=\"%g\"} %d\n", h.name, b, cum); err != nil {
			return err
		}
	}
	cum += counts[len(h.bounds)]
	if _, err := fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", h.name, cum); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s_sum %g\n%s_count %d\n", h.name, sum, h.name, count); err != nil {
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Prometheus 文本输出
// ---------------------------------------------------------------------------

// writeSample 输出 "# HELP / # TYPE / 样本" 三行。typ 为 Prometheus 类型名。
func writeSample(w io.Writer, name, help, typ, sampleName string, v float64) error {
	if help != "" {
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n", name, help); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "# TYPE %s %s\n", name, typ); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s %g\n", sampleName, v)
	return err
}

// WritePrometheus 将所有已注册指标按注册顺序输出为 Prometheus 文本格式。
func WritePrometheus(w io.Writer) error {
	regMu.RLock()
	names := make([]string, len(regOrder))
	copy(names, regOrder)
	regMu.RUnlock()

	for _, name := range names {
		regMu.RLock()
		m, ok := regMap[name]
		regMu.RUnlock()
		if !ok {
			continue
		}
		if err := m.WriteMetrics(w); err != nil {
			return err
		}
	}
	return writeProcessMetrics(w)
}

// writeProcessMetrics 附加进程级指标（goroutine 数等）。
// 内存细节交给 /debug/vars（expvar 自带 memstats），此处只放最常被告警的几个。
func writeProcessMetrics(w io.Writer) error {
	now := time.Now()
	if err := writeSample(w, "sloth_goroutines", "current goroutine count", "gauge", "sloth_goroutines", float64(goroutines())); err != nil {
		return err
	}
	return writeSample(w, "sloth_scrape_timestamp_seconds", "unix timestamp of this scrape",
		"gauge", "sloth_scrape_timestamp_seconds", float64(now.UnixNano())/1e9)
}
