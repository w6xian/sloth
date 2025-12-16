package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/w6xian/sloth"
	"github.com/w6xian/sloth/bucket"
	"github.com/w6xian/sloth/decoder/tlv"
	"github.com/w6xian/sloth/internal/utils"
	"github.com/w6xian/sloth/nrpc"
	"github.com/w6xian/sloth/nrpc/wsocket"

	"github.com/gorilla/mux"
)

func main() {
	ln, err := net.Listen("tcp", "localhost:8990")
	if err != nil {
		panic(err)
	}
	r := mux.NewRouter()
	client := sloth.LinkClientFunc(
		sloth.UseEncoder(tlv.DefaultEncoder),
		sloth.UseDecoder(tlv.DefaultDecoder))
	newConnect := sloth.NewConnect(sloth.Client(client))
	newConnect.RegisterRpc("v1", &HelloService{}, "")
	newConnect.StartWebsocketServer(
		wsocket.WithRouter(r),
		wsocket.WithServerHandle(&Handler{}),
	)
	go func() {
		for {
			time.Sleep(5 * time.Second)
			data, err := client.Call(context.Background(), 2, "shop.Test", []byte("hello"))
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

type Hello struct {
	Name string `json:"name"`
}

type Handler struct {
}

// OnClose implements wsocket.IServerHandleMessage.
func (h *Handler) OnClose(ctx context.Context, s *wsocket.WsServer, ch bucket.IChannel) error {
	fmt.Println("OnClose")
	return nil
}

// OnError implements wsocket.IServerHandleMessage.
func (h *Handler) OnError(ctx context.Context, s *wsocket.WsServer, ch bucket.IChannel, err error) error {
	fmt.Println("OnError:", err)
	return nil
}

// OnOpen implements wsocket.IServerHandleMessage.
func (h *Handler) OnOpen(ctx context.Context, s *wsocket.WsServer, ch bucket.IChannel) error {
	fmt.Println("OnOpen")
	return nil
}

func (h *Handler) OnData(ctx context.Context, s *wsocket.WsServer, ch bucket.IChannel, msgType int, message []byte) error {

	if ch.UserId() == 0 {
		userId := int64(2)
		roomId := int64(1)
		//1房1用户
		b := s.Bucket(userId)
		//insert into a bucket
		err := b.Put(userId, roomId, "token", ch)
		return err
	}
	return nil
}

type HelloReq struct {
	Name string `json:"name"`
}

type HelloService struct {
	Id int64 `json:"id"`
}

type AB struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

func (h *HelloService) Test(ctx context.Context, ab *AB) (any, error) {
	h.Id = h.Id + 1
	// c, err := sloth.Decode64ToTlv(data)
	// if err != nil {
	// 	fmt.Println("Decode64ToTlv error:", err)
	// 	return nil, err
	// }
	// fmt.Println("Decode64ToTlv success:", c)
	// fmt.Println("Decode64ToTlv success:", c.String())
	// fmt.Println("Test args:", string(data))
	fmt.Println("Test args:", ctx.Value(sloth.ChannelKey).(bucket.IChannel))
	fmt.Println("Test args:", ab)
	if h.Id%5 == 1 {
		return nil, fmt.Errorf("error %d", h.Id)
		// mapData := map[string]string{
		// 	"t": time.Now().Format("2006-01-02 15:04:05"),
		// 	"b": "2",
		// 	"c": "中国",
		// }
		// return mapData, nil
	}
	return map[string]string{"req": "server 1", "time": time.Now().Format("2006-01-02 15:04:05")}, nil
}

func (h *HelloService) Sign(ctx context.Context, data []byte) ([]byte, error) {
	h.Id = h.Id + 1
	ch, ok := ctx.Value(sloth.ChannelKey).(bucket.IChannel)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}
	svr, ok := ctx.Value(sloth.BucketKey).(nrpc.IBucket)
	if !ok {
		return nil, fmt.Errorf("bucket not found")
	}
	//根据data登录 解析出userId,roomId,token
	auth := nrpc.AuthInfo{
		UserId: 2,
		RoomId: 1,
	}
	svr.Bucket(auth.UserId).Put(auth.UserId, auth.RoomId, auth.Token, ch)
	fmt.Println("Sign args:", string(data))
	return tlv.Marshal(auth), nil
}

func (h *HelloService) TestByte(ctx context.Context, b []byte, i int, req HelloReq, resp *Hello, str *string, bytes *[]byte, strs []string, strsptr *[]string) (any, error) {
	h.Id = h.Id + 1
	// c, err := sloth.Decode64ToTlv(data)
	// if err != nil {
	// 	fmt.Println("Decode64ToTlv error:", err)
	// 	return nil, err
	// }
	// fmt.Println("Decode64ToTlv success:", c)
	// fmt.Println("Decode64ToTlv success:", c.String())
	// fmt.Println("Test args:", b[0])
	fmt.Println("Test args:", ctx.Value(sloth.ChannelKey).(bucket.IChannel))
	fmt.Println("Test args:", b)
	fmt.Println("Test args:", string(b), i, req, resp, *str, *bytes, strs, *strsptr)
	if h.Id%5 == 1 {
		return nil, fmt.Errorf("error %d", h.Id)
		// mapData := map[string]string{
		// 	"t": time.Now().Format("2006-01-02 15:04:05"),
		// 	"b": "2",
		// 	"c": "中国",
		// }
		// return mapData, nil
	}
	return map[string]string{"req": "server 1", "time": time.Now().Format("2006-01-02 15:04:05")}, nil
}
func (h *HelloService) Login(ctx context.Context, data []byte) ([]byte, error) {
	return utils.Serialize(map[string]string{"user_id": "2", "time": time.Now().Format("2006-01-02 15:04:05")}), nil
}
