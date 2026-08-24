package nrpc

import (
	"context"
	"net/http"

	"github.com/w6xian/sloth/v3/internal/codec"
	"github.com/w6xian/sloth/v3/internal/transport"
)

// RouteMessage is the protocol-neutral callback used by transports to route a frame.
type RouteMessage = transport.RouteHandler

type RouteArgs struct {
	Context     context.Context
	Request     *http.Request
	Data        []byte
	OnFn        RouteMessage
	OnData      RouteMessage
	Codec       codec.Codec
	MessageType int
}

func DispatchMessage(args RouteArgs) error {
	if args.Context == nil {
		args.Context = context.Background()
	}

	if args.Codec != nil {
		return transport.NewFrameRouterWithCodec(args.OnFn, args.OnData, args.Codec).Dispatch(args.Context, args.Data)
	}
	return transport.NewFrameRouter(args.OnFn, args.OnData).Dispatch(args.Context, args.Data)
}
