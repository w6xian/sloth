package types

import (
	"context"

	"github.com/w6xian/sloth/v2/bucket"
	"github.com/w6xian/sloth/v2/message"
)

type IBucket interface {
	Bucket(userId int64) *bucket.Bucket
}

type IConnRpc interface {
	Call(ctx context.Context, header message.Header, mtd string, data ...[]byte) ([]byte, error)
}
type IConnInfo interface {
	GetUserId() int64
	GetRoomId() int64
}
