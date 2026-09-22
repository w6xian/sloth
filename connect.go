package sloth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v4/errs"

	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/decoder"
	"github.com/w6xian/sloth/v4/logger"
	"github.com/w6xian/sloth/v4/metrics"
	"github.com/w6xian/sloth/v4/ref"
	"github.com/w6xian/sloth/v4/utils/id"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/sloth/v4/types/auth"
	"github.com/w6xian/sloth/v4/types/trpc"
)

type ContextType string

const (
	HeaderKey  = ContextType("nrpc_header")
	ChannelKey = ContextType("nrpc_channel")
	BucketKey  = ContextType("nrpc_bucket")
)

// Protocol 网络协议类型
type Protocol string

const (
	ProtocolHTTP      Protocol = "http" // HTTP/WebSocket (默认)
	ProtocolWebSocket Protocol = "ws"   // WebSocket
	ProtocolWSS       Protocol = "wss"  // WebSocket over TLS
	ProtocolTCP       Protocol = "tcp"  // TCP (TODO)
	ProtocolQUIC      Protocol = "quic" // QUIC (TODO)
	ProtocolGRPC      Protocol = "grpc" // gRPC (TODO)
)

// ServeHandler HTTP 处理函数接口
type ServeHandler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// implements [trpc.ICallRpc]
type Connect struct {
	// id         int64
	ServerId   string
	client     *ClientRpc
	server     *ServerRpc
	serviceMap map[string]*ref.ServiceFuncs
	// serviceMapMu 保护 serviceMap 的并发读写（Register 与 CallFunc/IsRegisteredService）
	serviceMapMu sync.RWMutex
	sleepTimes   int
	times        int
	cpuNum       int
	tlsConfig    *tls.Config
	Option       *option.Options
	protocols    map[string]ProtocolFactory

	// 多协议监听器
	listeners []ProtocolListener
	// httpHandlers []ServeHandler // HTTP 处理函数列表
	proxyHandler func(ctx context.Context, service string) (int64, error)
	// meta data
	metaData string
	// debugSrv 为 Option.DebugAddr 启动的调试服务（nil 表示未启用）
	debugSrv *http.Server

	// serveWg 跟踪 Serve() 拉起的监听 goroutine，serving 标记 Serve 是否已启动。
	// Close() 原先只关 listener 就返回，这些 goroutine 可能还卡在 http.Server.Serve
	// 上（进程收尾时资源不回收，测试里表现为收不到退出信号）。
	serveWg sync.WaitGroup
	serving atomic.Bool
}

