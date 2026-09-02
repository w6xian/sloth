package sloth

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/w6xian/sloth/v3/nrpc/wsocket"
	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/types/auth"
)

// ---- 测试服务 ----

type echoService struct{}

func (s *echoService) Hello(ctx context.Context, name string) (string, error) {
	return "hello:" + name, nil
}

func (s *echoService) Add(ctx context.Context, a int, b int) (int, error) {
	return a + b, nil
}

// ---- 单元测试 ----

// TestRegisterDuplicate 重复注册同一服务名必须报错。
func TestRegisterDuplicate(t *testing.T) {
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	if err := svr.Register("v1", &echoService{}, "echo"); err != nil {
		t.Fatalf("first register err: %v", err)
	}
	if err := svr.Register("v1", &echoService{}, "echo"); err == nil {
		t.Fatal("duplicate register should return error")
	}
}

// TestDialUnsupportedNetwork 未知协议必须返回 error，不允许静默降级。
func TestDialUnsupportedNetwork(t *testing.T) {
	cli := ClientConn(DefaultClient())
	defer cli.Close()
	if err := cli.Dial(context.Background(), "tcp", "127.0.0.1:8080"); err == nil {
		t.Fatal("Dial with unsupported network should return error")
	}
	if err := cli.Dial(context.Background(), "quic", "127.0.0.1:8080"); err == nil {
		t.Fatal("Dial with unsupported network should return error")
	}
}

// TestServiceMapConcurrent Register 与 IsRegisteredService 并发读写（配合 -race 检测数据竞争）。
func TestServiceMapConcurrent(t *testing.T) {
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			svr.IsRegisteredService("v1.Hello")
		}()
		go func() {
			defer wg.Done()
			_ = svr.Register("v1", &echoService{}, "echo")
		}()
	}
	wg.Wait()
	if !svr.IsRegisteredService("v1.Hello") {
		t.Fatal("service v1 should be registered")
	}
}

// ---- 端到端集成测试 ----

// waitServerReady 反复探测 WS 端口直到 HTTP server 就绪。
func waitServerReady(t *testing.T, ctx context.Context, addr string) {
	t.Helper()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("server %s not ready: %v", addr, ctx.Err())
		default:
		}
		conn, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", nil)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitClientReady 轮询客户端底层通道直到连接建立（LocalClient.Client() 非 nil）。
func waitClientReady(t *testing.T, ctx context.Context, cli *Connect) {
	t.Helper()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("client not ready: %v", ctx.Err())
		default:
		}
		// ServerRpc.Listen 在 Dial goroutine 中写入，读取需持锁
		cli.server.mu.RLock()
		lc, ok := cli.server.Listen.(*wsocket.LocalClient)
		cli.server.mu.RUnlock()
		if ok && lc != nil && lc.Client() != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestWsEndToEndCall 核心回归测试：客户端通过真实 WS 连接调用服务端注册的方法。
// 此前 Serve() 的 handler 与 mux 路由脱节，此测试必然失败（404/连接拒绝）。
func TestWsEndToEndCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	svr := ServerConn(DefaultServer())
	defer svr.Close()
	if err := svr.Register("v1", &echoService{}, "echo service"); err != nil {
		t.Fatalf("register err: %v", err)
	}
	if err := svr.Listen(ctx, "ws", "127.0.0.1:0"); err != nil {
		t.Fatalf("listen err: %v", err)
	}
	addr := svr.listeners[0].Listener.Addr().String()
	t.Logf("server listen at %s", addr)

	serveDone := make(chan error, 1)
	go func() { serveDone <- svr.Serve() }()
	waitServerReady(t, ctx, addr)

	cli := ClientConn(DefaultClient())
	defer cli.Close()
	dialDone := make(chan error, 1)
	go func() { dialDone <- cli.Dial(ctx, "ws", addr) }()
	waitClientReady(t, ctx, cli)

	// 客户端 → 服务端调用
	resp, err := cli.server.Call(ctx, "v1.Hello", "world")
	if err != nil {
		t.Fatalf("call v1.Hello err: %v", err)
	}
	if string(resp) != "hello:world" {
		t.Fatalf("unexpected resp: %q", resp)
	}
	t.Logf("call v1.Hello -> %s", resp)

	// 多参数 int 调用
	resp, err = cli.server.Call(ctx, "v1.Add", 3, 4)
	if err != nil {
		t.Fatalf("call v1.Add err: %v", err)
	}
	if string(resp) != "7" {
		t.Fatalf("unexpected resp: %q", resp)
	}
	t.Logf("call v1.Add -> %s", resp)

	// 登录后再次调用（覆盖 SetAuthInfo 路径）
	if err := cli.SetAuthInfo(&auth.AuthInfo{UserId: 1, RoomId: 1, Token: "test"}); err != nil {
		t.Fatalf("set auth err: %v", err)
	}
	resp, err = cli.server.Call(ctx, "v1.Hello", "authed")
	if err != nil {
		t.Fatalf("call after auth err: %v", err)
	}
	if string(resp) != "hello:authed" {
		t.Fatalf("unexpected resp: %q", resp)
	}

	// 清理：取消 ctx 断开客户端连接，关闭服务端
	cancel()
	select {
	case err := <-dialDone:
		if err != nil && err != context.Canceled {
			t.Logf("dial returned: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Log("dial goroutine did not return")
	}
	svr.Close()
	select {
	case err := <-serveDone:
		t.Logf("serve returned: %v", err)
	case <-time.After(3 * time.Second):
		t.Log("serve goroutine did not return")
	}
}

// TestWsMaxConnsLimit MaxConnsWS/MaxConnsGlobal 连接限额：超限连接被拒绝。
func TestWsMaxConnsLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	svr := ServerConn(DefaultServer(),
		WithMaxConnsWS(1),
		WithMaxConnsGlobal(1),
	)
	defer svr.Close()
	if err := svr.Listen(ctx, "ws", "127.0.0.1:0"); err != nil {
		t.Fatalf("listen err: %v", err)
	}
	addr := svr.listeners[0].Listener.Addr().String()
	go svr.Serve()
	waitServerReady(t, ctx, addr)

	// 第一个连接成功
	c1, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", nil)
	if err != nil {
		t.Fatalf("first conn err: %v", err)
	}
	defer c1.Close()

	// 第二个连接：Upgrade 成功但立即被服务端关闭（读立即出错）
	c2, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", nil)
	if err == nil {
		_, _, rerr := c2.ReadMessage()
		if rerr == nil {
			t.Fatal("second conn should be rejected by MaxConns limit")
		}
		c2.Close()
	}

	// 释放第一个连接后，新连接应能成功
	c1.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c3, _, derr := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", nil)
		if derr == nil {
			c3.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("conn after release should succeed, last err: %v", derr)
		}
		time.Sleep(50 * time.Millisecond)
	}
	svr.Close()
}

