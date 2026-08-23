package wsocket

import (
	"context"

	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/types"
	"github.com/w6xian/sloth/v3/types/trpc"
)

func GetWsServer(ctx context.Context, c trpc.ICallRpc, options ...option.ConnectOption) types.IServer {
	wsServer := NewWsServer(c, options...)
	wsServer.ListenAndServe(ctx)
	return wsServer
}

func GetWsClient(ctx context.Context, c trpc.ICallRpc, options ...option.ConnectOption) trpc.ICall {
	wsClient := NewLocalClient(c, options...)
	return wsClient
}
