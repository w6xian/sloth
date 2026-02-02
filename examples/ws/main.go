package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/w6xian/sloth"
	"github.com/w6xian/sloth/bucket"
	"github.com/w6xian/sloth/internal/utils"
	"github.com/w6xian/sloth/message"
	"github.com/w6xian/sloth/nrpc"
	"github.com/w6xian/sloth/nrpc/wsocket"
	"github.com/w6xian/tlv"
)

// main entry point for the WebSocket server
func main() {
	ln, err := net.Listen("tcp", "localhost:8990")
	if err != nil {
		panic(err)
	}
	r := mux.NewRouter()

	// Create default RPC server
	server := sloth.DefaultServer()
	newConnect := sloth.ServerConn(server)

	// Register services
	newConnect.Register("v1", &HelloService{}, "")

	// Set up WebSocket listener options
	newConnect.ListenOption(
		wsocket.WithRouter(r),
		wsocket.WithServerHandle(&Handler{}),
	)

	http.Handle("/", r)
	fmt.Println("WebSocket server listening on localhost:8990")

	// Start HTTP server
	if err := http.Serve(ln, nil); err != nil {
		fmt.Println("Server error:", err)
	}
}

// Hello represents a simple message structure
type Hello struct {
	Name string `json:"name"`
}

// Handler implements WebSocket event handling
type Handler struct {
}

// OnClose is called when a WebSocket connection is closed
func (h *Handler) OnClose(ctx context.Context, s *wsocket.WsServer, ch bucket.IChannel) error {
	fmt.Println("OnClose")
	return nil
}

// OnError is called when a WebSocket error occurs
func (h *Handler) OnError(ctx context.Context, s *wsocket.WsServer, ch bucket.IChannel, err error) error {
	fmt.Println("OnError:", err)
	return nil
}

// OnOpen is called when a new WebSocket connection is established
func (h *Handler) OnOpen(ctx context.Context, s *wsocket.WsServer, ch bucket.IChannel) error {
	fmt.Println("OnOpen")
	return nil
}

// OnData is called when data is received from a WebSocket connection
func (h *Handler) OnData(ctx context.Context, s *wsocket.WsServer, ch bucket.IChannel, msgType int, message []byte) error {
	// Simple authentication/bucketing logic
	if ch.UserId() == 0 {
		userId := int64(2)
		roomId := int64(1)
		// Assign user to a bucket (room)
		b := s.Bucket(userId)
		err := b.Put(userId, roomId, "token", ch)
		return err
	}
	return nil
}

// HelloReq represents the request for Hello service
type HelloReq struct {
	Name string `json:"name"`
}

// HelloService implements the RPC service
type HelloService struct {
	Id int64 `json:"id"`
}

// AB is a test struct
type AB struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

// Test is a sample RPC method
func (h *HelloService) Test(ctx context.Context, ab *AB) (any, error) {
	h.Id = h.Id + 1

	// Retrieve context values
	fmt.Println("Test args (Channel):", ctx.Value(sloth.ChannelKey).(bucket.IChannel))
	fmt.Println("Test header:", ctx.Value(sloth.HeaderKey).(message.Header))
	fmt.Println("Test args (AB):", ab)

	// Simulate error
	if h.Id%5 == 1 {
		return nil, fmt.Errorf("error %d", h.Id)
	}

	return map[string]string{
		"req":  "server 1",
		"time": time.Now().Format("2006-01-02 15:04:05"),
	}, nil
}

// Sign handles user signing/authentication
func (h *HelloService) Sign(ctx context.Context, data []byte) ([]byte, error) {
	h.Id = h.Id + 1

	// Get channel from context
	ch, ok := ctx.Value(sloth.ChannelKey).(bucket.IChannel)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}

	// Get bucket server from context
	svr, ok := ctx.Value(sloth.BucketKey).(nrpc.IBucket)
	if !ok {
		return nil, fmt.Errorf("bucket not found")
	}

	fmt.Println("Test header:", ctx.Value(sloth.HeaderKey).(message.Header))

	// Simulate auth info extraction
	auth := nrpc.AuthInfo{
		UserId: 2,
		RoomId: 1,
		Token:  "token_123", // Added fake token
	}

	// Register session in bucket
	svr.Bucket(auth.UserId).Put(auth.UserId, auth.RoomId, auth.Token, ch)
	fmt.Println("Sign args:", string(data))

	return tlv.Json(auth), nil
}

// TestByte tests various parameter types
func (h *HelloService) TestByte(ctx context.Context, b []byte, i int, req HelloReq, resp *Hello, str *string, bytes *[]byte, strs []string, strsptr *[]string) (any, error) {
	h.Id = h.Id + 1

	fmt.Println("Test args (Channel):", ctx.Value(sloth.ChannelKey).(bucket.IChannel))
	fmt.Println("Test args (b):", b)
	fmt.Println("Test args (all):", string(b), i, req, resp, *str, *bytes, strs, *strsptr)

	if h.Id%5 == 1 {
		return nil, fmt.Errorf("error %d", h.Id)
	}

	return map[string]string{
		"req":  "server 1",
		"time": time.Now().Format("2006-01-02 15:04:05"),
	}, nil
}

// Login handles login requests
func (h *HelloService) Login(ctx context.Context, data []byte) ([]byte, error) {
	return utils.Serialize(map[string]string{
		"user_id": "2",
		"time":    time.Now().Format("2006-01-02 15:04:05"),
	}), nil
}
