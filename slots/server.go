package slots

import (
	"context"
	"net/http"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/types"
)

// implements WebSocket event handling
type Server struct {
}

func (h *Server) OnConnect(ctx context.Context, r *http.Request) error {
	return nil
}

// OnClose is called when a WebSocket connection is closed
func (h *Server) OnClose(ctx context.Context, r *http.Request, s types.IBucket, ch bucket.IChannel) error {
	return nil
}

// OnError is called when a WebSocket error occurs
func (h *Server) OnError(ctx context.Context, r *http.Request, s types.IBucket, ch bucket.IChannel, err error) error {
	return nil
}

// OnOpen is called when a new WebSocket connection is established
func (h *Server) OnReady(ctx context.Context, r *http.Request, s types.IBucket, ch bucket.IChannel) error {
	return nil
}

// OnData is called when data is received from a WebSocket connection
func (h *Server) OnData(ctx context.Context, r *http.Request, s types.IBucket, ch bucket.IChannel, msgType int, msg []byte) error {
	return nil
}
