package group

import (
	"context"

	"github.com/w6xian/sloth/message"
)

type IChannel interface {
	Call(ctx context.Context, mtd string, args ...[]byte) ([]byte, error)
	Push(ctx context.Context, msg *message.Msg) error
	ReplySuccess(id uint64, data []byte) error
	ReplyError(id uint64, err []byte) error
	Prev(p ...IChannel) IChannel
	Next(n ...IChannel) IChannel
	Room(r ...*Room) *Room
	UserId(u ...int64) int64
}
