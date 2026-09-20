package types

import (
	"context"

	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/message"
)

type IServer interface {
	Bucket(userId int64) *bucket.Bucket
	Room(roomId int64) *bucket.Room
	Channel(userId int64) bucket.IChannel
	Broadcast(ctx context.Context, msg *message.Msg) error
	AllBuckets() []*bucket.Bucket
}
