package sloth

import (
	"context"
	"net/http"

	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/types"
	"github.com/w6xian/sloth/v3/types/trpc"
)

type RpcServer interface {
	Start(addr string) error
}

type IRpc interface {
	SetEncoder(encoder Encoder)
	SetDecoder(decoder Decoder)
}

type Connecter interface {
	CallFunc(ctx context.Context, r *http.Request, w *http.Response, s types.IBucket, msgReq *trpc.RpcCaller) ([]byte, error)
	CallNetFunc(ctx context.Context, r *http.Request, w *http.Response, service string, msgId uint64, payload []byte) ([]byte, error)
	IsRegisteredService(service string) bool
	Options() *option.Options
}