func (c *Connect) CallNetFunc(ctx context.Context, r *http.Request, service string, msgId uint64, msg []byte) ([]byte, error) {
	if c.proxyHandler == nil {
		return nil, errors.New("service not set")
	}
	proxyService, err := c.proxyHandler(ctx, service)
	if err != nil {
		return nil, err
	}
	data, err := c.client.CallNet(ctx, proxyService, msgId, msg)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (c *Connect) UseProxyHandler(proxyHandler func(ctx context.Context, service string) (int64, error)) error {
	c.proxyHandler = proxyHandler
	return nil
}

func (c *Connect) IsRegisteredService(service string) bool {
	// 每次 RPC 调用都会走到这里：strings.Split 会分配一个 []string，
	// 而这里只需要 "service.method" 的第一段，用 Cut 零分配即可。
	name, _, ok := strings.Cut(service, ".")
	if !ok || name == "" {
		return false
	}
	c.serviceMapMu.RLock()
	_, ok = c.serviceMap[name]
	c.serviceMapMu.RUnlock()
	return ok
}

func (c *Connect) Options() *option.Options {
	return c.Option
}

// TLSConfig 返回连接上配置的 TLS 配置（未配置时为 nil）。
//
// 传输层会读它：wss 用它做 ServeTLS，QUIC 更是离不开——QUIC 的加密由
// TLS 1.3 承担，没有证书连握手都完成不了。
func (c *Connect) TLSConfig() *tls.Config {
	return c.tlsConfig
}

func (c *Connect) RegisterProtocol(name string, factory ProtocolFactory) {
	if c.protocols == nil {
		c.protocols = make(map[string]ProtocolFactory)
	}
	c.protocols[name] = factory
}

func (c *Connect) resolveProtocol(name string) ProtocolFactory {
	if c.protocols == nil {
		return nil
	}
	if factory, ok := c.protocols[name]; ok {
		return factory
	}
	// 实例表未命中时回查全局表：此前两套注册机制完全不互通，
	// 通过全局 RegisterProtocol 注册的实现永远不会被用到（死代码）。
	return ResolveProtocol(name)
}

// ServerConn 建服务端连接：参数是由 DefaultServer() 得到的 *ClientRpc。
// 名字指**本进程扮演的角色**（这里是服务端），参数类型里的 Client 指调用目标，
// 两者不是同一层含义，详见 ClientRpc / ServerRpc 的注释。
func ServerConn(client *ClientRpc, opts ...ConnOption) *Connect {
	opts = append(opts, Client(client))
	return newConnect(opts...)
}

// ClientConn 建客户端连接：参数是由 DefaultClient() 得到的 *ServerRpc。
// 名字指本进程扮演的角色（这里是客户端），参数类型里的 Server 指调用目标。
func ClientConn(client *ServerRpc, opts ...ConnOption) *Connect {
	opts = append(opts, Server(client))
	return newConnect(opts...)
}

// newConnect 创建一个连接
// 请用 ServerConn 或 ClientConn 创建连接
func newConnect(opts ...ConnOption) *Connect {
	svr := new(Connect)
	// svr.id = atomic.AddInt64(&instCount, 1)
	svr.ServerId = id.ShortID()
	svr.serviceMap = make(map[string]*ref.ServiceFuncs)
	svr.sleepTimes = 15
	svr.times = 8
	svr.cpuNum = runtime.NumCPU()
	svr.client = LinkClientFunc()
	svr.server = LinkServerFunc()
	svr.Option = option.NewOptions()
	svr.protocols = snapshotProtocols()
	svr.listeners = make([]ProtocolListener, 0)
	svr.proxyHandler = func(ctx context.Context, service string) (int64, error) {
		return 0, nil
	}

	for _, opt := range opts {
		opt(svr)
	}
	svr.applyLogOptions()

	return svr
}

// applyLogOptions 应用日志级别：显式选项优先，其次环境变量 SLOTH_LOG_LEVEL，都没有则保持默认 info。
func (c *Connect) applyLogOptions() {
	lvl := ""
	if c.Option != nil {
		lvl = c.Option.LogLevel
	}
	if lvl == "" {
		lvl = os.Getenv("SLOTH_LOG_LEVEL")
	}
	if lvl != "" {
		logger.SetLevel(logger.ParseLevel(lvl))
	}
}

// startDebugServer 按 Option.DebugAddr 启动独立调试服务（metrics / pprof / vars）。
// 独立于业务端口：调试端点无鉴权，不应与对外服务共用监听地址。
func (c *Connect) startDebugServer() error {
	if c.Option == nil || c.Option.DebugAddr == "" {
		return nil
	}
	srv, err := metrics.Serve(c.Option.DebugAddr)
	if err != nil {
		return fmt.Errorf("start debug server on %s: %w", c.Option.DebugAddr, err)
	}
	c.debugSrv = srv
	c.Log(logger.Info, "debug server listening on %s (metrics=/debug/metrics, pprof=/debug/pprof/)", c.Option.DebugAddr)
	return nil
}

// Register 注册一个服务，name是服务名，rcvr是服务实现，metadata是服务描述
func (c *Connect) Register(name string, rcvr any, metadata string) error {
	c.serviceMapMu.Lock()
	defer c.serviceMapMu.Unlock()
	if _, ok := c.serviceMap[name]; ok {
		return fmt.Errorf("service %s already registered", name)
	}
	funcs := ref.Register(rcvr)
	c.metaData = metadata
	c.serviceMap[name] = funcs
	return nil
}

// Listen 注册协议监听器，不立即启动服务
// 可以多次调用注册多个协议，最后用 Serve() 启动所有服务
func (c *Connect) Listen(ctx context.Context, network, address string, opts ...option.ConnectOption) error {
	factory := c.resolveProtocol(network)
	if factory == nil {
		return fmt.Errorf("unsupported network type: %s", network)
	}
	// 底层监听：默认 TCP（ws/wss/tcp 都跑在 TCP 上，wss 的 TLS 由
	// http.Server.ServeTLS 在握手阶段接管，因此这里始终是明文 listener）；
	// QUIC 走 ListenerFactory，自己造 UDP 监听器。
	ln, err := c.makeListenerFor(factory, address)
	if err != nil {
		return err
	}
	// 传输实例由工厂创建：此前这里只判断 factory.Name()=="ws" 然后走 ws 硬编码，
	// CreateServer 从未被调用——抽象在只有一个实现时完全没被使用过。
	srv, err := c.transportInstance(factory, ctx, address, opts...)
	if err != nil {
		_ = ln.Close()
		return err
	}

	c.listeners = append(c.listeners, ProtocolListener{
		Network:  network,
		Address:  address,
		Context:  ctx,
		Listener: ln,
		Server:   srv,
		Options:  opts,
	})
	if c.client != nil {
		c.client.setServe(c.serveInstance())
	}
	runtime.GOMAXPROCS(c.cpuNum)
	c.Log(logger.Info, "registered %s listener on %s", factory.Name(), address)
	return nil
}

// serveInstance 返回对外暴露的传输实例。
//
// 只有一个传输时就是它本身（保持原有类型，调用方若做类型断言也不受影响）；
// 多个传输时返回合成实例——否则每次 Listen 覆盖 ClientRpc.Serve，
// 只有最后注册的协议能收到服务端推送（见 multiServer 的说明）。
func (c *Connect) serveInstance() types.IServer {
	seen := make(map[types.IServer]struct{}, len(c.listeners))
	list := make([]types.IServer, 0, len(c.listeners))
	for _, l := range c.listeners {
		if l.Server == nil {
			continue
		}
		if _, ok := seen[l.Server]; ok {
			continue
		}
		seen[l.Server] = struct{}{}
		list = append(list, l.Server)
	}
	if len(list) == 1 {
		return list[0]
	}
	return newMultiServer(list...)
}

// transportInstance 取得该协议的传输实例（没有才创建）。
func (c *Connect) transportInstance(factory ProtocolFactory, ctx context.Context, address string, opts ...option.ConnectOption) (types.IServer, error) {
	// 同一协议的多条监听地址共用同一个传输实例：ws/wss 原本就共用一个
	// WsServer（含它的 mux 路由与 bucket 体系），分成两个实例会让广播只覆盖一半。
	if srv := c.existingTransport(factory.Name()); srv != nil {
		return srv, nil
	}
	return factory.CreateServer(ctx, c, address, opts...)
}

func (c *Connect) existingTransport(name string) types.IServer {
	for _, l := range c.listeners {
		if l.Server == nil {
			continue
		}
		if f := c.resolveProtocol(l.Network); f != nil && f.Name() == name {
			return l.Server
		}
	}
	return nil
}

// newHTTPServer 构造带超时保护的 http.Server。
//
// 背景：http.Serve(ln, h) 内部使用零值 http.Server，没有任何超时限制，
// 慢速客户端（slowloris）可以长期占住连接直到耗尽服务端资源。
// 这里只设置握手阶段与空闲阶段的超时：
//   - ReadHeaderTimeout：限制请求头读取，防御慢速头攻击；
//   - IdleTimeout：限制 keep-alive 空闲连接存活时间。
//
// 不设置 ReadTimeout/WriteTimeout：连接一旦被 Upgrade（Hijack）就不再受其约束，
// 而普通 HTTP 请求若被截断会带来难以预期的问题，故交由业务侧按需配置。
func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

// secureNetwork 该网络是否在监听器之上还要做 TLS 握手（目前只有 wss/https）。
//
// 独立于 Connect.tlsConfig 判断：同一份 TLS 配置可能同时供 QUIC 使用，
// 不能因为"配了 TLS"就把 ws / tcp 端口也变成 TLS。
func secureNetwork(network string) bool {
	switch strings.ToLower(strings.TrimSpace(network)) {
	case "wss", "https":
		return true
	}
	return false
}

// isServeErr 判断监听循环的退出错误是否值得上报给 Serve 的调用方。
//
// Close() 关掉 listener 后，Accept / http.Server.Serve 一律返回 net.ErrClosed，
// 这是**预期的退出路径**而不是故障。多协议同时监听时这类错误会有 N 条
// （每个 listener 一条），不滤掉的话正常关闭也会被当成错误返回。
func isServeErr(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed)
}

