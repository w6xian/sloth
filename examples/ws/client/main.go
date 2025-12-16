package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/w6xian/sloth"
	"github.com/w6xian/sloth/decoder/tlv"
	"github.com/w6xian/sloth/internal/utils"
	"github.com/w6xian/sloth/nrpc"
	"github.com/w6xian/sloth/nrpc/wsocket"
	"github.com/w6xian/sloth/pprof"

	"github.com/gorilla/websocket"
)

func main() {

	server := sloth.LinkServerFunc(
		sloth.UseEncoder(tlv.DefaultEncoder),
		sloth.UseDecoder(tlv.DefaultDecoder))
	newConnect := sloth.NewConnect(sloth.Server(server))
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
			//server.Send(context.Background(), map[string]string{"user_id": "2", "room_id": "1"})
			// 	fmt.Println("Call success:", string(data))
			// }
			// args := tlv.FrameFromString("HelloService.Test 302 [34 97 98 99 34]")
			// args := "HelloService.Test 302 [34 97 98 99 34]"
			if server.UserId == 0 {
				data, err := server.Call(context.Background(), "v1.Sign", []byte("sign"))
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
				server.SetAuthInfo(auth)
			}

			data, err := server.Call(context.Background(), "pprof.Info", []byte("abc"),
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
	newConnect.StartWebsocketClient(
		wsocket.WithClientHandle(&Handler{server: server}),
		wsocket.WithClientUriPath("/ws"),
		// wsocket.WithClientServerUri("localhost:8990"),
		// ws addr: 0.0.0.0:8966
		wsocket.WithClientServerUri("localhost:8990"),
	)

}

type IotSignReq struct {
	Code  string `json:"code"`
	Token string `json:"token"`
}

type HelloReq struct {
	Name string `json:"name"`
}

type Handler struct {
	server *sloth.ServerRpc
}

// OnClose implements wsocket.IClientHandleMessage.
func (h *Handler) OnClose(ctx context.Context, c *wsocket.LocalClient, ch *wsocket.WsChannelClient) error {
	fmt.Println("OnClose:", ch.UserId)
	return nil
}

// OnData implements wsocket.IClientHandleMessage.
func (h *Handler) OnData(ctx context.Context, c *wsocket.LocalClient, ch *wsocket.WsChannelClient, msgType int, message []byte) error {
	if msgType == websocket.TextMessage {
		fmt.Println("HandleMessage:", 1, string(message))
	}

	return nil
}

// OnError implements wsocket.IClientHandleMessage.
func (h *Handler) OnError(ctx context.Context, c *wsocket.LocalClient, ch *wsocket.WsChannelClient, err error) error {
	fmt.Println("OnError:", err.Error())
	return nil
}

// onOpen implements wsocket.IClientHandleMessage.
func (h *Handler) OnOpen(ctx context.Context, c *wsocket.LocalClient, ch *wsocket.WsChannelClient) error {
	fmt.Println("OnOpen:", ch.UserId, h.server)
	ch.UserId = 2
	ch.RoomId = 1
	h.server.Send(context.Background(), map[string]string{"user_id": "2", "room_id": "1"})
	return nil
}

type HelloService struct {
}

func (h *HelloService) Test(ctx context.Context, b []byte) ([]byte, error) {
	fmt.Println("Test args:", b)
	ch := ctx.Value(sloth.ChannelKey).(nrpc.IChannel)
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	fmt.Println("Test args:", ch.GetAuthInfo())

	return utils.Serialize(map[string]string{"req": "local 1", "time": time.Now().Format("2006-01-02 15:04:05")}), nil
}

type Hello struct {
	Name string `json:"name"`
}
