package slots

import (
	"context"
	"net/http"

	"github.com/w6xian/sloth/v3"
	"github.com/w6xian/sloth/v3/types"
)

// handles client-side WebSocket events
type Client struct {
	server *sloth.ServerRpc
}

// OnConnect is called when connection is connected
func (h *Client) OnConnect(ctx context.Context, resp *http.Response) error {
	return nil
}

// OnClose is called when connection is closed
func (h *Client) OnClose(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo) error {

	return nil
}

// OnData handles received messages
func (h *Client) OnData(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo, msgType int, message []byte) error {
	return nil
}

// OnError handles errors
func (h *Client) OnError(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo, err error) error {
	return nil
}

// OnReady is called when connection is opened
func (h *Client) OnReady(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo) error {
	return nil
}
