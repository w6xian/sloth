package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/w6xian/sloth"
	"github.com/w6xian/sloth/group"
	"github.com/w6xian/sloth/internal/utils"
	"github.com/w6xian/sloth/nrpc/wsocket"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

func main() {
	ln, err := net.Listen("tcp", "localhost:8080")
	if err != nil {
		panic(err)
	}
	r := mux.NewRouter()
	client := sloth.NewClientRpc()
	newConnect := sloth.NewConnect(sloth.WithClientLogic(client))
	newConnect.RegisterRpc("v1", &HelloService{}, "")
	newConnect.StartWebsocketServer(
		wsocket.WithRouter(r),
		wsocket.WithHandleMessage(&Handler{}),
	)
	go func() {
		for {
			time.Sleep(5 * time.Second)
			data, err := client.Call(context.Background(), 2, "shop.Test", "abc")
			if err != nil {
				fmt.Println("Call error:", err)
				continue
			}
			fmt.Println("Call success:", string(data))
		}
	}()
	http.Handle("/", r)
	http.Serve(ln, nil)
}

type Handler struct {
}

func (h *Handler) HandleMessage(s *wsocket.WsServer, ch group.IChannel, msgType int, message []byte) error {
	if msgType == websocket.TextMessage {
		// fmt.Println("------------login------------")
		if ch.UserId() == 0 {
			userId := int64(2)
			roomId := int64(1)
			//1房1用户
			b := s.Bucket(userId)
			//insert into a bucket
			err := b.Put(userId, roomId, ch)
			return err
		}
	}
	fmt.Println(string(message))
	return nil
}

type HelloService struct {
}

func (h *HelloService) Test(ctx context.Context, data []byte) ([]byte, error) {
	return utils.Serialize(map[string]string{"req": "server 1", "time": time.Now().Format("2006-01-02 15:04:05")}), nil
}
func (h *HelloService) Login(ctx context.Context, data []byte) ([]byte, error) {
	return utils.Serialize(map[string]string{"user_id": "2", "time": time.Now().Format("2006-01-02 15:04:05")}), nil
}
