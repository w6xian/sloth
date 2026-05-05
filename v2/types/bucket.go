package types

import (
	"context"

	"github.com/w6xian/sloth/v2/bucket"
	"github.com/w6xian/sloth/v2/message"
)

type IConnRpc interface {
	Call(ctx context.Context, header message.Header, mtd string, data ...[]byte) ([]byte, error)
}
type IConnInfo interface {
	GetUserId() int64
	GetRoomId() int64
}

type IBucket interface {
	Bucket(userId int64) *bucket.Bucket
	Channel(userId int64) bucket.IChannel
	Room(roomId int64) *bucket.Room
	Broadcast(ctx context.Context, msg *message.Msg) error
}
