package option

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/w6xian/sloth/v2/types/handler"
)

type IConnectOption interface {
	SetUriPath(path string) error
	SetRouter(router *mux.Router) error
	SetAddress(address string) error
	SetServerHandleMessage(handler handler.IServerHandleMessage) error
	SetClientHandleMessage(handler handler.IClientHandleMessage) error
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

func WithRouter(router *mux.Router) ConnectOption {
	return func(s IConnectOption) {
		s.SetRouter(router)
		http.Handle("/", router)
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
