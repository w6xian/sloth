package wsocket

import "github.com/gorilla/mux"

type ServerOption func(s *WsServer)

func WithUriPath(path string) ServerOption {
	return func(s *WsServer) {
		s.uriPath = path
	}
}

func WithRouter(router *mux.Router) ServerOption {
	return func(s *WsServer) {
		s.router = router
	}
}

func WithHandleMessage(handler IServerHandleMessage) ServerOption {
	return func(s *WsServer) {
		s.handler = handler
	}
}
