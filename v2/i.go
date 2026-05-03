package sloth

import (
	"context"

	"github.com/w6xian/sloth/v2/bucket"
	"github.com/w6xian/sloth/v2/message"
	"github.com/w6xian/sloth/v2/pprof"
)

type RpcServer interface {
	Start(addr string) error
}

type IRpc interface {
	SetEncoder(encoder Encoder)
	SetDecoder(decoder Decoder)
}

type IServer interface {
	PProf(ctx context.Context) (*pprof.BucketInfo, error)
	Bucket(userId int64) *bucket.Bucket
	Room(roomId int64) *bucket.Room
	Channel(userId int64) bucket.IChannel
	Broadcast(ctx context.Context, msg *message.Msg) error
}
