package bucket

import (
	"context"

	"github.com/w6xian/sloth/message"
)

type IChannel interface {
	// Call performs an RPC call with the given method and arguments, returning the response or an error.
	Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error)
	// Push sends a message to the channel without expecting a response.
	Push(ctx context.Context, msg *message.Msg) error
	ReplySuccess(id string, data []byte) error
	ReplyError(id string, err []byte) error
	Prev(p ...IChannel) IChannel
	Next(n ...IChannel) IChannel
	Room(r ...*Room) *Room
	UserId(u ...int64) int64
	Token(t ...string) string
	Close() error
}