// Serve 启动所有注册的协议监听器
// 阻塞直到所有服务停止
func (c *Connect) Serve() error {
	if len(c.listeners) == 0 {
		return errors.New("no listeners registered, call Listen() first")
	}
	// 调试服务是附属能力（metrics / pprof），不是业务链路的一部分：
	// 端口被占用（同一台机器上跑多个实例时很常见）不该让整个服务起不来，
	// 因此这里只降级为告警——此前它会让 Serve() 直接返回错误、进程退出。
	if err := c.startDebugServer(); err != nil {
		c.Log(logger.Warning, "debug server disabled: %v", err)
	}

	// 每个 listener 起一个 goroutine，按传输实例的能力分派：
	//   - HTTP 型传输（ws）：用实例自己的 Handler 起 http.Server，TLS 交给 ServeTLS；
	//   - 流式传输（tcp）：由实例自己 Accept。
	// 此前这里按 network 字符串硬编码 ws 分支，其它协议的 listener 会静默空转
	// （goroutine 直接跑完、没人 Accept 端口）——第二实现才逼出这个默认分支。
	var wg = &c.serveWg
	errChan := make(chan error, len(c.listeners))
	// 标记"已启动"，供 Close() 判断是否需要等待这些 goroutine 退出
	c.serving.Store(true)

	for _, l := range c.listeners {
		wg.Add(1)
		go func(listener ProtocolListener) {
			defer wg.Done()
			// listener 结束（通常是 Close）后清理传输实例：关闭活跃连接与 bucket worker。
			// 以前 WsServer 从不关闭，进程内残留的连接与 worker 只能等 GC。
			defer func() {
				if cl, ok := listener.Server.(interface{ Close() error }); ok {
					_ = cl.Close()
				}
			}()
			runCtx := listener.Context
			if runCtx == nil {
				runCtx = context.Background()
			}
			_ = runCtx

			if listener.Server == nil {
				errChan <- fmt.Errorf("listener %s: no transport instance", listener.Network)
				return
			}
			if h, ok := listener.Server.(interface{ Handler() http.Handler }); ok {
				c.Log(logger.Info, "starting %s server on %s", listener.Network, listener.Address)
				// 用显式 http.Server 并设置超时（http.Serve 用的是零值 Server，无任何超时保护）
				srv := newHTTPServer(h.Handler())
				var err error
				if secureNetwork(listener.Network) {
					// TLS 两个来源：显式 *tls.Config（WithTLSConfig，可与 QUIC 共用同一份）
					// 或证书文件（WithTLSCertKey）。都没有才报错。
					switch {
					case c.tlsConfig != nil:
						srv.TLSConfig = c.tlsConfig.Clone()
						err = srv.ServeTLS(listener.Listener, "", "")
					default:
						certFile := strings.TrimSpace(c.Option.TLSCertFile)
						keyFile := strings.TrimSpace(c.Option.TLSKeyFile)
						if certFile == "" || keyFile == "" {
							errChan <- fmt.Errorf("wss requires TLS cert/key file, set WithTLSCertKey(certFile, keyFile) or WithTLSConfig(...)")
							return
						}
						err = srv.ServeTLS(listener.Listener, certFile, keyFile)
					}
				} else {
					err = srv.Serve(listener.Listener)
				}
				if isServeErr(err) {
					errChan <- err
				}
				return
			}
			if t, ok := listener.Server.(interface{ Serve(net.Listener) error }); ok {
				c.Log(logger.Info, "starting %s server on %s", listener.Network, listener.Address)
				if err := t.Serve(listener.Listener); isServeErr(err) {
					errChan <- err
				}
				return
			}
			errChan <- fmt.Errorf("listener %s: transport implements neither Handler() nor Serve()", listener.Network)
		}(l)
	}

	// 等待所有服务结束
	go func() {
		wg.Wait()
		close(errChan)
	}()

	// 返回第一个错误（如果有）
	for err := range errChan {
		if err != nil {
			return err
		}
	}
	return nil
}

