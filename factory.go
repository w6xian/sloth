package sloth

import (
	"context"

	"github.com/w6xian/sloth/v3/nrpc/wsocket"
	"github.com/w6xian/sloth/v3/option"
)

// path是uri中的路径，默认是"/ws"
func (c *Connect) initWsServerInstance(ctx context.Context, opts ...option.ConnectOption) error {
	wsServer := wsocket.GetWsServer(ctx, c, opts...)
	c.client.Serve = wsServer
	return nil
}

func (c *Connect) initWsClientInstance(ctx context.Context, opts ...option.ConnectOption) error {
	//set the maximum number of CPUs that can be executing
	wsClient := wsocket.NewLocalClient(c, opts...)
	c.server.Listen = wsClient
	return wsClient.ListenAndServe(ctx)
}
