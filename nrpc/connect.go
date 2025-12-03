package nrpc

import (
	"context"

	"github.com/w6xian/sloth/message"
)

type ICallRpc interface {
	CallFunc(msgReq *RpcCaller) ([]byte, error)
}

type RpcCaller struct {
	Id     string `json:"id"`
	Action int    `json:"action"`
	Method string `json:"method"`
	Data   string `json:"data"`
}

type ICall interface {
	Call(ctx context.Context, mtd string, args any) ([]byte, error)
	Push(msg *message.Msg) (err error)
}
