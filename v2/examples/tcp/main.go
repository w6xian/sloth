package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/w6xian/sloth/v2"
	"github.com/w6xian/sloth/v2/bucket"
	"github.com/w6xian/sloth/v2/message"
	"github.com/w6xian/sloth/v2/nrpc"
	"github.com/w6xian/sloth/v2/nrpc/middleware"
	"github.com/w6xian/sloth/v2/nrpc/tcpsock"
)

// main 演示 TCP 服务端和客户端的使用
func main() {
	// 启动服务端
	go server()

	// 等待服务端启动
	time.Sleep(500 * time.Millisecond)

	// 运行客户端
	client()
}

// ── 服务端 ──────────────────────────────────────────────────────────────────

func server() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建 Connect 并注册服务
	connect := sloth.ServerConn(nil)
	connect.Register("v1", &HelloService{}, "")

	// 创建 TCP 服务端
	tcpServer := tcpsock.NewTcpServer(connect, connect.Options())
	tcpServer.Use(middleware.Recovery(nil, "tcp-server"))
	tcpServer.Use(middleware.ClientLog(nil, "tcp-server"))

	// 启动监听
	addr := "localhost:8991"
	if _, err := tcpServer.Listen(ctx, addr); err != nil {
		fmt.Println("Server listen error:", err)
		return
	}
	fmt.Println("TCP server listening on", addr)

	// 运行服务（阻塞）
	if err := tcpServer.Serve(ctx); err != nil {
		fmt.Println("Server error:", err)
	}
}

// ── 客户端 ──────────────────────────────────────────────────────────────────

func client() {
	ctx := context.Background()

	// 创建 TCP 客户端
	tcpClient := tcpsock.NewTcpClient(nil)

	// 连接服务端（内部启动 readLoop）
	_, err := tcpClient.Dial(ctx, "localhost:8991")
	if err != nil {
		fmt.Println("Dial error:", err)
		return
	}
	defer tcpClient.Close()

	// 设置认证信息
	tcpClient.SetAuthInfo(&nrpc.AuthInfo{
		UserId: 1,
		RoomId: 1,
		Token:  "test-token",
	})

	// 调用 Test 方法
	header := message.Header{
		"seq":  "1",
		"flag": "0",
	}

	// 构造参数
	args := [][]byte{[]byte(`{"a":1,"b":2}`)}
	resp, err := tcpClient.Call(ctx, header, "Test", args...)
	if err != nil {
		fmt.Println("Call error:", err)
		return
	}

	fmt.Println("Test response:", string(resp))

	// 调用 Login 方法
	loginResp, err := tcpClient.Call(ctx, header, "Login", []byte(`{}`))
	if err != nil {
		fmt.Println("Login error:", err)
		return
	}
	fmt.Println("Login response:", string(loginResp))

	// 调用 Sign 方法（需要认证）
	signResp, err := tcpClient.Call(ctx, header, "Sign", []byte(`"hello"`))
	if err != nil {
		fmt.Println("Sign error:", err)
		return
	}
	fmt.Println("Sign response:", string(signResp))

	fmt.Println("Client done!")
}

// ── 服务实现 ────────────────────────────────────────────────────────────────

type HelloService struct {
	Id int64
}

type AB struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

func (h *HelloService) Test(ctx context.Context, ab *AB) (any, error) {
	h.Id++

	// 获取上下文中的信息
	if ch, ok := ctx.Value(sloth.ChannelKey).(bucket.IChannel); ok {
		fmt.Printf("  Channel: %+v\n", ch)
	}
	if hdr, ok := ctx.Value(sloth.HeaderKey).(message.Header); ok {
		fmt.Printf("  Header: %+v\n", hdr)
	}
	fmt.Printf("  Args: %+v\n", ab)

	// 模拟错误
	if h.Id%5 == 1 {
		return nil, fmt.Errorf("error %d", h.Id)
	}

	return map[string]any{
		"req":  "tcp server",
		"time": time.Now().Format("2006-01-02 15:04:05"),
	}, nil
}

func (h *HelloService) Sign(ctx context.Context, data []byte) ([]byte, error) {
	h.Id++

	// 获取 channel 和 bucket
	ch, ok := ctx.Value(sloth.ChannelKey).(bucket.IChannel)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}

	svr, ok := ctx.Value(sloth.BucketKey).(nrpc.IBucket)
	if !ok {
		return nil, fmt.Errorf("bucket not found")
	}

	fmt.Println("  Sign data:", string(data))

	// 注册到 bucket
	auth := nrpc.AuthInfo{
		UserId: 2,
		RoomId: 1,
		Token:  "token_123",
	}
	svr.Bucket(auth.UserId).Put(auth.UserId, auth.RoomId, auth.Token, ch)

	return json.Marshal(auth)
}

func (h *HelloService) Login(ctx context.Context, data []byte) ([]byte, error) {
	return json.Marshal(map[string]string{
		"user_id": "2",
		"time":    time.Now().Format("2006-01-02 15:04:05"),
	})
}
