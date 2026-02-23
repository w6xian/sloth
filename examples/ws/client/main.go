package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/w6xian/sloth"
	"github.com/w6xian/sloth/internal/utils"
	"github.com/w6xian/sloth/message"
	"github.com/w6xian/sloth/nrpc"
	"github.com/w6xian/sloth/nrpc/wsocket"
	"github.com/w6xian/sloth/pprof"
	"github.com/w6xian/tlv"

	"github.com/gorilla/websocket"
)

// main entry point for the WebSocket client
func main() {

	client := sloth.DefaultClient()
	newConnect := sloth.ClientConn(client)
	newConnect.Register("shop", &HelloService{}, "")
	// Get service methods

	// Start WebSocket Client in a goroutine
	go newConnect.StartWebsocketClient(
		wsocket.WithClientHandle(&Handler{server: client}),
		wsocket.WithClientUriPath("/ws"),
		wsocket.WithClientServerUri("localhost:8990"),
	)

	// Main loop for making RPC calls
	func() {
		for {
			time.Sleep(time.Second)

			// If not authenticated/signed in, do so
			if client.UserId == 0 {
				client.Header.Set("APP_ID", "1")
				client.Header.Set("USER_ID", "1")
				data, err := client.Call(context.Background(), "v1.Sign", []byte("sign"))
				if err != nil {
					fmt.Println("Call error:", err)
					continue
				}
				auth := &nrpc.AuthInfo{}
				err = tlv.Json2Struct(data, auth)
				if err != nil {
					fmt.Println("Deserialize error:", err)
					continue
				}
				fmt.Println("Sign success:", auth)
				client.SetAuthInfo(auth)
			}

			// Example RPC call with header and various arguments
			data, err := client.CallWithHeader(context.Background(), message.Header{
				"APP_ID":  "header_app_id",
				"USER_ID": "1",
			}, "pprof.Info", []byte("abc"),
				int(utils.RandInt64(1, 0xFFFF)),
				HelloReq{Name: "w6xian"}, &Hello{Name: "w6xian ptr"},
				"w6xian_str",
				&[]byte{0x01, 0x02, 0x03, 0x04},
				[]string{"a", "b", "c"},
				&[]string{"a", "b", "c"},
			)
			if err != nil {
				fmt.Println("Call error:", err)
				continue
			}
			info := &pprof.PProfInfo{}
			err = utils.Deserialize(data, info)
			if err != nil {
				fmt.Println("Deserialize error:", err)
				continue
			}
			fmt.Println("Call success:", info)
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
func (h *Handler) OnClose(ctx context.Context, c *wsocket.LocalClient, ch *wsocket.WsChannelClient) error {
	fmt.Println("OnClose:", ch.UserId)
	return nil
}

// OnData handles received messages
func (h *Handler) OnData(ctx context.Context, c *wsocket.LocalClient, ch *wsocket.WsChannelClient, msgType int, message []byte) error {
	if msgType == websocket.TextMessage {
		fmt.Println("HandleMessage:", 1, string(message))
	}

	return nil
}

// OnError handles errors
func (h *Handler) OnError(ctx context.Context, c *wsocket.LocalClient, ch *wsocket.WsChannelClient, err error) error {
	fmt.Println("OnError:", err.Error())
	return nil
}

// OnOpen is called when connection is opened
func (h *Handler) OnOpen(ctx context.Context, c *wsocket.LocalClient, ch *wsocket.WsChannelClient) error {
	fmt.Println("OnOpen:", ch.UserId, h.server)
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
	ch := ctx.Value(sloth.ChannelKey).(nrpc.IChannel)
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	fmt.Println("Test header:", ctx.Value(sloth.HeaderKey).(message.Header))

	auth, err := ch.GetAuthInfo()
	if err != nil {
		return nil, err
	}
	fmt.Println("Test args:", auth)

	return utils.Serialize(map[string]string{"req": "local 1", "time": time.Now().Format("2006-01-02 15:04:05")}), nil
}

// Hello struct
type Hello struct {
	Name string `json:"name"`
}
