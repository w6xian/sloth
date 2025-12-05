package nrpc

import (
	"context"

	"github.com/w6xian/sloth/message"
)

type ICallRpc interface {
	CallFunc(ctx context.Context, msgReq *RpcCaller) ([]byte, error)
}

type RpcCaller struct {
	Id     uint64 `json:"id"`
	Action int    `json:"action"`
	Method string `json:"method"`
	Data   []byte `json:"data"`
	Error  string `json:"error,omitempty"` // error message
}

type ICall interface {
	Call(ctx context.Context, mtd string, args any) ([]byte, error)
	Push(ctx context.Context, msg *message.Msg) (err error)
}
