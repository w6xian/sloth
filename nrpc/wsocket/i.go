package wsocket

import (
	"context"

	"github.com/w6xian/sloth/bucket"
)

type IServerHandleMessage interface {
	OnData(ctx context.Context, s *WsServer, ch bucket.IChannel, msgType int, message []byte) error
	OnClose(ctx context.Context, s *WsServer, ch bucket.IChannel) error
	OnError(ctx context.Context, s *WsServer, ch bucket.IChannel, err error) error
	OnOpen(ctx context.Context, s *WsServer, ch bucket.IChannel) error
}
type IClientHandleMessage interface {
	OnData(ctx context.Context, c *LocalClient, ch *WsChannelClient, msgType int, message []byte) error
	OnClose(ctx context.Context, c *LocalClient, ch *WsChannelClient) error
	OnError(ctx context.Context, c *LocalClient, ch *WsChannelClient, err error) error
	OnOpen(ctx context.Context, c *LocalClient, ch *WsChannelClient) error
}
