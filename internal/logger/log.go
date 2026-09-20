// Package logger 是 sloth 的统一日志门面。
//
// 背景：此前各包直接使用标准库 log，存在三类问题：
//   - 占位符与参数数量不匹配（日志里出现 "[]"），或误用 Println 传 format
//     （log.Println(logger.Error, "...%v", err) 把级别当普通参数打印，真实错误丢失）；
//   - 没有级别过滤，调试信息与错误混在一起，生产环境无法降噪；
//   - 没有 trace id，一次连接/一次调用产生的多条日志无法关联。
//
// 设计取舍：
//   - 级别判断发生在格式化之前，被过滤的级别零分配（热路径可直接调用 Debugf）；
//   - trace id 与结构化字段随 context 传播，业务函数只需透传 ctx；
//   - 输出目标/后端可替换（SetOutput / SetWriter），便于测试静音与对接 zap/slog。
package logger

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// LogLevel specifies the severity of a given log message
type LogLevel int

// Log levels
const (
	Debug LogLevel = iota
	Info
	Warning
	Error
	Max = iota - 1 // convenience - match highest log level
)

// String returns the string form for a given LogLevel
func (lvl LogLevel) String() string {
	switch lvl {
	case Info:
		return "INF"
	case Warning:
		return "WRN"
	case Error:
		return "ERR"
	}
	return "DBG"
}

// ParseLevel 解析级别名（大小写不敏感），未知值返回 Info。
// 便于从配置/环境变量设置级别：logger.SetLevel(logger.ParseLevel(os.Getenv("SLOTH_LOG_LEVEL")))
func ParseLevel(s string) LogLevel {
	switch s {
	case "debug", "DEBUG", "dbg":
		return Debug
	case "info", "INFO", "inf", "":
		return Info
	case "warn", "WARN", "warning", "WARNING", "wrn", "WRN":
		return Warning
	case "error", "ERROR", "err", "ERR":
		return Error
	}
	return Info
}

// Format 指定输出格式。
type Format int

const (
	// FormatText 人类可读单行：时间 级别 trace=xxx msg k=v k=v
	FormatText Format = iota
	// FormatJSON 单行 JSON，便于日志采集（ELK/Loki）直接解析。
	FormatJSON
)

// Logger 保留旧接口：与标准库 *log.Logger 的最小子集兼容。
type Logger interface {
	Output(calldepth int, s string) error
}

// Writer 是完全接管日志输出的后端（对接 zap / slog / 自建采集时用）。
// 设置后内置的格式化输出被旁路，trace 与 fields 原样传给后端。
//
// 实现必须是并发安全的：logger 不保证串行调用（Write 可能在任意 goroutine 触发）。
type Writer interface {
	Write(level LogLevel, trace string, msg string, fields []any)
}

// 全局状态。level/format/writer 走原子变量：读取在热路径上，不应加锁。
var (
	mu     sync.Mutex // 保护 out，保证单行输出不与其他 goroutine 交错
	out    io.Writer  = os.Stderr
	level  atomic.Int32
	format atomic.Int32
	writer atomic.Value // Writer
)

func init() {
	level.Store(int32(Info))
	format.Store(int32(FormatText))
}

// SetLevel 设置全局日志级别，低于该级别的日志被丢弃。
func SetLevel(lvl LogLevel) {
	level.Store(int32(lvl))
}

// Level 返回当前全局级别。
func Level() LogLevel { return LogLevel(level.Load()) }

// Enabled 报告 lvl 是否会被输出。热路径可用它包住昂贵的参数构造。
func Enabled(lvl LogLevel) bool { return lvl >= Level() }

// SetOutput 替换输出目标（默认 os.Stderr）。传 io.Discard 可静音（测试/benchmark 常用）。
func SetOutput(w io.Writer) {
	if w == nil {
		w = io.Discard
	}
	mu.Lock()
	out = w
	mu.Unlock()
}

// SetFormat 设置输出格式（默认文本）。
func SetFormat(f Format) { format.Store(int32(f)) }

