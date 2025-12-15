package wsocket

import "time"

type ClientOption func(s *LocalClient)

// writeWait default eq 10s
func WriteWait(wait time.Duration) ClientOption {
	return func(s *LocalClient) {
		s.WriteWait = wait
	}
}

// readWait default eq 10s
func ReadWait(wait time.Duration) ClientOption {
	return func(s *LocalClient) {
		s.ReadWait = wait
	}
}

func WithClientHandle(handler IClientHandleMessage) ClientOption {
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
