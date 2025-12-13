package wsocket

import (
	"context"

	"github.com/w6xian/sloth/group"
)

type IWsReply interface {
	ReplySuccess(id string, data []byte) error
	ReplyError(id string, err []byte) error
}

type IServerHandleMessage interface {
	OnData(ctx context.Context, s *WsServer, ch group.IChannel, msgType int, message []byte) error
	OnClose(ctx context.Context, s *WsServer, ch group.IChannel) error
	OnError(ctx context.Context, s *WsServer, ch group.IChannel, err error) error
	OnOpen(ctx context.Context, s *WsServer, ch group.IChannel) error
}
type IClientHandleMessage interface {
	OnData(ctx context.Context, c *LocalClient, ch *WsClient, msgType int, message []byte) error
	OnClose(ctx context.Context, c *LocalClient, ch *WsClient) error
	OnError(ctx context.Context, c *LocalClient, ch *WsClient, err error) error
	OnOpen(ctx context.Context, c *LocalClient, ch *WsClient) error
}
