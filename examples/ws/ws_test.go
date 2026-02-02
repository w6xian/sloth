package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/w6xian/sloth"
	"github.com/w6xian/sloth/nrpc"
	"github.com/w6xian/sloth/nrpc/wsocket"
	"github.com/w6xian/tlv"
)

// ClientHandler implements wsocket.IClientHandleMessage for testing
type ClientHandler struct{}

func (h *ClientHandler) OnClose(ctx context.Context, c *wsocket.LocalClient, ch *wsocket.WsChannelClient) error {
	return nil
}

func (h *ClientHandler) OnData(ctx context.Context, c *wsocket.LocalClient, ch *wsocket.WsChannelClient, msgType int, message []byte) error {
	if msgType == websocket.TextMessage {
		fmt.Println("Client HandleMessage:", string(message))
	}
	return nil
}

func (h *ClientHandler) OnError(ctx context.Context, c *wsocket.LocalClient, ch *wsocket.WsChannelClient, err error) error {
	return nil
}

func (h *ClientHandler) OnOpen(ctx context.Context, c *wsocket.LocalClient, ch *wsocket.WsChannelClient) error {
	return nil
}

// TestWebSocketRPCIntegration tests the full RPC flow over WebSocket
func TestWebSocketRPCIntegration(t *testing.T) {
	// 1. Setup Server
	r := mux.NewRouter()
	server := sloth.DefaultServer()
	newConnect := sloth.ServerConn(server)
	newConnect.Register("v1", &HelloService{}, "")
	newConnect.ListenOption(
		wsocket.WithRouter(r),
		wsocket.WithServerHandle(&Handler{}),
	)

	// Start a test HTTP server
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Parse the test server URL to get the address
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	host := u.Host // e.g., "127.0.0.1:54321"

	// 2. Setup Client
	client := sloth.DefaultClient()
	clientConnect := sloth.ClientConn(client)

	// Start WebSocket Client
	go func() {
		clientConnect.StartWebsocketClient(
			wsocket.WithClientHandle(&ClientHandler{}),
			wsocket.WithClientUriPath("/ws"),
			wsocket.WithClientServerUri(host),
		)
	}()

	// Wait for connection to establish (naive approach)
	time.Sleep(100 * time.Millisecond)

	// 3. Perform RPC Call (Sign)
	client.Header.Set("APP_ID", "test_app")
	client.Header.Set("USER_ID", "test_user")

	// Retry loop in case connection takes a bit longer
	var auth *nrpc.AuthInfo
	for i := 0; i < 5; i++ {
		data, err := client.Call(context.Background(), "v1.Sign", []byte("sign_test"))
		if err == nil {
			auth = &nrpc.AuthInfo{}
			err = tlv.Json2Struct(data, auth)
			if err == nil {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	if auth == nil {
		t.Fatal("Failed to call v1.Sign or parse response")
	}

	if auth.UserId != 2 || auth.RoomId != 1 {
		t.Errorf("Unexpected auth info: %+v", auth)
	}

	// 4. Perform another RPC Call (Test)
	ab := &AB{A: 10, B: 20}
	data, err := client.Call(context.Background(), "v1.Test", ab)
	if err != nil {
		t.Fatalf("Failed to call v1.Test: %v", err)
	}

	fmt.Printf("Test response: %s\n", string(data))

	if !strings.Contains(string(data), "server 1") {
		t.Errorf("Unexpected response: %s", string(data))
	}
}

// BenchmarkWebSocketRPC benchmarks the RPC call performance
func BenchmarkWebSocketRPC(b *testing.B) {
	// Setup Server
	r := mux.NewRouter()
	server := sloth.DefaultServer()
	newConnect := sloth.ServerConn(server)
	newConnect.Register("v1", &HelloService{}, "")
	newConnect.ListenOption(
		wsocket.WithRouter(r),
		wsocket.WithServerHandle(&Handler{}),
	)
	ts := httptest.NewServer(r)
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	host := u.Host

	// Setup Client
	client := sloth.DefaultClient()
	clientConnect := sloth.ClientConn(client)

	go clientConnect.StartWebsocketClient(
		wsocket.WithClientHandle(&ClientHandler{}),
		wsocket.WithClientUriPath("/ws"),
		wsocket.WithClientServerUri(host),
	)

	time.Sleep(200 * time.Millisecond)

	// Prepare args
	ab := &AB{A: 1, B: 2}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.Call(context.Background(), "v1.Test", ab)
		if err != nil {
			// b.Fatal(err)
		}
	}
}
