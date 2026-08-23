package sloth

import (
	"context"

	"github.com/w6xian/sloth/v3/nrpc/wsocket"
	"github.com/w6xian/sloth/v3/option"
)

// path是uri中的路径，默认是"/ws"
func (c *Connect) initWsServerInstance(ctx context.Context, opts ...option.ConnectOption) error {
	// create concrete WsServer so we can inject protocol-specific codec
	wsServer := wsocket.NewWsServer(c, opts...)
	// inject codec if Connect provides one for ws
	if c.protocols != nil {
		// default codec is already set in WsServer; if Connect has registered a codec mapping, user can set via RegisterProtocolCodec
	}
	c.client.Serve = wsServer
	// start listening (blocking inside Serve) similarly to previous behavior
	return wsServer.ListenAndServe(ctx)
}

func (c *Connect) initWsClientInstance(ctx context.Context, opts ...option.ConnectOption) error {
	//set the maximum number of CPUs that can be executing
	wsClient := wsocket.NewLocalClient(c, opts...)
	c.server.Listen = wsClient
	return wsClient.ListenAndServe(ctx)
}
