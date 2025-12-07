package main

import (
	"context"
	"fmt"
	"time"

	"github.com/w6xian/sloth"
	"github.com/w6xian/sloth/decoder/tlv"
	"github.com/w6xian/sloth/internal/utils"
	"github.com/w6xian/sloth/nrpc/wsocket"

	"github.com/gorilla/websocket"
)

func main() {

	server := sloth.NewServerRpc(sloth.UseEncoder(tlv.DefaultEncoder), sloth.UseDecoder(tlv.DefaultDecoder))
	newConnect := sloth.NewConnect(sloth.WithServerLogic(server))
	newConnect.RegisterRpc("shop", &HelloService{}, "")

	go func() {
		for {
			time.Sleep(time.Second)
			// if server.UserId == 0 {
			// 	data, err := server.Call(context.Background(), "v1.Test", "2")
			// 	if err != nil {
			// 		fmt.Println("Call error:", err)
			// 		continue
			// 	}
			// 	server.UserId = 2
			// 	server.RoomId = 1
			// 	server.Send(context.Background(), utils.Serialize(map[string]string{"user_id": "2", "room_id": "1"}))
			// 	fmt.Println("Call success:", string(data))
			// }
			// args := tlv.FrameFromString("HelloService.Test 302 [34 97 98 99 34]")
			args := "HelloService.Test 302 [34 97 98 99 34]"
			data, err := server.Call(context.Background(), "v1.Test", args)
			if err != nil {
				fmt.Println("Call error:", err)
				continue
			}
			fmt.Println("Call success:", string(data))
		}
	}()
	newConnect.StartWebsocketClient(
		wsocket.WithClientHandle(&Handler{}),
		wsocket.WithClientUriPath("/ws"),
		wsocket.WithClientServerUri("localhost:8990"),
	)

}

type Handler struct {
}

// OnClose implements wsocket.IClientHandleMessage.
func (h *Handler) OnClose(ctx context.Context, c *wsocket.LocalClient, ch *wsocket.WsClient) error {
	fmt.Println("OnClose:", ch.UserId)
	return nil
}

// OnData implements wsocket.IClientHandleMessage.
func (h *Handler) OnData(ctx context.Context, c *wsocket.LocalClient, ch *wsocket.WsClient, msgType int, message []byte) error {
	if msgType == websocket.TextMessage {
		fmt.Println("HandleMessage:", 1, string(message))
	}
	fmt.Println(string(message))
	return nil
}

// OnError implements wsocket.IClientHandleMessage.
func (h *Handler) OnError(ctx context.Context, c *wsocket.LocalClient, ch *wsocket.WsClient, err error) error {
	fmt.Println("OnError:", err.Error())
	return nil
}

// onOpen implements wsocket.IClientHandleMessage.
func (h *Handler) OnOpen(ctx context.Context, c *wsocket.LocalClient, ch *wsocket.WsClient) error {
	fmt.Println("OnOpen:", ch.UserId)
	return nil
}

type HelloService struct {
}

func (h *HelloService) Test(ctx context.Context, data []byte) ([]byte, error) {
	return utils.Serialize(map[string]string{"req": "local 1", "time": time.Now().Format("2006-01-02 15:04:05")}), nil
}
