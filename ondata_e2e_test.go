package sloth

// 保留协议通道的端到端回归。
//
// sloth 设计之初就留了一手：不是本协议（FN 帧）的数据**不进 RPC**，原样交给
// handler.OnData，让用户自己的协议能搭在同一条连接上。transport 包里有单测
// 守着路由函数（FrameRouter 的分流），但"从真实传输到 handler"这一段没有
// 任何端到端保护——判定规则一旦收窄（比如把帧识别从完整校验改成只认 magic），
// 单测照样绿，用户的自定义协议却已经全掉进 RPC 或黑洞里了。这里补的就是这一层。
//
// 两个传输的结局不同，这是**设计现实**，不是 bug，测试把它如实固定下来：
//   - WS 有消息边界，一条 message 可以承载任意字节 → OnData 是活的；
//   - TCP / QUIC 是字节流，分帧只能靠 FN 帧头的 magic + length（见
//     stream.ReadFrame），不带 magic 的裸数据在分帧层就被判 bad magic 断连，
//     根本走不到 dispatch。它们要"保留协议"只能靠 option.WithCodec 注入。

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/codec"
	"github.com/w6xian/sloth/v4/decoder/fn"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/sloth/v4/types/handler"
)

// ondataPayload 一段刻意挑的"既不是 FN 帧、也不该被 TLV 误剥壳"的字节：
// 不以 @F 开头（FN magic），且按 TLV 布局解释时 length 远大于实际数据，
// tlv.Deserialize 必然失败 → 不会被剥壳，必须原样进 OnData。
var ondataPayload = []byte("hello custom protocol")

// ondataMsg 一次 OnData 回调收到的内容。
type ondataMsg struct {
	msgType int // 仅 WS 有（websocket message type）；TCP 恒为 0
	data    []byte
}

// ondataRecorder 两种传输共用的记录器。
type ondataRecorder struct{ got chan ondataMsg }

func newOndataRecorder() *ondataRecorder {
	return &ondataRecorder{got: make(chan ondataMsg, 8)}
}

// record 记一条；缓冲满了就丢，绝不能让业务回调阻塞住读循环。
func (r *ondataRecorder) record(msgType int, data []byte) {
	select {
	case r.got <- ondataMsg{msgType: msgType, data: append([]byte(nil), data...)}:
	default:
	}
}

// ── WS 钩子（HTTP 版，带 *http.Request 与 message type） ──

type wsOndataHandler struct{ rec *ondataRecorder }

// 编译期约束：钩子接口签名一改，这里立刻编译失败——比运行到一半才发现
// "回调没挂上"更早暴露问题。
var _ handler.IServerHandleMessage = wsOndataHandler{}

func (wsOndataHandler) OnConnect(ctx context.Context, r *http.Request) error { return nil }
func (wsOndataHandler) OnReady(ctx context.Context, r *http.Request, s types.IBucket, ch bucket.IChannel) error {
	return nil
}
func (wsOndataHandler) OnClose(ctx context.Context, r *http.Request, s types.IBucket, ch bucket.IChannel) error {
	return nil
}
func (wsOndataHandler) OnError(ctx context.Context, r *http.Request, s types.IBucket, ch bucket.IChannel, err error) error {
	return nil
}
func (h wsOndataHandler) OnData(ctx context.Context, r *http.Request, s types.IBucket, ch bucket.IChannel,
	msgType int, message []byte) error {
	h.rec.record(msgType, message)
	return nil
}

// ── TCP 钩子（非 HTTP 版，没有 request 与 message type） ──

type tcpOndataHandler struct{ rec *ondataRecorder }

var _ handler.TcpHandleMessage = tcpOndataHandler{}

func (tcpOndataHandler) OnConnect(ctx context.Context, addr string) error { return nil }
func (tcpOndataHandler) OnReady(ctx context.Context, s types.IBucket, ch bucket.IChannel) error {
	return nil
}
func (tcpOndataHandler) OnClose(ctx context.Context, s types.IBucket, ch bucket.IChannel) error {
	return nil
}
func (tcpOndataHandler) OnError(ctx context.Context, s types.IBucket, ch bucket.IChannel, err error) error {
	return nil
}
func (h tcpOndataHandler) OnData(ctx context.Context, s types.IBucket, ch bucket.IChannel, msg []byte) error {
	h.rec.record(0, msg)
	return nil
}

// ondataStubCodec 只认 'X' 开头的帧：用它模拟"另一个协议"被注入的场景。
type ondataStubCodec struct{}

var _ codec.Codec = ondataStubCodec{}

