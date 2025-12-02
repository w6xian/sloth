package wsocket

type LocalClientOption func(s *LocalClient)

func WithClientHandleMessage(handler IClientHandleMessage) ClientOption {
	return func(s *LocalClient) {
		s.handler = handler
	}
}
