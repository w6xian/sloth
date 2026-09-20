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
	"time"

	"github.com/w6xian/sloth/v3/internal/codec"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/decoder"
	"github.com/w6xian/sloth/v3/internal/logger"
	"github.com/w6xian/sloth/v3/internal/metrics"
	"github.com/w6xian/sloth/v3/internal/ref"
	"github.com/w6xian/sloth/v3/internal/utils/id"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/nrpc/wsocket"
	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/types"
	"github.com/w6xian/sloth/v3/types/auth"
	"github.com/w6xian/sloth/v3/types/trpc"
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
	serviceMapMu  sync.RWMutex
	sleepTimes    int
	times         int
	cpuNum        int
	tlsConfig     *tls.Config
	Option        *option.Options
	protocols     map[string]ProtocolFactory
	protocolCodecs map[string]codec.Codec

	// 多协议监听器
	listeners []ProtocolListener
	// httpHandlers []ServeHandler // HTTP 处理函数列表
	proxyHandler func(ctx context.Context, service string) (int64, error)
	// meta data
	metaData string
	// wsServer 由 initWsServerInstance 创建，Serve() 用它挂载 HTTP handler，Close() 用它优雅关闭
	wsServer *wsocket.WsServer
	// debugSrv 为 Option.DebugAddr 启动的调试服务（nil 表示未启用）
	debugSrv *http.Server
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
	return nil
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
	svr.protocols = map[string]ProtocolFactory{
		"ws":        wsProtocolFactory{},
		"wss":       wsProtocolFactory{},
		"websocket": wsProtocolFactory{},
	}
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
	if factory := c.resolveProtocol(network); factory != nil {
		if factory.Name() == "ws" || factory.Name() == "wss" || factory.Name() == "websocket" {
			ln, err := net.Listen("tcp", address)
			if err != nil {
				return err
			}
			c.listeners = append(c.listeners, ProtocolListener{
				Network:  network,
				Address:  address,
				Context:  ctx,
				Listener: ln,
				Options:  opts,
			})
			c.Log(logger.Info, "registered %s listener on %s", factory.Name(), address)
			return nil
		}
	}

	runtime.GOMAXPROCS(c.cpuNum)
	switch network {
	case "ws", "wss", "websocket":
		ln, err := net.Listen("tcp", address)
		if err != nil {
			return err
		}
		c.listeners = append(c.listeners, ProtocolListener{
			Network:  network,
			Address:  address,
			Context:  ctx,
			Listener: ln,
			Options:  opts,
		})
		c.Log(logger.Info, "registered WebSocket listener on %s", address)
		return nil
	default:
		return fmt.Errorf("unsupported network type: %s", network)
	}
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

// Serve 启动所有注册的协议监听器
// 阻塞直到所有服务停止
func (c *Connect) Serve() error {
	if len(c.listeners) == 0 {
		return errors.New("no listeners registered, call Listen() first")
	}
	if err := c.startDebugServer(); err != nil {
		return err
	}

	// 初始化 WebSocket 服务器
	for _, l := range c.listeners {
		if l.Network == "ws" || l.Network == "wss" || l.Network == "websocket" {
			if err := c.initWsServerInstance(l.Context, l.Options...); err != nil {
				return err
			}
			break
		}
	}

	// 创建 HTTP 服务器来处理所有 WebSocket 监听器
	var wg sync.WaitGroup
	errChan := make(chan error, len(c.listeners))

	// WS/WSS 共用同一个 WsServer 的 mux router（含 /ws 路由与 OnConnect 校验）。
	// 此前 http.Serve(listener, nil) 使用 DefaultServeMux，导致上述路由从未生效。
	handler := http.Handler(nil)
	if c.wsServer != nil {
		handler = c.wsServer.Handler()
	}

	for _, l := range c.listeners {
		wg.Add(1)
		go func(listener ProtocolListener) {
			defer wg.Done()
			runCtx := listener.Context
			if runCtx == nil {
				runCtx = context.Background()
			}
			switch listener.Network {
			case "ws", "websocket":
				// WebSocket 服务
				c.Log(logger.Info, "starting WebSocket server on %s", listener.Address)

				// 用显式 http.Server 并设置超时（http.Serve 用的是零值 Server，无任何超时保护）
				if err := newHTTPServer(handler).Serve(listener.Listener); err != nil {
					errChan <- err
				}
			case "wss":
				c.Log(logger.Info, "starting WSS server on %s", listener.Address)
				certFile := strings.TrimSpace(c.Option.TLSCertFile)
				keyFile := strings.TrimSpace(c.Option.TLSKeyFile)
				if certFile == "" || keyFile == "" {
					errChan <- fmt.Errorf("wss requires TLS cert/key file, set WithTLSCertKey(certFile, keyFile)")
					return
				}
				srv := newHTTPServer(handler)
				if err := srv.ServeTLS(listener.Listener, certFile, keyFile); err != nil {
					errChan <- err
				}
			}
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
		if l.Transport != nil {
			if err := l.Transport.Close(); err != nil {
				c.Log(logger.Error, "close listener %s error: %v", l.Address, err)
			}
		}
	}
	c.listeners = nil
	if c.wsServer != nil {
		if err := c.wsServer.Close(); err != nil {
			c.Log(logger.Error, "close ws server error: %v", err)
		}
	}
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

	if factory := c.resolveProtocol(network); factory != nil {
		if factory.Name() == "ws" || factory.Name() == "wss" || factory.Name() == "websocket" {
			scheme := "ws://"
			if network == "wss" {
				scheme = "wss://"
			}
			opts := []option.ConnectOption{
				option.WithUriPath("/ws"),
				option.WithAddress(scheme + address),
			}
			opts = append(opts, options...)
			if err := c.initWsClientInstance(ctx, opts...); err != nil {
				c.Log(logger.Error, "websocket dial error: %v", err)
				return err
			}
			return nil
		}
	}

	runtime.GOMAXPROCS(c.cpuNum)
	switch network {
	case "ws", "wss", "websocket":
		scheme := "ws://"
		if network == "wss" {
			scheme = "wss://"
		}
		opts := []option.ConnectOption{
			option.WithUriPath("/ws"),
			option.WithAddress(scheme + address),
		}
		opts = append(opts, options...)
		if err := c.initWsClientInstance(ctx, opts...); err != nil {
			c.Log(logger.Error, "websocket dial error: %v", err)
			return err
		}
		return nil
	default:
		return fmt.Errorf("unsupported network type: %s", network)
	}
}

func (c *Connect) SetAuthInfo(auth *auth.AuthInfo) error {
	listen := c.server.getListen()
	if listen == nil {
		return errors.New("server not found")
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