// SetWriter 设置自定义后端；传 nil 恢复内置输出。
// 与 SetOutput 互斥：设置 Writer 后 SetOutput 不再生效。
func SetWriter(w Writer) {
	if w == nil {
		writer.Store((*holder)(nil))
		return
	}
	writer.Store(&holder{w: w})
}

type holder struct{ w Writer }

// ---------------------------------------------------------------------------
// trace id 与结构化字段（随 context 传播）
// ---------------------------------------------------------------------------

// TraceHeader 是跨进程传播 trace id 的头字段名：
// HTTP 握手请求头与 message.Header 均使用该键，服务端优先沿用客户端传入的 id。
const TraceHeader = "X-Trace-Id"

var (
	// 进程级前缀：纳秒时间戳低位 + pid，保证不同进程/重启后不与历史日志撞号。
	tracePrefix = strconv.FormatInt(time.Now().UnixNano()&0xFFFFFF, 16) + "-" + strconv.Itoa(os.Getpid())
	traceSeq    atomic.Uint64
)

// NewTraceID 生成一个新的短 trace id（进程内唯一，带进程前缀）。
func NewTraceID() string {
	return tracePrefix + "-" + strconv.FormatUint(traceSeq.Add(1), 36)
}

type logCtxKey struct{}

// logCtx 挂在 context 上，copy-on-write：派生时复制，读路径无锁无分配。
type logCtx struct {
	trace  string
	fields []any
}

func ctxOf(ctx context.Context) *logCtx {
	if ctx == nil {
		return nil
	}
	if lc, ok := ctx.Value(logCtxKey{}).(*logCtx); ok {
		return lc
	}
	return nil
}

// derive 复制一份 logCtx 并应用 fn，返回新的 context。
func derive(ctx context.Context, fn func(lc *logCtx)) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	base := ctxOf(ctx)
	lc := &logCtx{}
	if base != nil {
		*lc = *base
	}
	fn(lc)
	return context.WithValue(ctx, logCtxKey{}, lc)
}

// WithTraceID 在 ctx 上绑定 trace id（覆盖已有值，空 id 视为清空）。
func WithTraceID(ctx context.Context, id string) context.Context {
	return derive(ctx, func(lc *logCtx) { lc.trace = id })
}

// TraceID 返回 ctx 上的 trace id，不存在时返回空串。
func TraceID(ctx context.Context) string {
	if lc := ctxOf(ctx); lc != nil {
		return lc.trace
	}
	return ""
}

// EnsureTrace 保证 ctx 上已有 trace id：已有则原样返回，否则生成新的。
// 入口层（握手、RPC 发起、连接建立）调用一次即可让整条链路日志自动关联。
func EnsureTrace(ctx context.Context) (context.Context, string) {
	if id := TraceID(ctx); id != "" {
		return ctx, id
	}
	id := NewTraceID()
	return WithTraceID(ctx, id), id
}

// WithFields 追加结构化字段（交替的 key/value），其后所有日志自动携带。
// 典型用法：连接建立后 WithFields(ctx, "uid", uid, "ip", ip)，
// 该连接生命周期内的日志都会带上 uid/ip，无需每个调用点重复传。
//
// key 必须是 string；value 为任意类型（string/数字/bool/error/Duration 等有专门格式化）。
// kvs 长度为奇数时，最后一个 key 的值为 "MISSING"，避免静默错位。
func WithFields(ctx context.Context, kvs ...any) context.Context {
	if len(kvs) == 0 {
		return ctx
	}
	return derive(ctx, func(lc *logCtx) {
		// 复制底层数组，避免与父 ctx 共享后被 append 覆写
		merged := make([]any, 0, len(lc.fields)+len(kvs))
		merged = append(merged, lc.fields...)
		merged = append(merged, kvs...)
		lc.fields = merged
	})
}

// ---------------------------------------------------------------------------
// 输出 API
// ---------------------------------------------------------------------------

