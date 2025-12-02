package sloth

import (
	"context"
	"crypto/tls"
	"errors"
	"reflect"
	"runtime"
	"sloth/internal/tools"
	"sloth/nrpc"
	"sloth/nrpc/wsocket"
	"strings"
)

type Connect struct {
	ServerId   string
	client     *ClientRpc
	server     *ServerRpc
	serviceMap map[string]*tools.ServiceFuncs
	sleepTimes int
	times      int
	cpuNum     int
	tlsConfig  *tls.Config
}

func NewConnect(opts ...ConnOption) *Connect {
	svr := new(Connect)
	svr.serviceMap = make(map[string]*tools.ServiceFuncs)
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
	funcs := tools.Register(name, rcvr)
	c.serviceMap[name] = funcs
	return nil
}

func (c *Connect) Server(network, addr string) {
	//set the maximum number of CPUs that can be executing
	runtime.GOMAXPROCS(c.cpuNum)
	ln, err := c.makeListener(network, addr)
	if err != nil {
		panic(err)
	}
	defer ln.Close()
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

func (c *Connect) Client() {
	runtime.GOMAXPROCS(c.cpuNum)
}