// TestWsPerIPLimit MaxConnsPerIP 限额：同一 IP 超限被拒。
func TestWsPerIPLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	svr := ServerConn(DefaultServer(), WithMaxConnsPerIP(2))
	defer svr.Close()
	if err := svr.Listen(ctx, "ws", "127.0.0.1:0"); err != nil {
		t.Fatalf("listen err: %v", err)
	}
	addr := svr.listeners[0].Listener.Addr().String()
	go svr.Serve()
	waitServerReady(t, ctx, addr)

	c1, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", nil)
	if err != nil {
		t.Fatalf("conn1 err: %v", err)
	}
	defer c1.Close()
	c2, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", nil)
	if err != nil {
		t.Fatalf("conn2 err: %v", err)
	}
	defer c2.Close()

	// 第 3 个同 IP 连接应被拒绝
	c3, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", nil)
	if err == nil {
		_, _, rerr := c3.ReadMessage()
		if rerr == nil {
			t.Fatal("3rd conn from same IP should be rejected")
		}
		c3.Close()
	}
	svr.Close()
}

// TestWsOriginDefaultAllow 未配置 WithOrigin 时默认放行（不带 Origin 与带任意 Origin 均能连上）。
func TestWsOriginDefaultAllow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	svr := ServerConn(DefaultServer())
	defer svr.Close()
	if err := svr.Listen(ctx, "ws", "127.0.0.1:0"); err != nil {
		t.Fatalf("listen err: %v", err)
	}
	addr := svr.listeners[0].Listener.Addr().String()
	go svr.Serve()
	waitServerReady(t, ctx, addr)

	// 不带 Origin（原生客户端）
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", nil)
	if err != nil {
		t.Fatalf("dial without Origin err: %v", err)
	}
	conn.Close()

	// 带任意 Origin
	hdr := http.Header{}
	hdr.Set("Origin", "http://evil.example.com")
	conn, _, err = websocket.DefaultDialer.Dial("ws://"+addr+"/ws", hdr)
	if err != nil {
		t.Fatalf("dial with Origin err: %v", err)
	}
	conn.Close()
	svr.Close()
}

// TestWsOriginConfiguredReject 配置 WithOrigin 后，非白名单 Origin 被拒绝。
func TestWsOriginConfiguredReject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	svr := ServerConn(DefaultServer())
	defer svr.Close()
	// WithOrigin 属于 option.ConnectOption，通过 Listen 传入（Serve 时注入 WsServer）
	if err := svr.Listen(ctx, "ws", "127.0.0.1:0", option.WithOrigin("allowed.example.com")); err != nil {
		t.Fatalf("listen err: %v", err)
	}
	addr := svr.listeners[0].Listener.Addr().String()
	go svr.Serve()
	waitServerReady(t, ctx, addr)

	// 白名单 Origin 成功
	hdr := http.Header{}
	hdr.Set("Origin", "http://allowed.example.com")
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", hdr)
	if err != nil {
		t.Fatalf("dial with allowed Origin err: %v", err)
	}
	conn.Close()

	// 非白名单 Origin 被拒
	hdr.Set("Origin", "http://evil.example.com")
	if conn, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", hdr); err == nil {
		conn.Close()
		t.Fatal("dial with disallowed Origin should fail")
	}
	svr.Close()
}
