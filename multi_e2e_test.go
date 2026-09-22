package sloth

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/logger"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/sloth/v4/types/auth"
	"github.com/w6xian/sloth/v4/types/trpc"
)

// 本文件验证"多协议同时监听"：同一个 Connect 上挂 ws / tcp / quic 三条链路，
// 一次 Serve() 全部拉起，业务侧无感。
//
// 这里验的是两个此前真实存在的坑（都不是"多起几个 goroutine"就能绕过的）：
//
//  1. 监听器被误加 TLS：makeListenerFor 只看 Connect.tlsConfig 是否为 nil，
//     而 QUIC 强制要求 tlsConfig——于是"给 QUIC 配 TLS"会把 ws / tcp 端口
//     一起变成 TLS 监听器，明文客户端全部握手失败（且不报任何错）。
//
//  2. 服务端推送只能覆盖最后一条协议：ClientRpc 只有一个 Serve 字段，
//     每次 Listen 都直接覆盖它。先注册的协议照样收包、照样能调用服务端，
//     却永远收不到 Call / CallRoom / CallBucket / Broadcast 推下来的消息。

// multiEnv 三条协议都跑起来的环境（服务端 + 三条客户端连接）。
type multiEnv struct {
	ctx     context.Context
	push    *ClientRpc // 服务端主动推送入口（DefaultServer() 返回的那个）
	conns   map[string]*Connect
	sinks   map[string]*pushSink
	address map[string]string
}

// startMultiEnv 起一个同时监听 ws / tcp / quic 的服务端，并为三条协议各连一个客户端。
func startMultiEnv(tb testing.TB) *multiEnv {
	tb.Helper()
	// 连接建立/关闭会产生 ERROR 日志，不静音的话会混进测试输出
	logger.SetOutput(io.Discard)
	tb.Cleanup(func() { logger.SetOutput(os.Stderr) })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	tb.Cleanup(cancel)

	push := DefaultServer()
	svr := ServerConn(push, WithTLSConfig(selfSignedTLSForTest(tb)))
	if err := svr.Register("v1", &multiSignService{}, "sign service"); err != nil {
		tb.Fatalf("register err: %v", err)
	}
	// 三条协议共用同一份注册表：这里再挂一个 echo 服务，用来验证
	// "从哪条协议连上来都能调到 v1/echo 的所有方法"。
	if err := svr.Register("echo", &echoService{}, "echo service"); err != nil {
		tb.Fatalf("register err: %v", err)
	}
	// 端口 0：让内核分配，避免并行测试抢端口
	for _, network := range []string{WS, TCP, QUIC} {
		if err := svr.Listen(ctx, network, "127.0.0.1:0"); err != nil {
			tb.Fatalf("listen %s err: %v", network, err)
		}
	}
	env := &multiEnv{
		ctx:     ctx,
		push:    push,
		conns:   make(map[string]*Connect, 3),
		sinks:   make(map[string]*pushSink, 3),
		address: make(map[string]string, 3),
	}
	for _, l := range svr.listeners {
		env.address[l.Network] = l.Listener.Addr().String()
	}
	go svr.Serve()
	tb.Cleanup(func() { svr.Close() })

	// ws / tcp 的 listener 要等 Serve 起来才 Accept；QUIC 是 UDP，Listen 完就能收包
	waitServerReady(tb, ctx, env.address[WS])
	waitTcpServerReady(tb, ctx, env.address[TCP])

	for _, network := range []string{WS, TCP, QUIC} {
		name := network
		opts := []ConnOption{}
		if name == QUIC {
			// QUIC 必填 TLS；自签证书只能跳过校验
			opts = append(opts, WithTLSConfig(&tls.Config{InsecureSkipVerify: true}))
		}
		sink := &pushSink{}
		cli := ClientConn(DefaultClient(), opts...)
		tb.Cleanup(func() { cli.Close() })
		if err := cli.Register("shop", sink, ""); err != nil {
			tb.Fatalf("register client service on %s: %v", name, err)
		}
		// ws 的 Dial 内部跑到连接断开，必须放 goroutine；tcp / quic 立即返回
		go func() { _ = cli.Dial(ctx, name, env.address[name]) }()
		waitClientReady(tb, ctx, cli)
		env.conns[name] = cli
		env.sinks[name] = sink
	}
	return env
}

// signAll 让三条链路各自 Sign 进同一个房间，服务端才能按 roomId 推给它们。
//
// 入参故意三条都传同一个值（"sign"，三个 example 客户端就是这么调的）：
// 协议标识只能靠库自动写入的 sloth-protocol 头来区分，入参里没有可用信息。
func (e *multiEnv) signAll(tb testing.TB, roomId int64) {
	tb.Helper()
	for name, cli := range e.conns {
		if _, err := cli.server.Call(e.ctx, "v1.Sign", []byte("sign")); err != nil {
			tb.Fatalf("sign on %s err: %v", name, err)
		}
	}
	_ = roomId
}

