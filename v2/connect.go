package sloth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	"github.com/w6xian/sloth/v2/bucket"
	"github.com/w6xian/sloth/v2/internal/logger"
	"github.com/w6xian/sloth/v2/internal/utils"
	"github.com/w6xian/sloth/v2/internal/utils/array"
	"github.com/w6xian/sloth/v2/internal/utils/id"
	"github.com/w6xian/sloth/v2/message"
	"github.com/w6xian/sloth/v2/nrpc"
	"github.com/w6xian/sloth/v2/nrpc/kcpsock"
	"github.com/w6xian/sloth/v2/nrpc/tcpsock"
	"github.com/w6xian/sloth/v2/nrpc/wsocket"
	"github.com/w6xian/sloth/v2/option"
	"github.com/w6xian/sloth/v2/pprof"
	"github.com/w6xian/sloth/v2/types"
	"github.com/w6xian/sloth/v2/types/auth"
	"github.com/w6xian/sloth/v2/types/trpc"
	"github.com/w6xian/tlv"
)

var instCount int64

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

// Precompute the reflect type for context.
var typeOfContext = reflect.TypeOf((*context.Context)(nil)).Elem()

// Precompute the reflect type for error.
var typeOfError = reflect.TypeOf((*error)(nil)).Elem()

// ProtocolListener 协议监听器
type ProtocolListener struct {
	Network   string                 // 协议类型: ws, tcp, quic, grpc
	Address   string                 // 监听地址
	Listener  net.Listener           // net.Listener 监听器
	Transport nrpc.Listener          // Transport 抽象监听器
	Options   []option.ConnectOption // 连接	 选项
}

// ServeHandler HTTP 处理函数接口
type ServeHandler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

type Connect struct {
	// id         int64
	ServerId   string
	client     *ClientRpc
	server     *ServerRpc
	serviceMap map[string]*ServiceFuncs
	sleepTimes int
	times      int
	cpuNum     int
	tlsConfig  *tls.Config
	Option     *option.Options
	Encoder    func(any) ([]byte, error)
	Decoder    func([]byte) ([]byte, error)
	//log
	logger   []logger.Logger
	logLvl   logger.LogLevel
	logGuard sync.RWMutex
	// Transport 抽象，支持多协议
	transport nrpc.Transport
	// 多协议监听器
	listeners    []ProtocolListener
	httpHandlers []ServeHandler // HTTP 处理函数列表
}

func (c *Connect) Options() *option.Options {
	return c.Option
}

// SetTransport 设置 Transport 抽象层，支持多协议（TCP、WebSocket、QUIC等）
func (c *Connect) SetTransport(transport nrpc.Transport) {
	c.transport = transport
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
	svr.serviceMap = make(map[string]*ServiceFuncs)
	svr.sleepTimes = 15
	svr.times = 8
	svr.cpuNum = runtime.NumCPU()
	svr.client = LinkClientFunc()
	svr.server = LinkServerFunc()
	svr.Option = option.NewOptions()
	svr.Encoder = nrpc.DefaultEncoder
	svr.Decoder = nrpc.DefaultDecoder

	svr.logger = make([]logger.Logger, int(logger.Max+1))
	svr.logLvl = logger.Info
	l := log.New(os.Stderr, "", log.Flags())
	for index, _ := range svr.logger {
		svr.logger[index] = l
	}

	for _, opt := range opts {
		opt(svr)
	}
	svr.regist_pprof()
	return svr
}

func (c *Connect) regist_pprof() error {
	prof := pprof.New(c)
	funcs := register(prof)
	c.serviceMap["pprof"] = funcs
	return nil
}

// RegisterRpc 推荐使用Register方法注册rpc服务
// deprecated: RegisterRpc is deprecated, use Register instead
func (c *Connect) RegisterRpc(name string, rcvr any, metadata string) error {
	return c.Register(name, rcvr, metadata)
}

// Register 注册一个服务，name是服务名，rcvr是服务实现，metadata是服务描述
func (c *Connect) Register(name string, rcvr any, metadata string) error {
	if _, ok := c.serviceMap[name]; ok {
		return fmt.Errorf("service %s already registered", name)
	}
	funcs := register(rcvr)
	c.serviceMap[name] = funcs
	return nil
}