func (ondataStubCodec) Detect(raw []byte) bool { return len(raw) > 0 && raw[0] == 'X' }
func (ondataStubCodec) Decode(raw []byte) (uint8, uint64, []byte, error) {
	return 1, 1, raw, nil
}
func (ondataStubCodec) Encode(action uint8, id uint64, data []byte) ([]byte, error) {
	return data, nil
}

// dialRawTCP 连上 TCP 传输（带重试：Serve 起来之前连接会在 backlog 里排队）。
func dialRawTCP(t *testing.T, addr string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			return conn
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial tcp %s: %v", addr, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestWsOnDataReceivesRawProtocol 非协议数据必须原样进 OnData，且带对 message type。
//
// 这是"保留协议"的主回归：一旦有人把帧识别改宽（例如任何字节都当 FN 帧试解），
// 或者剥壳逻辑把原始字节换掉，这里立刻红。
func TestWsOnDataReceivesRawProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rec := newOndataRecorder()
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	if err := svr.Listen(ctx, "ws", "127.0.0.1:0", option.WithServerHandleMessage(wsOndataHandler{rec})); err != nil {
		t.Fatalf("listen err: %v", err)
	}
	addr := svr.listeners[0].Listener.Addr().String()
	go svr.Serve()
	waitServerReady(t, ctx, addr)

	conn, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", nil)
	if err != nil {
		t.Fatalf("dial err: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, ondataPayload); err != nil {
		t.Fatalf("write err: %v", err)
	}

	select {
	case got := <-rec.got:
		if !bytes.Equal(got.data, ondataPayload) {
			t.Fatalf("OnData 收到的不是原样字节: got %q, want %q", got.data, ondataPayload)
		}
		if got.msgType != websocket.TextMessage {
			t.Fatalf("OnData msgType = %d, want %d (TextMessage)", got.msgType, websocket.TextMessage)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OnData 没被调用：非协议数据没走到 handler（保留协议通道断了）")
	}
	svr.Close()
}

// TestTcpOnDataWithInjectedCodec TCP 上唯一能到达 OnData 的路径：帧是合法 FN 帧
// （分帧层要求 magic），但注入的 codec 不认领它 → 交给业务。
func TestTcpOnDataWithInjectedCodec(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rec := newOndataRecorder()
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	if err := svr.Listen(ctx, "tcp", "127.0.0.1:0",
		option.WithTcpHandleMessage(tcpOndataHandler{rec}),
		option.WithCodec(ondataStubCodec{})); err != nil {
		t.Fatalf("listen err: %v", err)
	}
	addr := svr.listeners[0].Listener.Addr().String()
	go svr.Serve()

	conn := dialRawTCP(t, addr)
	defer conn.Close()

	frame, err := fn.Encode(1, 7, []byte("body"))
	if err != nil {
		t.Fatalf("encode fn frame: %v", err)
	}
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write err: %v", err)
	}

	select {
	case got := <-rec.got:
		if !bytes.Equal(got.data, frame) {
			t.Fatalf("OnData 收到的不是原样帧: got %q, want %q", got.data, frame)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OnData 没被调用：注入的 codec 不认领时，帧应交给 handler 而不是丢弃")
	}
	svr.Close()
}

// TestTcpRawBytesNeverReachOnData 固化一个容易被误会的事实：字节流传输
// **没有**"裸数据走 OnData"这条通道。
//
// 没有消息边界，分帧只能认 FN magic；一段不带 magic 的数据在 stream.ReadFrame
// 就被判 bad magic，连接随即断开——dispatch 与 OnData 都没机会执行。
// 想在这类传输上带自己的协议，只有 option.WithCodec 一条路（见上一个测试）。
// 把这个行为写死，是为了避免有人"照着 WS 的用法"在 TCP 上发裸数据，
// 然后困惑为什么只看到断连。
func TestTcpRawBytesNeverReachOnData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rec := newOndataRecorder()
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	if err := svr.Listen(ctx, "tcp", "127.0.0.1:0", option.WithTcpHandleMessage(tcpOndataHandler{rec})); err != nil {
		t.Fatalf("listen err: %v", err)
	}
	addr := svr.listeners[0].Listener.Addr().String()
	go svr.Serve()

	conn := dialRawTCP(t, addr)
	defer conn.Close()

	if _, err := conn.Write(ondataPayload); err != nil {
		t.Fatalf("write err: %v", err)
	}

	select {
	case got := <-rec.got:
		t.Fatalf("TCP 上裸数据不该到 OnData，却收到了 %q", got.data)
	case <-time.After(500 * time.Millisecond):
	}

	// 分帧失败即结束连接：对端应当读到 EOF/错误，而不是还在等数据
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 64)
	if n, err := conn.Read(buf); err == nil {
		t.Fatalf("裸数据应让服务端分帧失败并断开，连接却仍可读（读到 %d 字节）", n)
	}
	svr.Close()
}
