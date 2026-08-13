package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/w6xian/sloth/v3"
	"github.com/w6xian/sloth/v3/internal/utils"
	"github.com/w6xian/sloth/v3/internal/utils/id"
	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/slots"
	"github.com/w6xian/sloth/v3/types/auth"
	"github.com/w6xian/sloth/v3/types/trpc"
	"github.com/w6xian/tlv"
)

// AB is a test struct
type AB struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

var name = "shop"

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

	go newConnect.Dial(ctx, "ws", "localhost:8990",
		option.WithClientHandleMessage(&Handler{}),
		option.WithRequestHeader("app_id", id.ShortStringID()))

	// Main loop for making RPC calls
	for {
		time.Sleep(time.Millisecond * 5000)
		// If not authenticated/signed in, do so
		// if client.UserId == 0 {
		// 	client.Header.Set("APP_ID", "1")
		// 	client.Header.Set("USER_ID", "1")
		// 	data, err := client.Call(context.Background(), "v1.Reg", name)
		// 	fmt.Println("------------")
		// 	t := &auth.AuthInfo{}
		// 	err = tlv.Json2Struct(data, t)
		// 	fmt.Println(string(data))
		// 	fmt.Println("Reg result", t)
		// 	d, err := t.Json()
		// 	fmt.Println("Reg result", string(d))

		// 	fmt.Println("------------")
		// 	if err != nil {
		// 		continue
		// 	}
		// 	auth := &auth.AuthInfo{}
		// 	err = json.Unmarshal(data, auth)
		// 	if err != nil {
		// 		continue
		// 	}
		// 	client.SetAuthInfo(auth)
		// 	break
		// }
		time.Sleep(time.Millisecond * 2000)
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
		data = tlv.Value(data)
		if err != nil {
			continue
		}

		fmt.Println(string(data))
		fmt.Println("v1.Sign Call success:")
		break

	}
	select {}

}

// HelloService implements client-side service methods
type HelloService struct {
	index int
}

type Handler struct {
	slots.Client
}

func (h *Handler) OnConnect(ctx context.Context, r *http.Response) error {
	fmt.Println("ttt")
	return nil
}

// Test is a sample client-side method
// []byte{1}, 655360, true, &AB{A: 1, B: 2)
func (h *HelloService) Test(ctx context.Context, auth *auth.AuthInfo, a []byte, b int64, c bool, ab *AB, d rune, e uint16) ([]byte, error) {
	fmt.Println("Test0:", auth)
	fmt.Println("Test1:", a)
	fmt.Println("Test2:", b)
	fmt.Println("Test3:", c)
	fmt.Println("Test4:", ab)
	fmt.Println("Test5 rune:", d)
	fmt.Println("Test6 uint16:", e)

	ch := ctx.Value(sloth.ChannelKey).(trpc.IChannel)
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	h.index++
	fmt.Println("Test1:", h.index)
	if h.index%2 == 0 {
		fmt.Println("--err-")
		return nil, fmt.Errorf("error %d", h.index)
	}
	_, err := ch.GetAuthInfo()
	if err != nil {
		return nil, err
	}
	fmt.Println("Test:", b)
	return utils.Serialize(map[string]string{"req": "local." + name + ".Test1", "time": time.Now().Format("2006-01-02 15:04:05")}), nil
}

// Hello struct
type Hello struct {
	Name string `json:"name"`
}
