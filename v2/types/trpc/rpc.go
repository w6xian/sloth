package trpc

import (
	"context"

	"github.com/w6xian/sloth/v2/internal/logger"
	"github.com/w6xian/sloth/v2/message"
	"github.com/w6xian/sloth/v2/option"
	"github.com/w6xian/sloth/v2/types"
	"github.com/w6xian/sloth/v2/types/auth"
)

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

type ICallRpc interface {
	Log(level logger.LogLevel, line string, args ...any)
	CallFunc(ctx context.Context, s types.IBucket, msgReq *RpcCaller) ([]byte, error)
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
	ReplySuccess(id string, data []byte) error
	ReplyError(id string, err []byte) error
}

type IChannel interface {
	Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error)
	Push(ctx context.Context, msg *message.Msg) error
	GetAuthInfo() (*auth.AuthInfo, error)
	SetAuthInfo(auth *auth.AuthInfo) error
}
