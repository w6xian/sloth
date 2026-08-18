package types

import (
	"context"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/message"
)

type IServer interface {
	Bucket(userId int64) *bucket.Bucket
	Room(roomId int64) *bucket.Room
	Channel(userId int64) bucket.IChannel
	Broadcast(ctx context.Context, msg *message.Msg) error
	AllBuckets() []*bucket.Bucket
}