// multiSignService 按协议名分配 userId，并把连接登记进房间 1。
type multiSignService struct{}

var multiUserIds = map[string]int64{"ws": 1, "tcp": 2, "quic": 3}

// Sign 优先用库写入的协议头（sloth-protocol）区分链路，只有拿不到时才退回入参。
//
// 这个顺序是必须的：所有客户端传给 Sign 的都是同一个值，只看入参的话三条
// 链路会被登记成同一个 userId——服务端推送时看似只推给了"一个"在线连接。
func (s *multiSignService) Sign(ctx context.Context, data []byte) ([]byte, error) {
	ch, ok := ctx.Value(ChannelKey).(bucket.IChannel)
	if !ok {
		return nil, io.EOF
	}
	svr, ok := ctx.Value(BucketKey).(types.IBucket)
	if !ok {
		return nil, io.EOF
	}
	name, err := GetProtocol(ctx)
	if err != nil || name == "" {
		name = strings.TrimSpace(string(data))
	}
	userId, ok := multiUserIds[name]
	if !ok {
		userId = 99
	}
	if err := svr.Bucket(userId).Put(userId, 1, "token", ch); err != nil {
		return nil, err
	}
	return []byte(strconv.FormatInt(userId, 10)), nil
}

// pushSink 客户端侧方法：被服务端 CallRoom 时计数，并顺带记录能不能读到身份。
//
// 记身份是因为这里踩过一次：同一个客户端方法里调 ch.GetAuthInfo()，
// ws 能拿到、tcp / quic 拿不到（stream 连接的身份只有服务端语义那一套，
// 客户端侧恒为 0）。换传输就换行为，正是本库要避免的事。
type pushSink struct {
	got     atomic.Int64
	authOK  atomic.Int64
	authErr atomic.Value // string
}

func (s *pushSink) Test(ctx context.Context, b []byte) ([]byte, error) {
	s.got.Add(1)
	ch, ok := ctx.Value(ChannelKey).(trpc.IChannel)
	if !ok || ch == nil {
		s.authErr.Store("channel missing in ctx")
		return []byte("ok"), nil
	}
	if _, err := ch.GetAuthInfo(); err != nil {
		s.authErr.Store(err.Error())
		return []byte("ok"), nil
	}
	s.authOK.Add(1)
	return []byte("ok"), nil
}

// TestMultiTransportRpc 三条协议同时在线，且每条都能调到同一份服务注册表。
func TestMultiTransportRpc(t *testing.T) {
	env := startMultiEnv(t)
	for name, cli := range env.conns {
		resp, err := cli.server.Call(env.ctx, "echo.Hello", name)
		if err != nil {
			t.Fatalf("[%s] call echo.Hello err: %v", name, err)
		}
		if want := "hello:" + name; string(resp) != want {
			t.Fatalf("[%s] resp = %q, want %q", name, resp, want)
		}
	}
}

// TestMultiTransportWsStaysPlaintextWithTLS 配了 TLS（QUIC 需要）之后，
// ws / tcp 端口必须仍是明文——否则明文客户端握手失败，且没有任何报错。
func TestMultiTransportWsStaysPlaintextWithTLS(t *testing.T) {
	env := startMultiEnv(t)

	conn, _, err := websocket.DefaultDialer.Dial("ws://"+env.address[WS]+"/ws", nil)
	if err != nil {
		t.Fatalf("plain ws dial should still work when TLS is configured: %v", err)
	}
	conn.Close()

	tcpConn, err := net.DialTimeout("tcp", env.address[TCP], 2*time.Second)
	if err != nil {
		t.Fatalf("plain tcp dial err: %v", err)
	}
	tcpConn.Close()
}

// TestMultiTransportSignDistinguishesProtocols 三条链路传**完全相同**的入参签名，
// 服务端必须把它们登记成三个不同的 userId。
//
// 这是真实踩过的坑：三个 example 客户端都调 Call("v1.Sign", []byte("sign"))，
// 服务端只能从入参取协议名，于是三条链路全部落到同一个兜底 userId（99）——
// 日志里再也分不出消息推给了哪条链路，看起来像只有一条链路在线。
// 修复方式是库在固定头里自动带上协议名（sloth.HeaderProtocol），
// 服务端用 GetProtocol(ctx) 读，不依赖业务入参。
func TestMultiTransportSignDistinguishesProtocols(t *testing.T) {
	env := startMultiEnv(t)

	ids := make(map[string]int64, len(env.conns))
	for name, cli := range env.conns {
		resp, err := cli.server.Call(env.ctx, "v1.Sign", []byte("sign"))
		if err != nil {
			t.Fatalf("[%s] sign err: %v", name, err)
		}
		id, err := strconv.ParseInt(string(resp), 10, 64)
		if err != nil {
			t.Fatalf("[%s] bad sign resp %q: %v", name, resp, err)
		}
		ids[name] = id
		if id == 99 {
			t.Errorf("[%s] userId 落到了兜底值 99：服务端没读到协议标识", name)
		}
	}
	if ids[WS] == ids[TCP] || ids[WS] == ids[QUIC] || ids[TCP] == ids[QUIC] {
		t.Fatalf("三条链路拿到了相同的 userId %v：协议没被区分开", ids)
	}
	for name, want := range multiUserIds {
		if ids[name] != want {
			t.Errorf("[%s] userId = %d, want %d", name, ids[name], want)
		}
	}
}

