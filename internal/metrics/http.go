package metrics

import (
	"context"
	"expvar"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"runtime"
	"time"
)

func goroutines() int { return runtime.NumGoroutine() }

// Paths 返回调试端点支持的路径，便于宿主路由做前缀挂载或网关白名单。
func Paths() []string {
	return []string{
		"/debug/metrics",
		"/debug/vars",
		"/debug/pprof/",
		"/debug/pprof/cmdline",
		"/debug/pprof/profile",
		"/debug/pprof/symbol",
		"/debug/pprof/trace",
	}
}

// Handler 返回调试端点的路由。
//
// 可直接 http.ListenAndServe，也可挂到宿主路由：
//
//	router.PathPrefix("/debug/").Handler(metrics.Handler())
//
// 内层用独立 ServeMux：宿主路由（如 gorilla/mux）转发时不会剥离前缀，
// 因此这里注册的路径仍以 /debug/ 开头，与直接访问时的路径一致。
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		// 采集端提前断开（抓取超时）是常态，写失败无需处理
		_ = WritePrometheus(w)
	})
	// expvar 自带 memstats / cmdline，内存细节无需另行实现
	mux.Handle("/debug/vars", expvar.Handler())

	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	// 兜底索引：列出可用端点，避免 /debug/ 404 后不知道拼什么
	mux.HandleFunc("/debug/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "sloth debug endpoints:")
		for _, p := range Paths() {
			fmt.Fprintln(w, "  "+p)
		}
	})
	return mux
}

// newDebugServer 构造带超时保护的调试用 http.Server。
// pprof 采集可能长时间占用连接（profile 默认 30s），因此不设整体 WriteTimeout，
// 只限制请求头读取与空闲连接，避免慢速连接占满资源。
func newDebugServer(addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

// Serve 在独立地址异步启动调试服务，返回 *http.Server 以便调用方优雅关闭。
//
// 推荐监听内网/回环地址：调试端点无鉴权，暴露公网等于开放 goroutine 栈与堆采样。
func Serve(addr string) (*http.Server, error) {
	srv := newDebugServer(addr)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	go func() { _ = srv.Serve(ln) }()
	return srv, nil
}

// ListenAndServe 阻塞式启动调试服务（通常放在 goroutine 里）。
func ListenAndServe(addr string) error {
	srv := newDebugServer(addr)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return srv.Serve(ln)
}

// Shutdown 优雅关闭 Serve 返回的调试服务。
func Shutdown(ctx context.Context, srv *http.Server) error {
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}
