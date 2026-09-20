package option

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/w6xian/sloth/v3/internal/codec"
	"github.com/w6xian/sloth/v3/types/handler"
)

type IConnectOption interface {
	SetUriPath(path string) error
	SetRouter(router *mux.Router) error
	SetAddress(address string) error
	SetServerHandleMessage(handler handler.IServerHandleMessage) error
	SetClientHandleMessage(handler handler.IClientHandleMessage) error
	SetHeader(key string, value string) error
	SetOrigin(origins ...string) error
	SetCodec(c codec.Codec)
}

type ConnectOption func(s IConnectOption)

func (o *ConnectOption) String() string {
	return "ConnectOption"
}

func WithUriPath(path string) ConnectOption {
	return func(s IConnectOption) {
		s.SetUriPath(path)
	}
}

func WithAddress(path string) ConnectOption {
	return func(s IConnectOption) {
		s.SetAddress(path)
	}
}

func WithRouter(router *mux.Router, path string) ConnectOption {
	return func(s IConnectOption) {
		s.SetRouter(router)
		http.Handle(path, router)
	}
}
func WithRouterWithoutHandle(router *mux.Router) ConnectOption {
	return func(s IConnectOption) {
		s.SetRouter(router)
	}
}

func WithServerHandleMessage(handler handler.IServerHandleMessage) ConnectOption {
	return func(s IConnectOption) {
		s.SetServerHandleMessage(handler)
	}

}

func WithClientHandleMessage(handler handler.IClientHandleMessage) ConnectOption {
	return func(s IConnectOption) {
		s.SetClientHandleMessage(handler)
	}
}

func WithRequestHeader(key string, value string) ConnectOption {
	return func(s IConnectOption) {
		s.SetHeader(key, value)
	}
}

func WithOrigin(origins ...string) ConnectOption {
	return func(s IConnectOption) {
		s.SetOrigin(origins...)
	}
}

func WithCodec(c codec.Codec) ConnectOption {
	return func(s IConnectOption) {
		s.SetCodec(c)
	}
}

// WithDebugHandler 把调试端点（/debug/metrics、/debug/pprof/*、/debug/vars）
// 挂到当前服务端的 HTTP 路由上，与业务端口共用监听地址。
//
// 仅建议内网环境：端点无鉴权。对外服务请用 option.WithDebugAddr 起独立端口，
// 或自行用 http.ServeMux 挂载（DebugHandler 返回的 handler）。
//
// 该选项只对实现 SetDebug 的服务端（如 *wsocket.WsServer）生效，客户端侧忽略。
func WithDebugHandler() ConnectOption {
	return func(s IConnectOption) {
		if v, ok := any(s).(interface{ SetDebug(bool) }); ok {
			v.SetDebug(true)
		}
	}
}
