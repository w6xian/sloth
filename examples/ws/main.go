package main

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"time"

	"github.com/w6xian/sloth"
	"github.com/w6xian/sloth/decoder"
	"github.com/w6xian/sloth/group"
	"github.com/w6xian/sloth/internal/utils"
	"github.com/w6xian/sloth/internal/utils/id"
	"github.com/w6xian/sloth/nrpc/wsocket"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

func main() {
	ln, err := net.Listen("tcp", "localhost:8990")
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

func (h *Handler) HandleMessage(ctx context.Context, s *wsocket.WsServer, ch group.IChannel, msgType int, message []byte) error {
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
	} else if decoder.IsHdCFrame(message) {
		hdc, err := decoder.DecodeHdC(message)
		if err != nil {
			fmt.Println("DecodeHdC error:", err)
			return err
		}
		fmt.Println("DecodeHdC success:", string(hdc.Data()))
	}
	fmt.Println(string(message))
	return nil
}

type HelloService struct {
	Id int64 `json:"id"`
}

func (h *HelloService) Test(ctx context.Context, data []byte) ([]byte, error) {
	h.Id = h.Id + 1
	fmt.Println("HelloService.Test", h.Id)
	if h.Id%2 == 1 {
		return utils.Serialize([]string{"a", "b", "c"}), nil
		hdc := decoder.NewHdCReply(0x01, 0x03, []byte(time.Now().Format("2006-01-02 15:04:05")+"a中c"))
		return hdc.Frame(), nil
	}
	hdc := decoder.NewHdCReply(0x01, 0x03, []byte(time.Now().Format("2006-01-02 15:04:05")+id.RandStr(rand.Intn(50))))
	return hdc.Frame(), nil
	return utils.Serialize(map[string]string{"req": "server 1", "time": time.Now().Format("2006-01-02 15:04:05")}), nil
}
func (h *HelloService) Login(ctx context.Context, data []byte) ([]byte, error) {
	return utils.Serialize(map[string]string{"user_id": "2", "time": time.Now().Format("2006-01-02 15:04:05")}), nil
}
