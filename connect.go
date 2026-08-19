package sloth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"maps"
	"net"
	"net/http"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/decoder"
	"github.com/w6xian/sloth/v3/internal/logger"
	"github.com/w6xian/sloth/v3/internal/ref"
	"github.com/w6xian/sloth/v3/internal/utils/id"
	"github.com/w6xian/sloth/v3/message"
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
	sleepTimes int
	times      int
	cpuNum     int
	tlsConfig  *tls.Config
	Option     *option.Options

	// 多协议监听器
	listeners []ProtocolListener
	// httpHandlers []ServeHandler // HTTP 处理函数列表
	proxyHandler func(ctx context.Context, service string) (int64, error)
	// meta data
	metaData string
}

func (c *Connect) CallNetFunc(ctx context.Context, service string, msgId uint64, msg []byte) ([]byte, error) {
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
	ns := strings.Split(service, ".")
	if len(ns) != 2 {
		return false
	}
	_, ok := c.serviceMap[ns[0]]
	return ok
}

func (c *Connect) Options() *option.Options {
	return c.Option
}

func ServerConn(client *ClientRpc, opts ...ConnOption) *Connect {
	opts = append(opts, Client(client))
	return newConnect(opts...)
}

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
	svr.listeners = make([]ProtocolListener, 0)
	svr.proxyHandler = func(ctx context.Context, service string) (int64, error) {
		return 0, nil
	}

	for _, opt := range opts {
		opt(svr)
	}

	return svr
}

// Register 注册一个服务，name是服务名，rcvr是服务实现，metadata是服务描述
func (c *Connect) Register(name string, rcvr any, metadata string) error {
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

	// 工厂模式，根据不同的协议，创建不同的服务器监听器
	runtime.GOMAXPROCS(c.cpuNum)
	switch network {
	case "ws", "wss", "websocket":
		// WebSocket 服务器
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

// Serve 启动所有注册的协议监听器
// 阻塞直到所有服务停止
func (c *Connect) Serve() error {
	if len(c.listeners) == 0 {
		return errors.New("no listeners registered, call Listen() first")
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

				if err := http.Serve(listener.Listener, nil); err != nil {
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
				srv := &http.Server{
					Handler: nil,
				}
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

// Close 关闭所有监听器
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
	c.Log(logger.Info, "all listeners closed")
	return nil
}

func (c *Connect) Dial(ctx context.Context, network, address string, options ...option.ConnectOption) {

	if c.server.Listen != nil {
		return
	}
	// 工厂模式，根据不同的协议，创建不同的客户端
	// 支持协议: ws, wss, websocket (WebSocket), tcp (TODO)
	runtime.GOMAXPROCS(c.cpuNum)

	switch network {
	case "ws", "wss", "websocket":
		// WebSocket 客户端
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
			return
		}
	default:
		// 默认使用 WebSocket
		c.Log(logger.Info, "unknown network type: %s, using WebSocket", network)
		opts := []option.ConnectOption{
			option.WithUriPath("/ws"),
			option.WithAddress(address),
		}
		opts = append(opts, options...)
		if err := c.initWsClientInstance(ctx, opts...); err != nil {
			c.Log(logger.Error, "websocket dial error: %v", err)
			return
		}
	}

}

func (c *Connect) SetAuthInfo(auth *auth.AuthInfo) error {
	return c.server.Listen.SetAuthInfo(auth)
}

// CallFunc 执行指定的方法，构造对应的参数，调用服务方法
func (c *Connect) CallFunc(ctx context.Context, svr types.IBucket, msgReq *trpc.RpcCaller) ([]byte, error) {
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
	serviceFns, ok := c.serviceMap[node.Service]
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
	ctx = context.WithValue(ctx, HeaderKey, header)

	funArgs := decoder.DecodeArgs(msgReq.Args, c.server.Decoder)
	return ref.CallFuncWithContext(ctx, serviceFns, node.Method, funArgs...)
}

func (w *Connect) Log(lvl logger.LogLevel, line string, args ...any) {
	log.Printf(line, args...)
}
