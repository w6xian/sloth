package main

import (
	"context"
	"encoding/json"
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
)

// AB is a test struct
type AB struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

var name = "shop1"

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
		if client.UserId == 0 {
			client.Header.Set("APP_ID", "1")
			client.Header.Set("USER_ID", "1")
			data, err := client.Call(context.Background(), "v1.Reg", name)
			fmt.Println("------------")
			fmt.Println("Reg result", string(data), err)
			fmt.Println("------------")
			if err != nil {
				continue
			}
			auth := &auth.AuthInfo{}
			err = json.Unmarshal(data, auth)
			if err != nil {
				continue
			}
			client.SetAuthInfo(auth)
			break
		}
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
func (h *HelloService) Test1(ctx context.Context, b []byte) ([]byte, error) {
	fmt.Println("Test1:", string(b))
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
	fmt.Println("Test:", string(b))
	return utils.Serialize(map[string]string{"req": "local." + name + ".Test1", "time": time.Now().Format("2006-01-02 15:04:05")}), nil
}

// Hello struct
type Hello struct {
	Name string `json:"name"`
}
