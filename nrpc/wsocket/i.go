package wsocket

import (
	"context"

	"github.com/w6xian/sloth/group"
)

type IWsReply interface {
	ReplySuccess(id uint64, data []byte) error
	ReplyError(id uint64, err []byte) error
}

type IServerHandleMessage interface {
	HandleMessage(ctx context.Context, s *WsServer, ch group.IChannel, msgType int, message []byte) error
}
type IClientHandleMessage interface {
	HandleMessage(ctx context.Context, c *LocalClient, ch *WsClient, msgType int, message []byte) error
}