// 输出 API（f 后缀 = printf 风格，w 后缀 = msg + 结构化 kv 风格）。
// 之所以不用 Debug/Info 等裸名：它们已被级别常量占用。
//
// ctx 可为 nil，此时只输出消息本身（不带 trace 与上下文字段）。
func Debugf(ctx context.Context, format string, args ...any) { logf(Debug, ctx, format, args...) }
func Infof(ctx context.Context, format string, args ...any)  { logf(Info, ctx, format, args...) }
func Warnf(ctx context.Context, format string, args ...any)  { logf(Warning, ctx, format, args...) }
func Errorf(ctx context.Context, format string, args ...any) { logf(Error, ctx, format, args...) }

func Debugw(ctx context.Context, msg string, kvs ...any) { logw(Debug, ctx, msg, kvs...) }
func Infow(ctx context.Context, msg string, kvs ...any)  { logw(Info, ctx, msg, kvs...) }
func Warnw(ctx context.Context, msg string, kvs ...any)  { logw(Warning, ctx, msg, kvs...) }
func Errorw(ctx context.Context, msg string, kvs ...any) { logw(Error, ctx, msg, kvs...) }

// Logf / Logw 供"级别由调用方动态决定"的适配层使用（如 Connect.Log(lvl, ...)）。
func Logf(lvl LogLevel, ctx context.Context, format string, args ...any) { logf(lvl, ctx, format, args...) }
func Logw(lvl LogLevel, ctx context.Context, msg string, kvs ...any)    { logw(lvl, ctx, msg, kvs...) }

// 兼容旧调用（无 ctx、Println 语义）：级别过滤同样生效。
func Debugln(args ...any) { logw(Debug, nil, sprint(args...)) }
func Infoln(args ...any)  { logw(Info, nil, sprint(args...)) }
func Warnln(args ...any)  { logw(Warning, nil, sprint(args...)) }
func Errorln(args ...any) { logw(Error, nil, sprint(args...)) }

func sprint(args ...any) string { return fmt.Sprintln(args...) }

func logf(lvl LogLevel, ctx context.Context, format string, args ...any) {
	if !Enabled(lvl) {
		return
	}
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	emit(lvl, ctx, msg, nil)
}

func logw(lvl LogLevel, ctx context.Context, msg string, kvs ...any) {
	if !Enabled(lvl) {
		return
	}
	emit(lvl, ctx, msg, kvs)
}

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 256)
		return &b
	},
}

func emit(lvl LogLevel, ctx context.Context, msg string, kvs []any) {
	lc := ctxOf(ctx)
	var trace string
	var fields []any
	if lc != nil {
		trace, fields = lc.trace, lc.fields
	}
	all := kvs
	if len(fields) > 0 {
		// 上下文字段在前，调用点字段在后（后者可覆盖同名的前者语义，由采集端决定）
		all = make([]any, 0, len(fields)+len(kvs))
		all = append(all, fields...)
		all = append(all, kvs...)
	}

	if v := writer.Load(); v != nil {
		if h, ok := v.(*holder); ok && h != nil {
			h.w.Write(lvl, trace, msg, all)
			return
		}
	}

	bufp := bufPool.Get().(*[]byte)
	buf := (*bufp)[:0]

	now := time.Now()
	if Format(format.Load()) == FormatJSON {
		buf = appendJSONLine(buf, now, lvl, trace, msg, all)
	} else {
		buf = appendTextLine(buf, now, lvl, trace, msg, all)
	}

	mu.Lock()
	_, _ = out.Write(buf)
	mu.Unlock()

	// 只放回合理大小的缓冲，避免偶发超长日志长期占用池
	if cap(buf) <= 64<<10 {
		*bufp = buf
		bufPool.Put(bufp)
	}
}

// ---------------------------------------------------------------------------
// 文本格式：2026-09-20 12:34:56.789 INF trace=xxx-1 msg="..." k=v k=v
//
// 消息放在字段之前并带 msg= 键：行尾是最后一个字段，
// 追加字段时不会把消息挤到中间，且 grep "msg=" 就能定位。
// ---------------------------------------------------------------------------

