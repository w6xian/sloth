package sloth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/nrpc/wsocket"
	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/types/auth"
	"github.com/w6xian/sloth/v3/types/trpc"
)

// ---- 测试服务 ----

type echoService struct{}

func (s *echoService) Hello(ctx context.Context, name string) (string, error) {
	return "hello:" + name, nil
}

func (s *echoService) Add(ctx context.Context, a int, b int) (int, error) {
	return a + b, nil
}

func (s *echoService) Echo(ctx context.Context, data string) (string, error) {
	return data, nil
}

// headerEchoService 读取 CallFunc 注入 ctx 的 Header（meta=注册描述、remote_addr=请求来源）。
type headerEchoService struct{}

func (s *headerEchoService) Echo(ctx context.Context, v string) (string, error) {
	h, err := GetHeader(ctx)
	if err != nil {
		return "", err
	}
	return h.Get("meta") + "|" + h.Get("remote_addr") + "|" + v, nil
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

// ---------- 补充：CallFunc / CallNetFunc 单元测试（不依赖真实网络） ----------

// TestCallFuncInjectHeader 验证 CallFunc 将注册描述与请求来源注入 ctx Header，
// 且不污染调用方传入的 Header map。
func TestCallFuncInjectHeader(t *testing.T) {
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	if err := svr.Register("v1", &headerEchoService{}, "desc"); err != nil {
		t.Fatalf("register err: %v", err)
	}
	req := &trpc.RpcCaller{
		Method: "v1.Echo",
		Header: message.Header{"x-uid": "9"},
		Args:   [][]byte{[]byte("in")},
	}
	r := &http.Request{RemoteAddr: "10.0.0.1:7777"}
	rst, err := svr.CallFunc(context.Background(), r, nil, nil, req)
	if err != nil {
		t.Fatalf("CallFunc err: %v", err)
	}
	if string(rst) != "desc|10.0.0.1:7777|in" {
		t.Fatalf("unexpected result: %q", rst)
	}
	// 调用方 header 未被写入 meta / remote_addr
	if _, ok := req.Header["meta"]; ok {
		t.Fatal("meta should not pollute caller header")
	}
	if req.Header["x-uid"] != "9" {
		t.Fatalf("caller header lost: %v", req.Header)
	}
}

// TestCallFuncMethodFormatError 方法名必须为 service.method 两段式。
func TestCallFuncMethodFormatError(t *testing.T) {
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	if _, err := svr.CallFunc(context.Background(), nil, nil, nil, &trpc.RpcCaller{Method: "badmethod"}); err == nil {
		t.Fatal("method without dot separator should error")
	}
	if _, err := svr.CallFunc(context.Background(), nil, nil, nil, &trpc.RpcCaller{Method: "a.b.c"}); err == nil {
		t.Fatal("method with extra dots should error")
	}
}

// TestCallFuncServiceNotFound 调用未注册服务/方法返回错误而非 panic。
func TestCallFuncServiceNotFound(t *testing.T) {
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	if _, err := svr.CallFunc(context.Background(), nil, nil, nil, &trpc.RpcCaller{Method: "v1.Missing"}); err == nil {
		t.Fatal("calling unregistered service should error")
	}
	if err := svr.Register("v1", &echoService{}, "e"); err != nil {
		t.Fatalf("register err: %v", err)
	}
	if _, err := svr.CallFunc(context.Background(), nil, nil, nil, &trpc.RpcCaller{Method: "v1.NotAMethod"}); err == nil {
		t.Fatal("calling unregistered method should error")
	}
}

// TestCallNetFuncProxyFlow CallNetFunc 需经 UseProxyHandler 解析目标 userId。
func TestCallNetFuncProxyFlow(t *testing.T) {
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	ctx := context.Background()

	called := false
	if err := svr.UseProxyHandler(func(ctx context.Context, service string) (int64, error) {
		called = true
		if service != "v1.Hello" {
			t.Fatalf("unexpected service: %s", service)
		}
		return 0, nil
	}); err != nil {
		t.Fatalf("UseProxyHandler err: %v", err)
	}
	if _, err := svr.CallNetFunc(ctx, nil, "v1.Hello", 1, []byte("x")); err == nil {
		t.Fatal("CallNetFunc without established client conn should error")
	}
	if !called {
		t.Fatal("proxy handler should have been invoked")
	}
}

// TestServeWithoutListen 未 Listen 直接 Serve 必须报错。
func TestServeWithoutListen(t *testing.T) {
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	if err := svr.Serve(); err == nil {
		t.Fatal("Serve without Listen should error")
	}
}

// TestListenUnsupportedNetwork 未注册协议的网络类型应被拒绝。
func TestListenUnsupportedNetwork(t *testing.T) {
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	if err := svr.Listen(context.Background(), "kcp", "127.0.0.1:0"); err == nil {
		t.Fatal("Listen with unsupported network should error")
	}
}

// TestCloseIdempotent Close 多次调用不 panic 不报错。
func TestCloseIdempotent(t *testing.T) {
	svr := ServerConn(DefaultServer())
	if err := svr.Close(); err != nil {
		t.Fatalf("first Close err: %v", err)
	}
	if err := svr.Close(); err != nil {
		t.Fatalf("second Close err: %v", err)
	}
	cli := ClientConn(DefaultClient())
	if err := cli.Close(); err != nil {
		t.Fatalf("client Close err: %v", err)
	}
	if err := cli.Close(); err != nil {
		t.Fatalf("client second Close err: %v", err)
	}
}

// TestIsRegisteredServiceFormat 格式非法的服务名一律视为未注册。
func TestIsRegisteredServiceFormat(t *testing.T) {
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	for _, m := range []string{"", "Hello", "v1", "a.b.c", "  v1.Hello"} {
		if svr.IsRegisteredService(m) {
			t.Fatalf("IsRegisteredService(%q) should be false", m)
		}
	}
	if err := svr.Register("v1", &echoService{}, "e"); err != nil {
		t.Fatalf("register err: %v", err)
	}
	if !svr.IsRegisteredService("v1.Hello") {
		t.Fatal("IsRegisteredService(v1.Hello) should be true after register")
	}
}

// ---------- 补充：端到端集成测试 ----------

// TestWsEndToEndLargePayload 大 payload 往返（触发分片发送/重组路径）。
func TestWsEndToEndLargePayload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
	serveDone := make(chan error, 1)
	go func() { serveDone <- svr.Serve() }()
	waitServerReady(t, ctx, addr)

	cli := ClientConn(DefaultClient())
	defer cli.Close()
	dialDone := make(chan error, 1)
	go func() { dialDone <- cli.Dial(ctx, "ws", addr) }()
	waitClientReady(t, ctx, cli)

	// 中文小消息：单帧内 utf8 完整往返
	if resp, err := cli.server.Call(ctx, "v1.Echo", "你好，世界"); err != nil {
		t.Fatalf("call utf8 err: %v", err)
	} else if string(resp) != "你好，世界" {
		t.Fatalf("utf8 roundtrip mismatch: %q", resp)
	}

	// 48KB ASCII 大 payload（ag 参数编码上限 64KB）：远超分片阈值，触发多分片传输/重组
	big := strings.Repeat("0123456789abcdef", 3*1024)
	resp, err := cli.server.Call(ctx, "v1.Echo", big)
	if err != nil {
		t.Fatalf("call v1.Echo err: %v", err)
	}
	if string(resp) != big {
		t.Fatalf("large payload mismatch: got %d bytes want %d bytes", len(resp), len(big))
	}
	t.Logf("large payload roundtrip ok: %d bytes", len(big))

	cancel()
	select {
	case err := <-dialDone:
		if err != nil && err != context.Canceled {
			t.Logf("dial returned: %v", err)
		}
	case <-time.After(3 * time.Second):
	}
	svr.Close()
	<-serveDone
}

