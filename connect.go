package sloth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"

	"github.com/w6xian/sloth/nrpc"
	"github.com/w6xian/sloth/nrpc/wsocket"
)

// Precompute the reflect type for context.
var typeOfContext = reflect.TypeOf((*context.Context)(nil)).Elem()

// Precompute the reflect type for error.
var typeOfError = reflect.TypeOf((*error)(nil)).Elem()

type Connect struct {
	ServerId   string
	client     *ClientRpc
	server     *ServerRpc
	serviceMap map[string]*ServiceFuncs
	sleepTimes int
	times      int
	cpuNum     int
	tlsConfig  *tls.Config
}

func NewConnect(opts ...ConnOption) *Connect {
	svr := new(Connect)
	svr.serviceMap = make(map[string]*ServiceFuncs)
	svr.sleepTimes = 15
	svr.times = 8
	svr.cpuNum = runtime.NumCPU()
	svr.client = NewClientRpc()
	svr.server = NewServerRpc()
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
	wsServer.ListenAndServe()
}

func (c *Connect) StartWebsocketClient(options ...wsocket.ClientOption) {
	//set the maximum number of CPUs that can be executing
	runtime.GOMAXPROCS(c.cpuNum)
	wsClient := wsocket.NewLocalClient(c, options...)
	c.server.Listen = wsClient
	wsClient.ListenAndServe()
}

func (c *Connect) CallFunc(msgReq *nrpc.RpcCaller) ([]byte, error) {
	parts := strings.Split(msgReq.Method, ".")
	if len(parts) != 2 {
		return nil, errors.New("method format error")
	}
	serviceName := parts[0]
	methodName := parts[1]
	serviceFns, ok := c.serviceMap[serviceName]
	if !ok {
		return nil, errors.New("service not found")
	}
	mtd, ok := serviceFns.M[methodName]
	if !ok {
		return nil, errors.New("method not found")
	}
	ret := mtd.Func.Call([]reflect.Value{
		serviceFns.V,
		reflect.ValueOf(context.Background()),
		reflect.ValueOf([]byte(msgReq.Data)),
	})
	if len(ret) != 2 {
		return nil, errors.New("call func error")
	}
	err, ok := ret[1].Interface().(error)
	if ok && err != nil {
		return nil, err
	}
	// 调用成功，返回结果
	rst := ret[0].Interface().([]byte)
	return rst, nil
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
			panic(fmt.Sprintf("method %s must have at least 1 arguments", m.Name))
		}
		arg1 := m.Type.In(1)
		// 判定第一个参数是不是context.Context
		if !arg1.Implements(typeOfContext) {
			panic(fmt.Sprintf("method %s must have at least 1 arguments, first argument must be context.Context", m.Name))
		}
		// 返回值最后一个值需要是error
		if m.Type.NumOut() < 1 {
			panic(fmt.Sprintf("method %s must have 1-2 return value and last return value must be error", m.Name))
		}
		if m.Type.NumOut() > 2 {
			panic(fmt.Sprintf("method %s must have 1-2 return values and last return value must be error", m.Name))
		}
		out := m.Type.Out(m.Type.NumOut() - 1)
		if !out.Implements(typeOfError) {
			panic(fmt.Sprintf("method %s must have at least 1 return value, last return value must be error", m.Name))
		}
		methods[m.Name] = m
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