// TestMultiTransportPushReachesAllProtocols 服务端一次 CallRoom 必须打到三条链路。
//
// 这是"多协议"最容易悄悄出错的地方：改之前只有最后 Listen 的那条协议能收到推送，
// 前面几条收不到却不报错（看起来像是客户端没注册对）。
func TestMultiTransportPushReachesAllProtocols(t *testing.T) {
	env := startMultiEnv(t)
	env.signAll(t, 1)

	if _, err := env.push.CallRoom(env.ctx, 1, "shop.Test", "hello"); err != nil {
		t.Fatalf("CallRoom err: %v", err)
	}
	// 投递是异步的（各传输的 bucket worker 队列），给一点时间收敛
	deadline := time.Now().Add(5 * time.Second)
	for name, sink := range env.sinks {
		for sink.got.Load() == 0 && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if got := sink.got.Load(); got == 0 {
			t.Errorf("[%s] 没收到服务端推送（推送只覆盖了部分协议？）", name)
		}
	}
}

// TestMultiTransportClientMethodHasAuth 客户端方法里读自己的身份，三条传输必须一致。
//
// 服务端反调客户端方法时，业务代码从 ctx 上取到的 channel 就是那条连接。
// ws 的连接自带身份，tcp / quic 走的是 stream 连接——它的身份原本只有服务端
// 语义那一套（bucket.Put 写进去），客户端侧恒为 0，于是这条断言在
// tcp / quic 上必然失败：同一个业务方法换传输就换行为。
func TestMultiTransportClientMethodHasAuth(t *testing.T) {
	env := startMultiEnv(t)
	env.signAll(t, 1)

	// 客户端保存身份：Sign 之后由业务自己 SetAuthInfo（样例里也是这个顺序）
	for name, cli := range env.conns {
		if err := cli.server.SetAuthInfo(&auth.AuthInfo{UserId: 1, RoomId: 1, Token: "tk"}); err != nil {
			t.Fatalf("[%s] SetAuthInfo err: %v", name, err)
		}
	}
	if _, err := env.push.CallRoom(env.ctx, 1, "shop.Test", "hello"); err != nil {
		t.Fatalf("CallRoom err: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for name, sink := range env.sinks {
		for sink.got.Load() == 0 && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if sink.authOK.Load() == 0 {
			if v := sink.authErr.Load(); v != nil {
				t.Errorf("[%s] 客户端方法里读不到自己的身份: %v", name, v)
			} else {
				t.Errorf("[%s] 客户端方法没被调用", name)
			}
		}
	}
}

// TestMultiServeInstanceIsComposite 多协议时对外暴露的是合成实例，
// 单协议时必须仍是具体传输实例（保持原有类型，调用方类型断言不受影响）。
func TestMultiServeInstanceIsComposite(t *testing.T) {
	logger.SetOutput(io.Discard)
	defer logger.SetOutput(os.Stderr)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	one := ServerConn(DefaultServer())
	if err := one.Listen(ctx, WS, "127.0.0.1:0"); err != nil {
		t.Fatalf("listen ws err: %v", err)
	}
	defer one.Close()
	if _, ok := one.client.getServe().(*multiServer); ok {
		t.Fatal("单协议时不应使用合成实例")
	}

	many := ServerConn(DefaultServer(), WithTLSConfig(selfSignedTLSForTest(t)))
	defer many.Close()
	for _, network := range []string{WS, TCP, QUIC} {
		if err := many.Listen(ctx, network, "127.0.0.1:0"); err != nil {
			t.Fatalf("listen %s err: %v", network, err)
		}
	}
	ms, ok := many.client.getServe().(*multiServer)
	if !ok {
		t.Fatalf("多协议时 getServe() = %T, want *multiServer（推送会只覆盖一条协议）", many.client.getServe())
	}
	if len(ms.list()) != 3 {
		t.Fatalf("合成实例持有 %d 个传输实例, want 3", len(ms.list()))
	}
}
