package handler

import (
	"context"

	"github.com/w6xian/sloth/v2/bucket"
	"github.com/w6xian/sloth/v2/types"
)

type IServerHandleMessage interface {
	OnData(ctx context.Context, s types.IBucket, ch bucket.IChannel, msgType int, message []byte) error
	OnClose(ctx context.Context, s types.IBucket, ch bucket.IChannel) error
	OnError(ctx context.Context, s types.IBucket, ch bucket.IChannel, err error) error
	OnOpen(ctx context.Context, s types.IBucket, ch bucket.IChannel) error
}

type IClientHandleMessage interface {
	OnData(ctx context.Context, c types.IConnRpc, ch types.IConnInfo, msgType int, message []byte) error
	OnClose(ctx context.Context, c types.IConnRpc, ch types.IConnInfo) error
	OnError(ctx context.Context, c types.IConnRpc, ch types.IConnInfo, err error) error
	OnOpen(ctx context.Context, c types.IConnRpc, ch types.IConnInfo) error
}