// ServeAsync 异步启动所有监听器，不阻塞
func (c *Connect) ServeAsync() {
	go func() {
		if err := c.Serve(); err != nil {
			c.Log(logger.Error, "serve error: %v", err)
		}
	}()
}

// Close 优雅关闭：先停止所有监听器，再关闭 WebSocket 活跃连接与 bucket worker 池
func (c *Connect) Close() error {
	for _, l := range c.listeners {
		if l.Listener != nil {
			if err := l.Listener.Close(); err != nil {
				c.Log(logger.Error, "close listener %s error: %v", l.Address, err)
			}
		}
	}
	// 等待 Serve() 拉起的监听 goroutine 退出：listener 已关闭，http.Server.Serve
	// 会立即返回，这里不会长阻塞；若 Serve 从未调用（纯客户端），serving 为 false。
	if c.serving.Load() {
		c.serveWg.Wait()
	}
	c.listeners = nil
	// 传输实例（WsServer / TcpServer）的关闭由 Serve 的监听 goroutine 在退出时完成，
	// 并且对每种传输都生效——此前这里只关 ws 那一个实例，换传输就漏掉。
	if c.debugSrv != nil {
		if err := c.debugSrv.Close(); err != nil {
			c.Log(logger.Error, "close debug server error: %v", err)
		}
		c.debugSrv = nil
	}
	c.Log(logger.Info, "all listeners closed")
	return nil
}

