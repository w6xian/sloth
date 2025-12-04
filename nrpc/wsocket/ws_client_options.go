package wsocket

type LocalClientOption func(s *LocalClient)

func WithClientHandleMessage(handler IClientHandleMessage) ClientOption {
	return func(s *LocalClient) {
		s.handler = handler
	}
}

// s.uriPath = "/ws"
// s.serverUri = "127.0.0.1:8080"

func WithClientUriPath(uriPath string) ClientOption {
	return func(s *LocalClient) {
		s.uriPath = uriPath
	}
}

func WithClientServerUri(serverUri string) ClientOption {
	return func(s *LocalClient) {
		s.serverUri = serverUri
	}
}
