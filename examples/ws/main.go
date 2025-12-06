package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/w6xian/sloth"
	"github.com/w6xian/sloth/decoder"
	"github.com/w6xian/sloth/decoder/tlv"
	"github.com/w6xian/sloth/group"
	"github.com/w6xian/sloth/internal/utils"
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
		wsocket.WithServerHandle(&Handler{}),
	)
	go func() {
		for {
			time.Sleep(5 * time.Second)
			data, err := client.Call(context.Background(), 2, "shop.Test", []byte("abc"))
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

// OnClose implements wsocket.IServerHandleMessage.
func (h *Handler) OnClose(ctx context.Context, s *wsocket.WsServer, ch group.IChannel) error {
	fmt.Println("OnClose")
	return nil
}

// OnError implements wsocket.IServerHandleMessage.
func (h *Handler) OnError(ctx context.Context, s *wsocket.WsServer, ch group.IChannel, err error) error {
	fmt.Println("OnError:", err)
	return nil
}

// OnOpen implements wsocket.IServerHandleMessage.
func (h *Handler) OnOpen(ctx context.Context, s *wsocket.WsServer, ch group.IChannel) error {
	fmt.Println("OnOpen")
	return nil
}

func (h *Handler) OnData(ctx context.Context, s *wsocket.WsServer, ch group.IChannel, msgType int, message []byte) error {
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
	// c, err := sloth.Decode64ToTlv(data)
	// if err != nil {
	// 	fmt.Println("Decode64ToTlv error:", err)
	// 	return nil, err
	// }
	// fmt.Println("Decode64ToTlv success:", c)
	// fmt.Println("Decode64ToTlv success:", c.String())
	fmt.Println("Test success:", string(data))
	if tlv.IsTLVFrame(data) {
		args, err := tlv.Deserialize(data)
		if err != nil {
			fmt.Println("Deserialize error:", err)
			return nil, err
		}
		fmt.Println("Test success:", args.String())
	}
	fmt.Println(string(data))
	if h.Id%10 == 1 {
		// return utils.Serialize([]string{"a", "b", "c"}), nil
		mapData := map[string]string{
			"t": time.Now().Format("2006-01-02 15:04:05"),
			"b": "2",
			"c": "中国",
		}
		return sloth.Json(mapData, nil)
	}
	return utils.Serialize(map[string]string{"req": "server 1", "time": time.Now().Format("2006-01-02 15:04:05")}), nil
}
func (h *HelloService) Login(ctx context.Context, data []byte) ([]byte, error) {
	return utils.Serialize(map[string]string{"user_id": "2", "time": time.Now().Format("2006-01-02 15:04:05")}), nil
}
