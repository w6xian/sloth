package nrpc

import (
	"context"

	"github.com/w6xian/sloth/message"
	"github.com/w6xian/sloth/options"
)

type ICallRpc interface {
	CallFunc(ctx context.Context, msgReq *RpcCaller) ([]byte, error)
	Options() *options.Options
}

type RpcCaller struct {
	Id     uint64 `json:"id"`
	Action int    `json:"action"`
	Method string `json:"method"`
	Data   []byte `json:"data"`
	Error  string `json:"error,omitempty"` // error message
}

type ICall interface {
	Call(ctx context.Context, mtd string, args []byte) ([]byte, error)
	// channel / client中实现
	Push(ctx context.Context, msg *message.Msg) (err error)
}
