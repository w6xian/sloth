package group

import (
	"context"
	"sloth/message"
)

type IChannel interface {
	Call(ctx context.Context, mtd string, args any) ([]byte, error)
	Push(ctx context.Context, req *message.Msg) error
	Reply(id string, data []byte) error
	Chans() (broadcast chan *message.Msg, rpcCaller chan *message.JsonCallObject, rpcBacker chan *message.JsonBackObject)
	Prev(p ...IChannel) IChannel
	Next(n ...IChannel) IChannel
	Room(r ...*Room) *Room
	UserId(u ...int64) int64
}
