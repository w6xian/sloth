package bucket

import (
	"context"

	"github.com/w6xian/sloth/v3/message"
)

type IChannel interface {
	// Call performs an RPC call with the given method and arguments, returning the response or an error.
	Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error)
	CallNet(ctx context.Context, msgId uint64, payload []byte) ([]byte, error)
	// Push sends a message to the channel without expecting a response.
	Push(ctx context.Context, msg *message.Msg) error
	ReplySuccess(id uint64, data []byte) error
	ReplyError(id uint64, err []byte) error
	Prev(p ...IChannel) IChannel
	Next(n ...IChannel) IChannel
	Room(r ...*Room) *Room
	UserId(u ...int64) int64
	Token(t ...string) string
	Close() error
}