func (c *Connect) GetServiceList(ctx context.Context) (map[string][]string, error) {
	rst := make(map[string][]string)
	for svr, funcs := range c.serviceMap {
		fs := []string{}
		for _, f := range funcs.A {
			fs = append(fs, f.Define)
		}
		rst[svr] = fs
	}
	return rst, nil
}

func (c *Connect) GetServiceFuncs(name string) map[string]FuncStruct {
	return c.serviceMap[name].A
}

// Listen 注册协议监听器，不立即启动服务
// 可以多次调用注册多个协议，最后用 Serve() 启动所有服务
func (c *Connect) Listen(ctx context.Context, network, address string, opts ...option.ConnectOption) error {
	// 如果设置了 Transport，使用 Transport 抽象
	if c.transport != nil {
		ctx := context.Background()
		listener, err := c.transport.Listen(ctx, address)
		if err != nil {
			return err
		}
		c.listeners = append(c.listeners, ProtocolListener{
			Network:   network,
			Address:   address,
			Transport: listener,
		})
		return nil
	}

	// 工厂模式，根据不同的协议，创建不同的服务器监听器
	runtime.GOMAXPROCS(c.cpuNum)
	switch network {
	case "ws", "wss", "websocket":
		// WebSocket 服务器
		ln, err := net.Listen("tcp", address)
		if err != nil {
			return err
		}
		r := mux.NewRouter()
		opts = append(opts, option.WithRouter(r))
		c.httpHandlers = append(c.httpHandlers, r)
		c.listeners = append(c.listeners, ProtocolListener{
			Network:  network,
			Address:  address,
			Listener: ln,
			Options:  opts,
		})
		c.Log(logger.Info, "registered WebSocket listener on %s", address)
		return nil
	case "tcp", "tcp4", "tcp6":
		// TODO: 实现 TCP 服务器监听器
		ln, err := net.Listen(network, address)
		if err != nil {
			return err
		}
		c.listeners = append(c.listeners, ProtocolListener{
			Network:  network,
			Address:  address,
			Listener: ln,
		})
		c.Log(logger.Info, "registered TCP listener on %s", address)
		return nil
	case "kcp":
		srv := kcpsock.NewKcpServer(c, c.Options())
		if _, err := srv.Listen(ctx, address); err != nil {
			return err
		}
		c.listeners = append(c.listeners, ProtocolListener{
			Network:   network,
			Address:   address,
			Transport: srv,
		})
		c.Log(logger.Info, "registered KCP listener on %s", address)
		return nil
	case "quic":
		// TODO: 实现 QUIC 服务器监听器
		c.Log(logger.Error, "QUIC server not implemented yet")
		return errors.New("QUIC server not implemented yet")
	case "grpc":
		// TODO: 实现 gRPC 服务器监听器
		c.Log(logger.Error, "gRPC server not implemented yet")
		return errors.New("gRPC server not implemented yet")
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
			if err := c.initWsServerInstance(l.Options...); err != nil {
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
			switch listener.Network {
			case "ws", "wss", "websocket":
				// WebSocket 服务
				c.Log(logger.Info, "starting WebSocket server on %s", listener.Address)

				if err := http.Serve(listener.Listener, nil); err != nil {
					errChan <- err
				}
			case "tcp", "tcp4", "tcp6":
				c.Log(logger.Info, "starting TCP server on %s", listener.Address)
				tcpServer := tcpsock.NewTcpServer(c, c.Options())
				tcpServer.UseListener(listener.Listener)
				if c.client != nil && c.client.Serve == nil {
					c.client.Serve = tcpServer
				}
				if err := tcpServer.Serve(context.Background()); err != nil {
					errChan <- err
				}
			case "kcp":
				c.Log(logger.Info, "starting KCP server on %s", listener.Address)
				srv, ok := listener.Transport.(*kcpsock.KcpServer)
				if !ok || srv == nil {
					errChan <- fmt.Errorf("kcp listener not initialized")
					return
				}
				if c.client != nil && c.client.Serve == nil {
					c.client.Serve = srv
				}
				if err := srv.Serve(context.Background()); err != nil {
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
	// 如果设置了 Transport，使用 Transport 抽象
	if c.transport != nil {
		ctx := context.Background()
		icall, err := c.transport.Dial(ctx, address)
		if err != nil {
			panic(err)
		}
		c.server.Listen = icall
		return
	}

	if c.server.Listen != nil {
		return
	}
	// 工厂模式，根据不同的协议，创建不同的客户端
	// 支持协议: ws, wss, websocket (WebSocket), tcp (TODO)
	runtime.GOMAXPROCS(c.cpuNum)

	switch network {
	case "ws", "wss", "websocket":
		// WebSocket 客户端
		opts := []option.ConnectOption{
			option.WithUriPath("/ws"),
			option.WithAddress(address),
		}
		opts = append(opts, options...)
		if err := c.initWsClientInstance(opts...); err != nil {
			panic(err)
		}
	case "tcp", "tcp4", "tcp6":
		tcpClient := tcpsock.NewTcpClient(c)
		if _, err := tcpClient.Dial(ctx, address); err != nil {
			c.Log(logger.Error, "TCP dial error: %v", err)
			return
		}
		c.server.Listen = tcpClient
		return
	case "kcp":
		kcpClient := kcpsock.NewKcpClient(c)
		if _, err := kcpClient.Dial(ctx, address); err != nil {
			c.Log(logger.Error, "KCP dial error: %v", err)
			return
		}
		c.server.Listen = kcpClient
		return
	case "quic":
		// TODO: 实现 QUIC 客户端
		c.Log(logger.Error, "QUIC client not implemented yet")
		return
	case "grpc":
		// TODO: 实现 gRPC 客户端
		c.Log(logger.Error, "gRPC client not implemented yet")
		return
	default:
		// 默认使用 WebSocket
		c.Log(logger.Info, "unknown network type: %s, using WebSocket", network)
		opts := []option.ConnectOption{
			option.WithUriPath("/ws"),
			option.WithAddress(address),
		}
		opts = append(opts, options...)
		if err := c.initWsClientInstance(opts...); err != nil {
			panic(err)
		}
	}

}

func (c *Connect) SetAuthInfo(auth *auth.AuthInfo) error {
	return c.server.Listen.SetAuthInfo(auth)
}

var commonTypes = []string{"int", "int32", "int64", "uint", "uint32", "uint64", "float32", "float64", "string", "uint8", "bool"}

// CallFunc 执行指定的方法，构造对应的参数，调用服务方法
func (c *Connect) CallFunc(ctx context.Context, svr types.IBucket, msgReq *trpc.RpcCaller) ([]byte, error) {
	defer func() {
		if err := recover(); err != nil {
			// fmt.Println("------------")
			c.Log(logger.Error, "connect.CallFunc %s recover err : %v", msgReq.Method, err)
			c.Log(logger.Error, "connect.CallFunc %s recover stack : %s", msgReq.Method, string(debug.Stack()))
		}
	}()
	parts := strings.Split(msgReq.Method, ".")
	if len(parts) != 2 {
		c.Log(logger.Info, "(%s) method format error", c.ServerId)
		return nil, errors.New("method format error")
	}
	serviceName := parts[0]
	methodName := parts[1]
	serviceFns, ok := c.serviceMap[serviceName]
	if !ok {
		c.Log(logger.Info, "(%s) service not found", c.ServerId)
		return nil, errors.New("service not found")
	}
	mtd, ok := serviceFns.M[methodName]
	if !ok {
		c.Log(logger.Info, "(%s) method not found", c.ServerId)
		return nil, errors.New("method not found")
	}

	// 编码
	args, err := c.Decoder(msgReq.Data)
	if err != nil {
		args = msgReq.Data
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

	header := message.Header{}
	if len(msgReq.Header) > 0 {
		header = msgReq.Header
	}
	ctx = context.WithValue(ctx, HeaderKey, message.Header(header))

	funcArgs := []reflect.Value{
		serviceFns.V,
		reflect.ValueOf(ctx),
	}
	if len(args) > 0 && mtd.Type.NumIn() > 2 {
		// Elem() 相当于 *T 取指针指向的类型
		in2 := mtd.Type.In(2)
		param, iErr := instance_params(in2, args)
		// fmt.Println("param:", param, err)
		if iErr != nil {
			return nil, iErr
		}
		funcArgs = append(funcArgs, param)
		// fmt.Println("funcArgs:", funcArgs)
		if len(msgReq.Args) > 0 {
			moreIn := mtd.Type.NumIn()
			// more args
			for i := 3; i < moreIn; i++ {
				data, iErr := c.Decoder(msgReq.Args[i-3])
				if iErr != nil {
					return nil, iErr
				}
				inx := mtd.Type.In(i)
				param, iErr := instance_params(inx, data)
				if iErr != nil {
					return nil, iErr
				}
				// fmt.Println("more args  344444:", param, err)
				funcArgs = append(funcArgs, param)
			}
		}
	}
	// fmt.Println("-----------------------")
	// fmt.Println("funcArgs:", funcArgs)
	// fmt.Println("-----------------------")
	ret := mtd.Func.Call(funcArgs)
	if len(ret) != 2 {
		c.Log(logger.Info, "(%s) call func error", c.ServerId)
		return nil, errors.New("call func error")
	}
	iErr, ok := ret[1].Interface().(error)
	if ok && iErr != nil {
		return nil, iErr
	}
	// 调用成功，返回结果
	data := ret[0].Interface()
	// fmt.Println("---------result--------")
	// fmt.Println(data)
	// fmt.Println("-----------------------")
	// textmessage 协议，1 no tlv
	if msgReq.Protocol == wsocket.TextMessage {
		// 调用成功，返回结果
		resp, err := utils.AnyToStr(data)
		if err != nil {
			return nil, err
		}
		return []byte(resp), nil
	}
	// 调用成功，返回结果
	resp, err := c.Encoder(data)
	// fmt.Println("---------result-- resp------")
	// fmt.Println(resp)
	// fmt.Println(err)
	// fmt.Println("-----------------------")
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func instance_params(params reflect.Type, data []byte) (reflect.Value, error) {
	isPtr := params.Kind() == reflect.Pointer
	structType := params
	if isPtr {
		structType = params.Elem()
	}
	nameStr := structType.String()
	if nameStr == "[]byte" || nameStr == "[]uint8" {
		if isPtr {
			return reflect.ValueOf(&data), nil
		}
		return reflect.ValueOf(data), nil
	} else if array.InArray(nameStr, commonTypes) {
		// 检查参数类型，根据参数类型进行转换（[]byte改成 “name“对应的类型）
		// fmt.Println("nameStr:", nameStr)
		r := tlv.GetType(isPtr, nameStr, data)
		return r, nil
	} else {
		// 转换参数类型为reflect.Value
		if instance, cErr := new_instance_reflect(structType); cErr == nil {
			utils.Deserialize(data, instance.Interface())
			// 根据需要返回对应的类型
			if !isPtr {
				return instance.Elem(), nil
			}
			return instance, nil
		}
	}
	return reflect.Value{}, fmt.Errorf("unknown type: %s", params.String())
}

// 根据type生成新的实例
func new_instance_reflect(typ reflect.Type) (reflect.Value, error) {
	if typ == nil {
		return reflect.Value{}, fmt.Errorf("unknown type: %s", typ.String())
	}
	instance := reflect.New(typ)
	return instance, nil
}

// SetLogger assigns the logger to use as well as a level
//
// The logger parameter is an interface that requires the following
// method to be implemented (such as the the stdlib log.Logger):
//
//	Output(calldepth int, s string)
func (w *Connect) SetLogger(l logger.Logger, lvl logger.LogLevel) {
	w.logGuard.Lock()
	defer w.logGuard.Unlock()

	for level := range w.logger {
		w.logger[level] = l
	}
	w.logLvl = lvl
}

// SetLoggerForLevel assigns the same logger for specified `level`.
func (w *Connect) SetLoggerForLevel(l logger.Logger, lvl logger.LogLevel) {
	w.logGuard.Lock()
	defer w.logGuard.Unlock()

	w.logger[lvl] = l
}

// SetLoggerLevel sets the package logging level.
func (w *Connect) SetLoggerLevel(lvl logger.LogLevel) {
	w.logGuard.Lock()
	defer w.logGuard.Unlock()

	w.logLvl = lvl
}

func (w *Connect) getLogger(lvl logger.LogLevel) (logger.Logger, logger.LogLevel) {
	w.logGuard.RLock()
	defer w.logGuard.RUnlock()

	return w.logger[lvl], w.logLvl
}

func (w *Connect) getLogLevel() logger.LogLevel {
	w.logGuard.RLock()
	defer w.logGuard.RUnlock()

	return w.logLvl
}

func (w *Connect) Log(lvl logger.LogLevel, line string, args ...any) {
	logger, logLvl := w.getLogger(lvl)
	if logger == nil {
		return
	}
	if logLvl > lvl {
		return
	}
	logger.Output(2, fmt.Sprintf("%-4s %s", lvl, fmt.Sprintf(line, args...)))
}

func suitableMethods(typ reflect.Type) (map[string]reflect.Method, map[string]FuncStruct) {
	methods := make(map[string]reflect.Method)
	// 方法 及定义的参数
	iface := make(map[string]FuncStruct)

	// 遍历所有方法
	for m := 0; m < typ.NumMethod(); m++ {
		m := typ.Method(m)
		// 这里可以加一些方法需要什么样的参数，比如第一个参数必须是context.Context
		if m.Type.NumIn() < 2 || m.Type.In(1) != reflect.TypeOf((*context.Context)(nil)).Elem() {
			continue
		}
		// Method must be exported.
		if m.PkgPath != "" {
			continue
		}
		if !m.IsExported() {
			continue
		}
		// 只限定第一个参数，一这是context.Context，后面的参数可以是任意类型
		if m.Type.NumIn() < 2 {
			log.Printf("[notice]method %s must have at least 1 arguments", m.Name)
			continue
		}
		arg1 := m.Type.In(1)
		// 判定第一个参数是不是context.Context
		if !arg1.Implements(typeOfContext) {
			log.Printf("[notice]method %s must have at least 1 arguments, first argument must be context.Context", m.Name)
			continue
		}
		// 返回值最后一个值需要是error
		if m.Type.NumOut() < 1 {
			log.Printf("[notice]method %s must have 1-2 return value and last return value must be error", m.Name)
			continue
		}
		if m.Type.NumOut() > 2 {
			log.Printf("[notice]method %s must have 1-2 return values and last return value must be error", m.Name)
			continue
		}
		out := m.Type.Out(m.Type.NumOut() - 1)
		if !out.Implements(typeOfError) {
			log.Printf("[notice]method %s must have at least 1 return value, last return value must be error", m.Name)
			continue
		}
		methods[m.Name] = m
		// 方法的参数
		args := make([]ArgStruct, 0)
		for i := 2; i < m.Type.NumIn(); i++ {
			args = append(args, ArgStruct{
				Name: fmt.Sprintf("arg%d", i-2),
				Type: m.Type.In(i).String(),
			})
		}
		s := strings.SplitN(m.Type.String(), ",", 2)
		api := fmt.Sprintf("%s(", m.Name)
		s[0] = api
		iface[m.Name] = FuncStruct{
			Name:   m.Name,
			Define: fmt.Sprintf("%s", strings.Join(s, "")),
		}
	}

	for _, m := range methods {
		log.Printf("[success]method %s is registered", m.Name)
	}

	return methods, iface
}

func register(rcvr any) *ServiceFuncs {
	service := new(ServiceFuncs)
	getType := reflect.TypeOf(rcvr)
	service.V = reflect.ValueOf(rcvr)
	k := getType.Kind()
	if k == reflect.Pointer {
		el := getType.Elem()
		sname := fmt.Sprintf("%s.%s", el.PkgPath(), el.Name())
		service.N = sname
	} else {
		sname := fmt.Sprintf("%s.%s", getType.PkgPath(), getType.Name())
		service.N = sname
	}
	// Install the methods
	m, a := suitableMethods(getType)
	service.M = m
	service.A = a
	return service
}

type Functions []string
type ServiceApi map[string]FuncStruct

type ServiceFuncs struct {
	N string                    // name of service
	V reflect.Value             // receiver of methods for the service
	M map[string]reflect.Method // registered methods
	A ServiceApi                // arguments of methods
}

type FuncStruct struct {
	Name   string `json:"name"`
	Define string `json:"define"`
}

type ArgStruct struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Desc string `json:"desc"`
}
