package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/w6xian/sloth/v3"
	"github.com/w6xian/sloth/v3/internal/utils"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/types"
	"github.com/w6xian/sloth/v3/types/auth"
	"github.com/w6xian/sloth/v3/types/trpc"

	"github.com/gorilla/websocket"
)

// AB is a test struct
type AB struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

// main entry point for the WebSocket client
func main() {
	runtime := context.Background()
	ctx, cancel := context.WithCancel(runtime)
	defer cancel()

	client := sloth.DefaultClient()
	newConnect := sloth.ClientConn(client)
	newConnect.Register("shop", &HelloService{}, "")
	// Get service methods

	// Start WebSocket Client in a goroutine

	go newConnect.Dial(ctx, "ws", "localhost:8990")

	// Main loop for making RPC calls
	func() {
		for {
			time.Sleep(time.Millisecond * 2000)

			// If not authenticated/signed in, do so
			if client.UserId == 0 {
				client.Header.Set("APP_ID", "1")
				client.Header.Set("USER_ID", "1")
				data, err := client.Call(context.Background(), "v1.Sign", []byte("sign"))
				fmt.Println("------------")
				fmt.Println("Sign result", data, err)
				fmt.Println("------------")
				if err != nil {
					fmt.Println("v1.Sign Call error:", err)
					continue
				}
				auth := &auth.AuthInfo{}
				err = json.Unmarshal(data, auth)
				if err != nil {
					continue
				}
				fmt.Println(auth)
				client.SetAuthInfo(auth)
				fmt.Println("v1.Sign Call success:")
			}

			// // Example RPC call with header and various arguments
			// data, err := client.CallWithHeader(context.Background(), message.Header{
			// 	"APP_ID":  "header_app_id",
			// 	"USER_ID": "1",
			// }, "v1.Test", &AB{A: 1, B: 2},
			// )
			// fmt.Println("v1.Test Call result:", data, err)
			// Example RPC call with header and various arguments
			data1, err := client.CallWithHeader(context.Background(), message.Header{
				"APP_ID":  "header_app_id",
				"USER_ID": "1",
			}, "shop1.Test1", []byte("abc"),
			)
			fmt.Println("shop1.Test1:", string(data1), err)

			data2, err := client.CallWithHeader(context.Background(), message.Header{
				"APP_ID":  "header_app_id",
				"USER_ID": "1",
			}, "shop2.Test1", []byte("abc"),
			)
			fmt.Println("shop2.Test1:", string(data2), err)
		}
	}()

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
func (h *HelloService) Test(ctx context.Context, b []byte) ([]byte, error) {
	fmt.Println("Test args:", b)
	ch := ctx.Value(sloth.ChannelKey).(trpc.IChannel)
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	fmt.Println("Test header:", ctx.Value(sloth.HeaderKey).(message.Header))

	auth, err := ch.GetAuthInfo()
	if err != nil {
		return nil, err
	}
	fmt.Println("Test args:", auth)

	return utils.Serialize(map[string]string{"req": "local test", "time": time.Now().Format("2006-01-02 15:04:05")}), nil
}

// Hello struct
type Hello struct {
	Name string `json:"name"`
}
