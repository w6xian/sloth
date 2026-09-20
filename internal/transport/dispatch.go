package transport

import (
	"context"

	"github.com/w6xian/sloth/v4/internal/codec"
)

// RouteHandler is the protocol-neutral hook for dispatching a frame.
type RouteHandler func(ctx context.Context, raw []byte) error

// FrameRouter separates protocol frame recognition from the business path.
// It is intentionally protocol-agnostic so that ws / tcp / kcp can share the same
// routing rules while each concrete transport only supplies its own raw data.
type FrameRouter struct {
	codec       codec.Codec
	FnHandler   RouteHandler
	DataHandler RouteHandler
}

// NewFrameRouter creates a router using the default FN codec.
func NewFrameRouter(fnHandler, dataHandler RouteHandler) *FrameRouter {
	return NewFrameRouterWithCodec(fnHandler, dataHandler, codec.DefaultFnCodec())
}

// NewFrameRouterWithCodec creates a router with a specific codec implementation.
func NewFrameRouterWithCodec(fnHandler, dataHandler RouteHandler, c codec.Codec) *FrameRouter {
	return &FrameRouter{codec: c, FnHandler: fnHandler, DataHandler: dataHandler}
}

func (r *FrameRouter) Dispatch(ctx context.Context, raw []byte) error {
	if raw == nil {
		return nil
	}
	// 帧识别统一走 codec.Select（与 HandleFn 的解码入口同一个判定），
	// 避免"路由认为是业务数据、解码又当成 FN 帧"这类分叉。
	c := r.codec
	if c == nil {
		c, _ = codec.Select(raw)
	} else if !c.Detect(raw) {
		c = nil
	}
	if c != nil {
		if r.FnHandler != nil {
			return r.FnHandler(ctx, raw)
		}
		return nil
	}
	if r.DataHandler != nil {
		return r.DataHandler(ctx, raw)
	}
	return nil
}

// DispatchFrame preserves legacy call-sites that only need a one-shot dispatch.
func DispatchFrame(ctx context.Context, raw []byte, onFn, onData RouteHandler) error {
	return NewFrameRouter(onFn, onData).Dispatch(ctx, raw)
}
