package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/w6xian/sloth/v3/internal/utils"
	"github.com/w6xian/sloth/v3/nrpc/wsocket"

	"github.com/gorilla/websocket"
)

func main() {

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// 直接输入index.html，返回index.html
		http.ServeFile(w, r, "./index_v3.html")
	})
	http.HandleFunc("/min", func(w http.ResponseWriter, r *http.Request) {
		// 直接输入index.html，返回index.html
		http.ServeFile(w, r, "./index_v3_min.html")
	})
	http.HandleFunc("/sock_rpc_v3.js", func(w http.ResponseWriter, r *http.Request) {
		// 直接输入index.html，返回index.html
		http.ServeFile(w, r, "./sock_rpc_v3.js")
	})
	http.HandleFunc("/slice.js", func(w http.ResponseWriter, r *http.Request) {
		// 直接输入index.html，返回index.html
		http.ServeFile(w, r, "./slice.js")
	})
	http.HandleFunc("/ag.js", func(w http.ResponseWriter, r *http.Request) {
		// 直接输入index.html，返回index.html
		http.ServeFile(w, r, "./ag.js")
	})
	http.HandleFunc("/fn.js", func(w http.ResponseWriter, r *http.Request) {
		// 直接输入index.html，返回index.html
		http.ServeFile(w, r, "./fn.js")
	})
	http.HandleFunc("/tools.js", func(w http.ResponseWriter, r *http.Request) {
		// 直接输入index.html，返回index.html
		http.ServeFile(w, r, "./tools.js")
	})
	http.HandleFunc("/sloth_v3_min.js", func(w http.ResponseWriter, r *http.Request) {
		// 直接输入index.html，返回index.html
		http.ServeFile(w, r, "./sloth_v3_min.js")
	})
	http.HandleFunc("/sloth_v3_bundle.js", func(w http.ResponseWriter, r *http.Request) {
		// 直接输入index.html，返回index.html
		http.ServeFile(w, r, "./sloth_v3_bundle.js")
	})

	fmt.Println("Server is running on http://localhost:8081")
	http.ListenAndServe(":8081", nil)
}

type Handler struct {
}

func (h *Handler) HandleMessage(s *wsocket.LocalClient, ch *wsocket.WsChannelClient, msgType int, message []byte) error {
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
