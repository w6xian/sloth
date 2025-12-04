package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/w6xian/sloth/internal/utils"
	"github.com/w6xian/sloth/nrpc/wsocket"

	"github.com/gorilla/websocket"
)

func main() {

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// 直接输入index.html，返回index.html
		http.ServeFile(w, r, "./index.html")
	})
	http.HandleFunc("/sock_rpc.js", func(w http.ResponseWriter, r *http.Request) {
		// 直接输入index.html，返回index.html
		http.ServeFile(w, r, "./sock_rpc.js")
	})
	fmt.Println("Server is running on http://localhost:8081")
	http.ListenAndServe(":8081", nil)
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
