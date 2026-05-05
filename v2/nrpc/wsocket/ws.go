package wsocket

import (
	"context"

	"github.com/w6xian/sloth/v2/option"
	"github.com/w6xian/sloth/v2/pprof"
	"github.com/w6xian/sloth/v2/types"
	"github.com/w6xian/sloth/v2/types/trpc"
)

func GetWsServer(c trpc.ICallRpc, options ...option.ConnectOption) types.IServer {
	//set the maximum number of CPUs that can be executing
	// runtime.GOMAXPROCS(c.cpuNum)
	wsServer := NewWsServer(c, options...)
	// c.client.Serve = wsServer
	pprof.New(c.(pprof.IService)).UsePProf(wsServer)
	wsServer.ListenAndServe(context.Background())
	return wsServer
}

func GetWsClient(c trpc.ICallRpc, options ...option.ConnectOption) trpc.ICall {
	// wsClient := NewLocalClient(c, options...)
	// wsClient.ListenAndServe(context.Background())
	// return wsClient
	return nil

}
