package main

import (
	"context"
	"fmt"
	"sloth"
	"sloth/internal/utils"
	"sloth/nrpc/wsocket"
	"time"

	"github.com/gorilla/websocket"
)

func main() {

	server := sloth.NewServerRpc()
	newConnect := sloth.NewConnect(sloth.WithServerLogic(server))
	newConnect.RegisterRpc("shop", &HelloService{}, "")

	go func() {
		for {
			time.Sleep(time.Second)
			if server.UserId == 0 {
				data, err := server.Call(context.Background(), "v1.Login", "2")
				if err != nil {
					fmt.Println("Call error:", err)
					continue
				}
				server.UserId = 2
				server.RoomId = 1
				server.Send(context.Background(), utils.Serialize(map[string]string{"user_id": "2", "room_id": "1"}))
				fmt.Println("Call success:", string(data))
			}
			data, err := server.Call(context.Background(), "v1.Test", "abc")
			if err != nil {
				fmt.Println("Call error:", err)
				continue
			}
			fmt.Println("Call success:", string(data))
		}
	}()
	newConnect.StartWebsocketClient(
		wsocket.WithClientHandleMessage(&Handler{}),
	)
}

type Handler struct {
}

func (h *Handler) HandleMessage(s *wsocket.LocalClient, ch *wsocket.WsClient, msgType int, message []byte) error {
	if msgType == websocket.TextMessage {
		fmt.Println("HandleMessage:", 1, string(message))
	}
	fmt.Println(string(message))
	return nil
}

type HelloService struct {
}

func (h *HelloService) Test(ctx context.Context, data []byte) ([]byte, error) {
	return utils.Serialize(map[string]string{"req": "local 1", "time": time.Now().Format("2006-01-02 15:04:05")}), nil
}