// Dial 建立客户端连接。返回 error 而非静默降级：未知协议直接报错，
// 避免配置错误被掩盖（此前未知协议会静默走 WebSocket 且地址不带 scheme）。
func (c *Connect) Dial(ctx context.Context, network, address string, options ...option.ConnectOption) error {
	if c.server.getListen() != nil {
		return errors.New("dial not allowed: server already listening")
	}

	// 不支持的协议直接返回错误，不再静默降级到 WebSocket
	factory := c.resolveProtocol(network)
	if factory == nil {
		return fmt.Errorf("unsupported network type: %s", network)
	}

	// 地址形态由各传输自己解释：ws 需要 ws:// scheme 与 /ws 路径，tcp 要裸 host:port。
	// 之前这段 URL 拼接写在调用方，协议细节泄漏到了抽象之外——每种新协议都要
	// 在 Connect 里加一段 switch，而且只认 ws，其它协议永远 dial 不出去。
	call, err := factory.CreateClient(ctx, c, address, options...)
	if err != nil {
		c.Log(logger.Error, "%s dial error: %v", network, err)
		return err
	}
	// 连同协议名一起登记：之后每次调用都会自动带上 sloth-protocol 头
	c.server.setListen(call, network)
	// 连接由传输自己建立（实现了 ListenAndServe 的那些：ws / tcp）
	if d, ok := call.(interface{ ListenAndServe(context.Context) error }); ok {
		if err := d.ListenAndServe(ctx); err != nil {
			c.Log(logger.Error, "%s dial error: %v", network, err)
			return err
		}
	}
	runtime.GOMAXPROCS(c.cpuNum)
	return nil
}

func (c *Connect) SetAuthInfo(auth *auth.AuthInfo) error {
	listen := c.server.getListen()
	if listen == nil {
		return fmt.Errorf("server not found: %w", errs.ErrNotServing)
	}
	return listen.SetAuthInfo(auth)
}

// CallFunc 执行指定的方法，构造对应的参数，调用服务方法
func (c *Connect) CallFunc(ctx context.Context, r *http.Request, w *http.Response, svr types.IBucket, msgReq *trpc.RpcCaller) ([]byte, error) {
	defer func() {
		if err := recover(); err != nil {
			c.Log(logger.Error, "connect.CallFunc %s recover err : %v", msgReq.Method, err)
			c.Log(logger.Error, "connect.CallFunc %s recover stack : %s", msgReq.Method, string(debug.Stack()))
		}
	}()
	node, err := GetNode(msgReq.Method)
	if err != nil {
		c.Log(logger.Info, "(%s) method format error", c.ServerId)
		return nil, errors.New("method format error")
	}
	c.serviceMapMu.RLock()
	serviceFns, ok := c.serviceMap[node.Service]
	c.serviceMapMu.RUnlock()
	if !ok {
		c.Log(logger.Info, "(%s) service not found", c.ServerId)
		return nil, errors.New("service not found")
	}

	if svr != nil {
		ctx = context.WithValue(ctx, BucketKey, svr)
		if ch, cok := msgReq.Channel.(bucket.IChannel); cok {
			ctx = context.WithValue(ctx, ChannelKey, ch)
		}
	} else {
		if ch, cok := msgReq.Channel.(trpc.IChannel); cok {
			ctx = context.WithValue(ctx, ChannelKey, ch)
		}
	}

	// 克隆调用方 header 后追加 meta，避免写污染调用方持有的 map（RpcCaller 可能被复用）
	header := make(message.Header, len(msgReq.Header)+1)
	maps.Copy(header, msgReq.Header)
	header.Set("meta", c.metaData)
	if r != nil {
		header.Set("remote_addr", r.RemoteAddr)
	}
	ctx = context.WithValue(ctx, HeaderKey, header)

	funArgs := decoder.DecodeArgs(msgReq.Args, c.server.Decoder)
	return ref.CallFuncWithContext(ctx, serviceFns, node.Method, funArgs...)
}

// Log 按级别输出一行日志。
//
// 原实现是 log.Printf(line, args...)：级别参数被完全忽略（所有日志都是同一副样子），
// 且 args 与占位符数量不匹配时标准库会输出 "%!v(MISSING)" 之类的占位垃圾。
// 现在走统一门面：级别过滤生效，trace/字段由 ctx 携带（此处无 ctx 可传）。
func (w *Connect) Log(lvl logger.LogLevel, line string, args ...any) {
	logger.Logf(lvl, nil, line, args...)
}
