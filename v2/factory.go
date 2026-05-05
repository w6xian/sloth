package sloth

import (
	"context"
	"runtime"

	"github.com/w6xian/sloth/v2/nrpc/wsocket"
	"github.com/w6xian/sloth/v2/option"
)

// path是uri中的路径，默认是"/ws"
func (c *Connect) initWsServerInstance(opts ...option.ConnectOption) error {
	//set the maximum number of CPUs that can be executing
	// runtime.GOMAXPROCS(c.cpuNum)
	// wsServer := wsocket.NewWsServer(c, options...)
	// c.client.Serve = wsServer
	// pprof.New(c).UsePProf(wsServer)
	// wsServer.ListenAndServe(context.Background())
	// return nil
	runtime.GOMAXPROCS(c.cpuNum)
	wsServer := wsocket.GetWsServer(c, opts...)
	c.client.Serve = wsServer
	return nil
}

func (c *Connect) initWsClientInstance(opts ...option.ConnectOption) error {
	//set the maximum number of CPUs that can be executing
	wsClient := wsocket.NewLocalClient(c, opts...)
	c.server.Listen = wsClient
	wsClient.ListenAndServe(context.Background())
	return nil
}