func appendTextLine(dst []byte, t time.Time, lvl LogLevel, trace, msg string, kvs []any) []byte {
	dst = appendTime(dst, t)
	dst = append(dst, ' ')
	dst = append(dst, lvl.String()...)
	dst = append(dst, ' ', 't', 'r', 'a', 'c', 'e', '=')
	if trace == "" {
		dst = append(dst, '-')
	} else {
		dst = append(dst, trace...)
	}
	dst = append(dst, ' ', 'm', 's', 'g', '=')
	dst = appendTextValue(dst, msg)
	for i := 0; i+1 < len(kvs); i += 2 {
		dst = append(dst, ' ')
		dst = appendField(dst, kvs[i], kvs[i+1])
	}
	// 奇数个 kv：补一个显式占位，避免字段静默丢失
	if len(kvs)%2 == 1 {
		dst = append(dst, ' ')
		dst = appendField(dst, kvs[len(kvs)-1], "MISSING")
	}
	dst = append(dst, '\n')
	return dst
}

func appendField(dst []byte, k, v any) []byte {
	dst = appendTextValue(dst, fieldKey(k))
	dst = append(dst, '=')
	return appendTextValue(dst, v)
}

func fieldKey(k any) string {
	if s, ok := k.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", k)
}

// appendTime 输出 2006-01-02 15:04:05.000。
// 手写而非 t.Format：Format 会分配字符串（每次日志 1 次堆分配）。
func appendTime(dst []byte, t time.Time) []byte {
	y, m, d := t.Date()
	hh, mm, ss := t.Clock()
	dst = appendIntWidth(dst, y, 4)
	dst = append(dst, '-')
	dst = appendIntWidth(dst, int(m), 2)
	dst = append(dst, '-')
	dst = appendIntWidth(dst, d, 2)
	dst = append(dst, ' ')
	dst = appendIntWidth(dst, hh, 2)
	dst = append(dst, ':')
	dst = appendIntWidth(dst, mm, 2)
	dst = append(dst, ':')
	dst = appendIntWidth(dst, ss, 2)
	dst = append(dst, '.')
	dst = appendIntWidth(dst, t.Nanosecond()/1e6, 3)
	return dst
}

func appendIntWidth(dst []byte, v, width int) []byte {
	for i := width - 1; i > 0; i-- {
		if v/pow10(i) == 0 {
			dst = append(dst, '0')
		} else {
			break
		}
	}
	return strconv.AppendInt(dst, int64(v), 10)
}

func pow10(n int) int {
	v := 1
	for i := 0; i < n; i++ {
		v *= 10
	}
	return v
}

// appendTextValue 格式化字段值：含空格/引号/等号时加引号，保证单行可被简单切分。
func appendTextValue(dst []byte, v any) []byte {
	switch x := v.(type) {
	case nil:
		return append(dst, "null"...)
	case string:
		return appendQuotedIfNeeded(dst, x)
	case []byte:
		return appendQuotedIfNeeded(dst, string(x))
	case bool:
		return strconv.AppendBool(dst, x)
	case int:
		return strconv.AppendInt(dst, int64(x), 10)
	case int8:
		return strconv.AppendInt(dst, int64(x), 10)
	case int16:
		return strconv.AppendInt(dst, int64(x), 10)
	case int32:
		return strconv.AppendInt(dst, int64(x), 10)
	case int64:
		return strconv.AppendInt(dst, x, 10)
	case uint:
		return strconv.AppendUint(dst, uint64(x), 10)
	case uint8:
		return strconv.AppendUint(dst, uint64(x), 10)
	case uint16:
		return strconv.AppendUint(dst, uint64(x), 10)
	case uint32:
		return strconv.AppendUint(dst, uint64(x), 10)
	case uint64:
		return strconv.AppendUint(dst, x, 10)
	case float32:
		return strconv.AppendFloat(dst, float64(x), 'g', -1, 32)
	case float64:
		return strconv.AppendFloat(dst, x, 'g', -1, 64)
	case time.Duration:
		return appendQuotedIfNeeded(dst, x.String())
	case time.Time:
		return appendQuotedIfNeeded(dst, x.Format(time.RFC3339))
	case error:
		return appendQuotedIfNeeded(dst, x.Error())
	case fmt.Stringer:
		return appendQuotedIfNeeded(dst, x.String())
	}
	return appendQuotedIfNeeded(dst, fmt.Sprintf("%v", v))
}

