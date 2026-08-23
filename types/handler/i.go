package handler

import (
	"context"
	"net/http"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/types"
)

type IServerHandleMessage interface {
	OnConnect(ctx context.Context, r *http.Request) error
	OnReady(ctx context.Context, r *http.Request, s types.IBucket, ch bucket.IChannel) error
	OnClose(ctx context.Context, r *http.Request, s types.IBucket, ch bucket.IChannel) error
	OnData(ctx context.Context, r *http.Request, s types.IBucket, ch bucket.IChannel, msgType int, message []byte) error
	OnError(ctx context.Context, r *http.Request, s types.IBucket, ch bucket.IChannel, err error) error
}

type IClientHandleMessage interface {
	OnConnect(ctx context.Context, resp *http.Response) error
	OnReady(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo) error
	OnData(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo, msgType int, message []byte) error
	OnClose(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo) error
	OnError(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo, err error) error
}

type IHandleMessage interface {
	OnConnect(ctx context.Context, r *http.Request, w *http.Response) error
	OnReady(ctx context.Context, r *http.Request, w *http.Response, c types.IActionHandler, ch types.IConnInfo) error
	OnData(ctx context.Context, r *http.Request, w *http.Response, c types.IActionHandler, ch types.IConnInfo, msgType int, message []byte) error
	OnClose(ctx context.Context, r *http.Request, w *http.Response, c types.IActionHandler, ch types.IConnInfo) error
	OnError(ctx context.Context, r *http.Request, w *http.Response, c types.IActionHandler, ch types.IConnInfo, err error) error
}
