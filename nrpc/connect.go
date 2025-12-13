package nrpc

import (
	"context"

	"github.com/w6xian/sloth/internal/logger"
	"github.com/w6xian/sloth/message"
	"github.com/w6xian/sloth/options"
)

type ICallRpc interface {
	Log(level logger.LogLevel, line string, args ...any)
	CallFunc(ctx context.Context, msgReq *RpcCaller) ([]byte, error)
	Options() *options.Options
}

type RpcAction struct {
	Action int `json:"action"`
}

type RpcCaller struct {
	Id       string   `json:"id"`
	Protocol int      `json:"protocol"` // 1 string 0 tlv
	Action   int      `json:"action"`
	Method   string   `json:"method"`
	Data     []byte   `json:"data"`
	Args     [][]byte `json:"args,omitempty"`  // args
	Error    string   `json:"error,omitempty"` // error message
}

type ICall interface {
	Call(ctx context.Context, mtd string, args ...[]byte) ([]byte, error)
	// channel / client中实现
	Push(ctx context.Context, msg *message.Msg) (err error)
}
