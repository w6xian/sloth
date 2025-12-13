package sloth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"

	"github.com/w6xian/sloth/internal/logger"
	"github.com/w6xian/sloth/internal/utils"
	"github.com/w6xian/sloth/internal/utils/array"
	"github.com/w6xian/sloth/internal/utils/id"
	"github.com/w6xian/sloth/nrpc"
	"github.com/w6xian/sloth/nrpc/wsocket"
	"github.com/w6xian/sloth/options"
)

var instCount int64

// Precompute the reflect type for context.
var typeOfContext = reflect.TypeOf((*context.Context)(nil)).Elem()

// Precompute the reflect type for error.
var typeOfError = reflect.TypeOf((*error)(nil)).Elem()

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
	Option     *options.Options
	Encoder    func(any) ([]byte, error)
	Decoder    func([]byte) ([]byte, error)
	//log
	logger   []logger.Logger
	logLvl   logger.LogLevel
	logGuard sync.RWMutex
}

func (c *Connect) Options() *options.Options {
	return c.Option
}

func NewConnect(opts ...ConnOption) *Connect {
	svr := new(Connect)
	// svr.id = atomic.AddInt64(&instCount, 1)
	svr.ServerId = id.ShortID()
	svr.serviceMap = make(map[string]*ServiceFuncs)
	svr.sleepTimes = 15
	svr.times = 8
	svr.cpuNum = runtime.NumCPU()
	svr.client = ConnectClientRpc()
	svr.server = ConnectServerRpc()
	svr.Option = options.NewOptions()
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
	return svr
}

func (c *Connect) RegisterRpc(name string, rcvr any, metadata string) error {
	funcs := register(rcvr)
	c.serviceMap[name] = funcs
	return nil
}

// path是uri中的路径，默认是"/ws"
func (c *Connect) StartWebsocketServer(options ...wsocket.ServerOption) {
	//set the maximum number of CPUs that can be executing
	runtime.GOMAXPROCS(c.cpuNum)
	wsServer := wsocket.NewWsServer(c, options...)
	c.client.Serve = wsServer
	wsServer.ListenAndServe(context.Background())
}

func (c *Connect) StartWebsocketClient(options ...wsocket.ClientOption) {
	//set the maximum number of CPUs that can be executing
	runtime.GOMAXPROCS(c.cpuNum)
	wsClient := wsocket.NewLocalClient(c, options...)
	c.server.Listen = wsClient
	wsClient.ListenAndServe(context.Background())
}

var commonTypes = []string{"int", "int32", "int64", "uint", "uint32", "uint64", "float32", "float64", "string", "uint8", "bool"}

func (c *Connect) CallFunc(ctx context.Context, msgReq *nrpc.RpcCaller) ([]byte, error) {
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
		return nil, err
	}

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
			// fmt.Println("moreIn:", moreIn, "len(msgReq.Args):", len(msgReq.Args))
			if len(msgReq.Args) != moreIn-3 {
				return nil, errors.New("args error")
			}

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
		r := utils.GetType(isPtr, nameStr, data)
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

func suitableMethods(typ reflect.Type) map[string]reflect.Method {
	methods := make(map[string]reflect.Method)
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
	}

	for _, m := range methods {
		log.Printf("[success]method %s is registered", m.Name)
	}
	return methods
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
	service.M = suitableMethods(getType)
	return service
}

type ServiceFuncs struct {
	N string                    // name of service
	V reflect.Value             // receiver of methods for the service
	M map[string]reflect.Method // registered methods
}
