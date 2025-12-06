package sloth

import (
	"context"

	"github.com/w6xian/sloth/group"
	"github.com/w6xian/sloth/message"
)

type RpcServer interface {
	Start(addr string) error
}

type IRpc interface {
	SetProtocol(protocol string)
}

type IServer interface {
	Bucket(userId int64) *group.Bucket
	Room(roomId int64) *group.Room
	Channel(userId int64) group.IChannel
	Broadcast(ctx context.Context, msg *message.Msg) error
}