func needsQuote(s string) bool {
	if s == "" {
		return true
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '"', '=', '\n', '\r', '\t':
			return true
		}
	}
	return false
}

func appendQuotedIfNeeded(dst []byte, s string) []byte {
	if !needsQuote(s) {
		return append(dst, s...)
	}
	dst = append(dst, '"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			dst = append(dst, '\\', '"')
		case '\\':
			dst = append(dst, '\\', '\\')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		case '\t':
			dst = append(dst, '\\', 't')
		default:
			dst = append(dst, c)
		}
	}
	return append(dst, '"')
}

// ---------------------------------------------------------------------------
// JSON 格式：{"ts":"...","level":"INF","trace":"...","msg":"...","k":"v"}
// ---------------------------------------------------------------------------

func appendJSONLine(dst []byte, t time.Time, lvl LogLevel, trace, msg string, kvs []any) []byte {
	dst = append(dst, '{')
	dst = append(dst, `"ts":"`...)
	dst = appendTime(dst, t)
	dst = append(dst, '"', ',')
	dst = append(dst, `"level":"`...)
	dst = append(dst, lvl.String()...)
	dst = append(dst, '"', ',')
	dst = append(dst, `"trace":"`...)
	dst = appendJSONEscape(dst, trace)
	dst = append(dst, '"', ',')
	dst = append(dst, `"msg":"`...)
	dst = appendJSONEscape(dst, msg)
	dst = append(dst, '"')
	for i := 0; i+1 < len(kvs); i += 2 {
		dst = append(dst, ',', '"')
		dst = appendJSONEscape(dst, fieldKey(kvs[i]))
		dst = append(dst, '"', ':')
		dst = appendJSONValue(dst, kvs[i+1])
	}
	if len(kvs)%2 == 1 {
		dst = append(dst, ',', '"')
		dst = appendJSONEscape(dst, fieldKey(kvs[len(kvs)-1]))
		dst = append(dst, '"', ':', '"', 'M', 'I', 'S', 'S', 'I', 'N', 'G', '"')
	}
	dst = append(dst, '}', '\n')
	return dst
}

// appendJSONValue：数字/bool 不加引号，其余按字符串输出。
func appendJSONValue(dst []byte, v any) []byte {
	switch x := v.(type) {
	case nil:
		return append(dst, "null"...)
	case bool:
		return strconv.AppendBool(dst, x)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return appendTextValue(dst, x) // 数字无需转义，直接输出
	default:
		dst = append(dst, '"')
		dst = appendJSONEscape(dst, fmt.Sprintf("%v", v))
		return append(dst, '"')
	}
}

// appendJSONEscape 只处理 JSON 必须转义的字符（不依赖 internal/utils，避免包依赖倒置）。
func appendJSONEscape(dst []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			dst = append(dst, '\\', '"')
		case c == '\\':
			dst = append(dst, '\\', '\\')
		case c == '\n':
			dst = append(dst, '\\', 'n')
		case c == '\r':
			dst = append(dst, '\\', 'r')
		case c == '\t':
			dst = append(dst, '\\', 't')
		case c < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hexDigit(c>>4), hexDigit(c&0xf))
		default:
			dst = append(dst, c)
		}
	}
	return dst
}

func hexDigit(b byte) byte {
	if b < 10 {
		return '0' + b
	}
	return 'a' + b - 10
}
