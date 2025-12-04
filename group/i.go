package group

import (
	"context"

	"github.com/w6xian/sloth/message"
)

type IChannel interface {
	Call(ctx context.Context, mtd string, args any) ([]byte, error)
	Push(ctx context.Context, req *message.Msg) error
	ReplySuccess(id string, data []byte) error
	ReplyError(id string, err []byte) error
	Prev(p ...IChannel) IChannel
	Next(n ...IChannel) IChannel
	Room(r ...*Room) *Room
	UserId(u ...int64) int64
}