// TestWsEndToEndConcurrentCalls 并发调用服务端方法（-race 下验证通道并发安全）。
func TestWsEndToEndConcurrentCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
	go svr.Serve()
	waitServerReady(t, ctx, addr)

	cli := ClientConn(DefaultClient())
	defer cli.Close()
	go cli.Dial(ctx, "ws", addr)
	waitClientReady(t, ctx, cli)

	var wg sync.WaitGroup
	errCh := make(chan error, 200)
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				resp, err := cli.server.Call(ctx, "v1.Add", i, 1)
				if err != nil {
					errCh <- err
					return
				}
				if string(resp) != fmt.Sprintf("%d", i+1) {
					errCh <- fmt.Errorf("unexpected result: %s", resp)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent call err: %v", err)
	}
	cancel()
	svr.Close()
}

// TestWsEndToEndUnknownMethod 服务端不存在的方法错误应跨连接传回客户端。
func TestWsEndToEndUnknownMethod(t *testing.T) {
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
	go svr.Serve()
	waitServerReady(t, ctx, addr)

	cli := ClientConn(DefaultClient())
	defer cli.Close()
	go cli.Dial(ctx, "ws", addr)
	waitClientReady(t, ctx, cli)

	// 已注册服务上的未知方法
	if _, err := cli.server.Call(ctx, "v1.NoSuchMethod"); err == nil {
		t.Fatal("call unknown method should error over the wire")
	}
	// 未注册服务
	if _, err := cli.server.Call(ctx, "v2.Hello"); err == nil {
		t.Fatal("call unregistered service should error over the wire")
	}
	cancel()
	svr.Close()
}

// TestWsEndToEndDialAfterClose 服务端关闭后客户端主动断开，Dial goroutine 正常返回。
func TestWsEndToEndDialAfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	svr := ServerConn(DefaultServer())
	if err := svr.Listen(ctx, "ws", "127.0.0.1:0"); err != nil {
		t.Fatalf("listen err: %v", err)
	}
	addr := svr.listeners[0].Listener.Addr().String()
	serveDone := make(chan error, 1)
	go func() { serveDone <- svr.Serve() }()
	waitServerReady(t, ctx, addr)

	cli := ClientConn(DefaultClient())
	defer cli.Close()
	dialDone := make(chan error, 1)
	go func() { dialDone <- cli.Dial(ctx, "ws", addr) }()
	waitClientReady(t, ctx, cli)

	// 服务端优雅关闭 → 客户端连接被断开
	svr.Close()
	select {
	case err := <-serveDone:
		if err != nil {
			t.Logf("serve returned: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return after Close")
	}

	// 取消客户端上下文使重连循环退出，Dial goroutine 应返回
	cancel()
	select {
	case err := <-dialDone:
		if err != nil && err != context.Canceled {
			t.Logf("dial returned: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Log("dial goroutine did not return after ctx cancel")
	}
}
