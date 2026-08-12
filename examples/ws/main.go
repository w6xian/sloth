package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/w6xian/sloth/v3"
	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/internal/utils"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/slots"
	"github.com/w6xian/sloth/v3/types"
	"github.com/w6xian/sloth/v3/types/auth"
	"github.com/w6xian/tlv"
)

var smap *sloth.SMap

func init() {
	smap = sloth.NewSMap()
}

// main entry point for the WebSocket server
func main() {
	// Create a context with a cancel function
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	server := sloth.DefaultServer()
	drpc := sloth.ServerConn(server)
	r := mux.NewRouter()
	// Register services
	drpc.Register("v1", &HelloService{}, "metadata")
	drpc.Listen(ctx, "ws", "localhost:8990",
		option.WithRouter(r, "/ws"),
		option.WithServerHandleMessage(&Handler{}))

	drpc.UseProxyHandler(func(ctx context.Context, service string) (int64, error) {
		node, err := sloth.GetNode(service)
		if err != nil {
			return 0, err
		}
		svrId, ok := smap.Get(node.Service)
		if !ok {
			return 0, fmt.Errorf("service %s not registered", node.Service)
		}
		return svrId, nil
	})

	go func() {
		for {
			time.Sleep(time.Millisecond * 2000)
			rst, err := server.Call(ctx, 2, "shop.Test", nil, []byte{1}, 655360, true, tlv.Json(&AB{A: 1, B: 2}))
			if err != nil {
				fmt.Println("Call error:", err)
				continue
			}
			fmt.Println("Call result:", string(rst))
		}
	}()

	if err := drpc.Serve(); err != nil {
		panic(err)
	}
	fmt.Println("WebSocket server listening on localhost:8990")

}

// Hello represents a simple message structure
type Hello struct {
	Name string `json:"name"`
}

type Handler struct {
	slots.Server
}

func (h *Handler) OnConnect(ctx context.Context, r *http.Request) error {
	h.Server.OnConnect(ctx, r)
	fmt.Println("OnConnect Handler1", r.RemoteAddr)
	fmt.Println("OnConnect Handler1", r.RequestURI)
	fmt.Println("OnConnect Handler1")

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
	svr, ok := ctx.Value(sloth.BucketKey).(types.IBucket)
	if !ok {
		return nil, fmt.Errorf("bucket not found")
	}

	// Simulate auth info extraction
	auth := auth.AuthInfo{
		UserId: 2,
		RoomId: 1,
		Token:  "token_123", // Added fake token
	}

	// Register session in bucket
	svr.Bucket(auth.UserId).Put(auth.UserId, auth.RoomId, auth.Token, ch)
	return tlv.Json(auth), nil
}

func (h *HelloService) Reg(ctx context.Context, name string) ([]byte, error) {
	h.Id = h.Id + 1

	// Get channel from context
	ch, ok := ctx.Value(sloth.ChannelKey).(bucket.IChannel)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}
	// Get bucket server from context
	svr, ok := ctx.Value(sloth.BucketKey).(types.IBucket)
	if !ok {
		return nil, fmt.Errorf("bucket not found")
	}

	svrId, err := smap.Reg(name, false)
	if err != nil {
		return nil, err
	}

	// Simulate auth info extraction
	auth := auth.AuthInfo{
		UserId: svrId,
		RoomId: -1,
		Token:  "token_123", // Added fake token
	}
	// Register session in bucket
	svr.Bucket(auth.UserId).Put(auth.UserId, auth.RoomId, auth.Token, ch)
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
