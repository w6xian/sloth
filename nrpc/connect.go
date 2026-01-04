package nrpc

import (
	"context"

	"github.com/w6xian/sloth/bucket"
	"github.com/w6xian/sloth/internal/logger"
	"github.com/w6xian/sloth/message"
	"github.com/w6xian/sloth/options"
)

type ICallRpc interface {
	Log(level logger.LogLevel, line string, args ...any)
	CallFunc(ctx context.Context, s IBucket, msgReq *RpcCaller) ([]byte, error)
	Options() *options.Options
}

type IBucket interface {
	Bucket(userId int64) *bucket.Bucket
	Channel(userId int64) bucket.IChannel
	Room(roomId int64) *bucket.Room
	Broadcast(ctx context.Context, msg *message.Msg) error
}

type RpcAction struct {
	Action int `json:"action"`
}

type RpcCaller struct {
	Id       string            `json:"id"`
	Protocol int               `json:"protocol"` // 1 string 0 tlv
	Action   int               `json:"action"`
	Method   string            `json:"method"`
	Header   map[string]string `json:"header,omitempty"`
	Data     []byte            `json:"data"`
	Args     [][]byte          `json:"args,omitempty"`  // args
	Error    string            `json:"error,omitempty"` // error message
	Channel  IWsReply          `json:"-"`
}

type ICall interface {
	Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error)
	Push(ctx context.Context, msg *message.Msg) error
	GetAuthInfo() (*AuthInfo, error)
	SetAuthInfo(auth *AuthInfo) error
}

type IWsReply interface {
	ReplySuccess(id string, data []byte) error
	ReplyError(id string, err []byte) error
}

type IChannel interface {
	Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error)
	Push(ctx context.Context, msg *message.Msg) error
	GetAuthInfo() (*AuthInfo, error)
	SetAuthInfo(auth *AuthInfo) error
}

type AuthInfo struct {
	UserId int64  `json:"user_id"`
	RoomId int64  `json:"room_id"`
	Token  string `json:"token"`
}
