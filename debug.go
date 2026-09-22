package sloth

import (
	"context"
	"io"
	"net/http"

	"github.com/w6xian/sloth/v4/logger"
	"github.com/w6xian/sloth/v4/metrics"
)

// ---------------------------------------------------------------------------
// 调试端点
// ---------------------------------------------------------------------------

// DebugHandler 返回调试端点路由，可直接挂到自有 HTTP 服务：
//
//	mux := http.NewServeMux()
//	mux.Handle("/debug/", sloth.DebugHandler())
//
// 包含：
//   - /debug/metrics：Prometheus 文本格式指标（连接数、RPC 耗时、广播丢弃等）
//   - /debug/pprof/*：CPU/堆/goroutine 采样
//   - /debug/vars：expvar（含 memstats）
//
// 端点无鉴权，勿直接暴露公网。
func DebugHandler() http.Handler { return metrics.Handler() }

// ServeDebug 在独立地址异步启动调试服务，返回 *http.Server 以便关闭。
//
// 这是生产环境的推荐用法：调试端口与业务端口分离，
// 只需监听内网/回环地址，不必在公网网关上放行。
//
//	srv, _ := sloth.ServeDebug("127.0.0.1:6060")
//	defer srv.Close()
func ServeDebug(addr string) (*http.Server, error) { return metrics.Serve(addr) }

// DebugPaths 返回调试端点路径列表，便于网关配置白名单。
func DebugPaths() []string { return metrics.Paths() }

// ---------------------------------------------------------------------------
// 日志
// ---------------------------------------------------------------------------

// SetLogLevel 设置全局日志级别，取值 "debug"/"info"/"warn"/"error"（大小写不敏感）。
// 默认 info：debug 级日志（如方法注册明细）不输出。
func SetLogLevel(level string) { logger.SetLevel(logger.ParseLevel(level)) }

// SetLogOutput 替换日志输出目标。传 io.Discard 可静音。
func SetLogOutput(w io.Writer) { logger.SetOutput(w) }

// SetLogJSON 切换日志为 JSON 单行输出（默认人类可读文本）。
// 日志进 ELK/Loki 时开启，便于直接解析字段。
func SetLogJSON(enabled bool) {
	if enabled {
		logger.SetFormat(logger.FormatJSON)
		return
	}
	logger.SetFormat(logger.FormatText)
}

// TraceHeader 是跨进程传播 trace id 的头字段名（message.Header 与 HTTP 头通用）。
// 服务端会自动沿用客户端通过它传入的 trace id，缺失时本地生成。
const TraceHeader = logger.TraceHeader

// WithTraceID 在 ctx 上绑定 trace id，其后所有日志自动携带。
func WithTraceID(ctx context.Context, id string) context.Context {
	return logger.WithTraceID(ctx, id)
}

// TraceID 返回 ctx 上的 trace id，不存在时返回空串。
func TraceID(ctx context.Context) string { return logger.TraceID(ctx) }

// NewTraceID 生成一个新的短 trace id。
func NewTraceID() string { return logger.NewTraceID() }

// EnsureTrace 保证 ctx 上带 trace id：已有则沿用，没有则生成。
// 返回的新 ctx 与 trace id，可用于把同一次业务的多条日志串起来。
func EnsureTrace(ctx context.Context) (context.Context, string) { return logger.EnsureTrace(ctx) }

// ---------------------------------------------------------------------------
// 日志输出
// ---------------------------------------------------------------------------
//
// 两族 API，按需选一个即可：
//   - Xxxf(ctx, format, args...)：printf 风格，适合"一句话里嵌变量"
//   - Xxxw(ctx, msg, kvs...)：消息 + 结构化键值对（kvs 必须成对），
//     适合日志进 ELK/Loki 后被按字段检索（如 err=、userId=）
//
// ctx 必须传（实在没有就传 context.Background()）：
// 库已在握手、RPC 入口把 trace id 放进 ctx，只有传 ctx 日志才会带上 trace=xxx，
// 否则这条日志无法与同一次调用/同一条连接的其它日志对账。
//
// 动态文本（尤其是 err.Error()、用户输入）务必走 Xxxw 的 kv，不要拼进 format：
// 文本里含 % 会被当成占位符，输出成 %!s(MISSING)。

func Debugf(ctx context.Context, format string, args ...any) { logger.Debugf(ctx, format, args...) }
func Infof(ctx context.Context, format string, args ...any)  { logger.Infof(ctx, format, args...) }
func Warnf(ctx context.Context, format string, args ...any)  { logger.Warnf(ctx, format, args...) }
func Errorf(ctx context.Context, format string, args ...any) { logger.Errorf(ctx, format, args...) }

func Debugw(ctx context.Context, msg string, kvs ...any) { logger.Debugw(ctx, msg, kvs...) }
func Infow(ctx context.Context, msg string, kvs ...any)  { logger.Infow(ctx, msg, kvs...) }
func Warnw(ctx context.Context, msg string, kvs ...any)  { logger.Warnw(ctx, msg, kvs...) }
func Errorw(ctx context.Context, msg string, kvs ...any) { logger.Errorw(ctx, msg, kvs...) }
