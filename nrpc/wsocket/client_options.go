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
