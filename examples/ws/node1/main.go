package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/w6xian/sloth/v2"
	"github.com/w6xian/sloth/v2/internal/utils"
	"github.com/w6xian/sloth/v2/types"
	"github.com/w6xian/sloth/v2/types/auth"
	"github.com/w6xian/sloth/v2/types/trpc"
	"github.com/w6xian/tlv"

	"github.com/gorilla/websocket"
)

// AB is a test struct
type AB struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

var name = "shop2"

// main entry point for the WebSocket client
func main() {
	runtime := context.Background()
	ctx, cancel := context.WithCancel(runtime)
	defer cancel()

	client := sloth.DefaultClient()
	newConnect := sloth.ClientConn(client)
	newConnect.Register(name, &HelloService{}, "")
	// Get service methods

	// Start WebSocket Client in a goroutine

	go newConnect.Dial(ctx, "ws", "localhost:8990")

	// Main loop for making RPC calls
	for {
		time.Sleep(time.Millisecond * 5000)
		// If not authenticated/signed in, do so
		if client.UserId == 0 {
			client.Header.Set("APP_ID", "1")
			client.Header.Set("USER_ID", "1")
			data, err := client.Call(context.Background(), "v1.Reg", name)
			if err != nil {
				fmt.Println("v1.Reg Call error:", err)
				continue
			}
			auth := &auth.AuthInfo{}
			err = tlv.Json2Struct(data, auth)
			if err != nil {
				continue
			}
			fmt.Println(auth)
			client.SetAuthInfo(auth)
			fmt.Println("v1.Sign Call success:")
			break
		}
	}
	select {}

}

// IotSignReq represents IoT signing request
type IotSignReq struct {
	Code  string `json:"code"`
	Token string `json:"token"`
}

// HelloReq represents hello request
type HelloReq struct {
	Name string `json:"name"`
}

// Handler handles client-side WebSocket events
type Handler struct {
	server *sloth.ServerRpc
}

// OnClose is called when connection is closed
func (h *Handler) OnClose(ctx context.Context, c types.IConnRpc, ch types.IConnInfo) error {
	fmt.Println("OnClose:", ch.GetUserId())
	return nil
}

// OnData handles received messages
func (h *Handler) OnData(ctx context.Context, c types.IConnRpc, ch types.IConnInfo, msgType int, message []byte) error {
	if msgType == websocket.TextMessage {
		fmt.Println("HandleMessage:", 1, string(message))
	}

	return nil
}

// OnError handles errors
func (h *Handler) OnError(ctx context.Context, c types.IConnRpc, ch types.IConnInfo, err error) error {
	fmt.Println("OnError:", err.Error())
	return nil
}

// OnOpen is called when connection is opened
func (h *Handler) OnOpen(ctx context.Context, c types.IConnRpc, ch types.IConnInfo) error {
	fmt.Println("OnOpen:", ch.GetUserId(), h.server)
	// Example of sending an initial message or setting state
	// ch.UserId = 2
	// ch.RoomId = 1
	// h.server.Send(context.Background(), map[string]string{"user_id": "2", "room_id": "1"})
	return nil
}

// HelloService implements client-side service methods
type HelloService struct {
}

// Test is a sample client-side method
func (h *HelloService) Test1(ctx context.Context, b []byte) ([]byte, error) {
	ch := ctx.Value(sloth.ChannelKey).(trpc.IChannel)
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	_, err := ch.GetAuthInfo()
	if err != nil {
		return nil, err
	}

	return utils.Serialize(map[string]string{"req": "local." + name + ".Test1", "time": time.Now().Format("2006-01-02 15:04:05")}), nil
}

// Hello struct
type Hello struct {
	Name string `json:"name"`
}
