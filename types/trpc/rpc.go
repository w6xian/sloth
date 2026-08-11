package trpc

import (
	"context"

	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/types"
	"github.com/w6xian/sloth/v3/types/auth"
)

type RpcAction struct {
	Action int `json:"action"`
}

type RpcCaller struct {
	Method  string            `json:"method"`
	Header  map[string]string `json:"header,omitempty"`
	Data    []byte            `json:"data"`
	Args    [][]byte          `json:"args,omitempty"`  // args
	Error   string            `json:"error,omitempty"` // error message
	Channel IWsReply          `json:"-"`
}

type ICallRpc interface {
	CallFunc(ctx context.Context, s types.IBucket, msgReq *RpcCaller) ([]byte, error)
	CallNetFunc(ctx context.Context, service string, msgId uint64, payload []byte) ([]byte, error)
	IsRegisteredService(service string) bool
	Options() *option.Options
}

type ICall interface {
	Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error)
	Push(ctx context.Context, msg *message.Msg) error
	DefaultHeader() message.Header
	GetAuthInfo() (*auth.AuthInfo, error)
	SetAuthInfo(auth *auth.AuthInfo) error
}

type IWsReply interface {
	Reply(id uint64, data []byte, err error) error
}

type IChannel interface {
	Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error)
	Push(ctx context.Context, msg *message.Msg) error
	GetAuthInfo() (*auth.AuthInfo, error)
	SetAuthInfo(auth *auth.AuthInfo) error
}
