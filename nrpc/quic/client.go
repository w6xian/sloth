package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	quicgo "github.com/quic-go/quic-go"
	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/internal/codec"
	"github.com/w6xian/sloth/v4/internal/metrics"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/nrpc"
	"github.com/w6xian/sloth/v4/nrpc/stream"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types/auth"
	"github.com/w6xian/sloth/v4/types/handler"
	"github.com/w6xian/sloth/v4/types/trpc"

	"github.com/gorilla/mux"
)

// handshakeTimeout QUIC 握手的等待上限。
//
// UDP 没有 TCP 那样的 RST：地址不可达时不会立刻失败，
// 没有超时就会一直挂到上层 ctx 取消。
const handshakeTimeout = 10 * time.Second

// QuicClientHandleMessage 客户端连接事件钩子（与 TCP 客户端同一套接口：
// 钩子描述的是"一条连接的生命周期"，与底层是 TCP 还是 QUIC 无关）。
type QuicClientHandleMessage = handler.TcpClientHandleMessage

// QuicClient QUIC 客户端：实现 trpc.ICall，可直接接入 ServerRpc 的调用链。
type QuicClient struct {
	nrpc.RpcConn

	address   string
	tlsConf   *tls.Config
	quicConf  *quicgo.Config
	handler   QuicClientHandleMessage
	ch        atomic.Pointer[stream.Channel]
	authMu    sync.RWMutex
	auth      *auth.AuthInfo
	queueSize int
	closeOnce sync.Once
	closeChan chan struct{}
	m         stream.Metrics
}

// ── option.IConnectOption ─────────────────────────────────────────────

func (c *QuicClient) SetRouter(router *mux.Router) error { return nil }
func (c *QuicClient) SetUriPath(path string) error       { return nil }

func (c *QuicClient) SetAddress(address string) error {
	c.address = address
	return nil
}

func (c *QuicClient) SetHeader(key string, value string) error { return nil }
func (c *QuicClient) SetOrigin(origins ...string) error        { return nil }
func (c *QuicClient) SetCodec(cd codec.Codec)                  { c.Codec = cd }

// SetTLSConfig 客户端的 TLS 配置（QUIC 必填）。
func (c *QuicClient) SetTLSConfig(conf *tls.Config) { c.tlsConf = conf }

// SetQuicConfig 透传 QUIC 参数（MaxIdleTimeout / KeepAlivePeriod 等）。
func (c *QuicClient) SetQuicConfig(conf *quicgo.Config) { c.quicConf = conf }

func (c *QuicClient) SetTcpClientHandleMessage(h QuicClientHandleMessage) { c.handler = h }

func (c *QuicClient) SetClientHandleMessage(h handler.IClientHandleMessage) error { return nil }

func (c *QuicClient) SetServerHandleMessage(h handler.IServerHandleMessage) error {
	return errors.New("SetServerHandleMessage is not implemented on quic client")
}

func (c *QuicClient) SetChannelQueueSize(n int) {
	if n > 0 {
		c.queueSize = n
	}
}

func NewQuicClient(connect trpc.ICallRpc, opts ...option.ConnectOption) *QuicClient {
	c := new(QuicClient)
	c.Connect = connect
	c.address = "127.0.0.1:8080"
	c.closeChan = make(chan struct{})

	opt := connect.Options()
	c.WriteWait = opt.WriteWait
	c.ReadWait = opt.ReadWait
	c.queueSize = opt.ChannelQueueSize
	if c.queueSize <= 0 {
		c.queueSize = stream.DefaultQueueSize
	}
	if c.WriteWait <= 0 {
		c.WriteWait = 10 * time.Second
	}
	if c.ReadWait <= 0 {
		c.ReadWait = 10 * time.Second
	}
	c.registerMetrics()
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *QuicClient) registerMetrics() {
	c.m = stream.Metrics{
		PumpRecovers: metrics.NewCounter("sloth_quic_client_pump_recovers_total", "客户端读写循环 panic 被兜住的次数"),
		FrameErrors:  metrics.NewCounter("sloth_quic_client_frame_errors_total", "客户端帧解析失败次数"),
	}
}

// ListenAndServe 建立 QUIC 连接并开一条流，之后在后台服务它。
//
// 与 TCP 客户端一致：只建一条连接，断开即结束（不做重连——重连后是否重新
// Sign、身份是否重建是个语义问题，未定不动，见 README 的"传输层差异"）。
func (c *QuicClient) ListenAndServe(ctx context.Context) error {
	select {
	case <-c.closeChan:
		return errors.New("quic client closed")
	default:
	}
	if c.tlsConf == nil {
		return errNoTLSCert
	}
	if c.quicConf == nil {
		c.quicConf = defaultQuicConfig()
	}
	// 握手带超时：地址不可达时 UDP 不会像 TCP 那样快速失败（没有 RST），
	// 不设超时会一直等到 ctx 取消。
	dialCtx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()

	qconn, err := quicgo.DialAddr(dialCtx, c.address, withALPN(c.tlsConf), c.quicConf)
	if err != nil {
		return fmt.Errorf("quic dial %s: %w", c.address, err)
	}
	st, err := qconn.OpenStreamSync(dialCtx)
	if err != nil {
		_ = qconn.CloseWithError(quicgo.ApplicationErrorCode(0), "")
		return fmt.Errorf("quic open stream %s: %w", c.address, err)
	}
	conn := &streamConn{Stream: st, local: qconn.LocalAddr(), remote: qconn.RemoteAddr()}

	ch := stream.NewChannel(c.Connect, conn, stream.ClientIP(conn.RemoteAddr()), c.queueSize)
	if c.WriteWait > 0 {
		ch.PWriteWait = c.WriteWait
	}
	if c.ReadWait > 0 {
		ch.PReadWait = c.ReadWait
	}
	c.ch.Store(ch)

	if c.handler != nil {
		if err := c.handler.OnConnect(ctx, c.address); err != nil {
			_ = ch.Close()
			c.ch.Store(nil)
			return err
		}
	}
	go func() {
		defer func() {
			c.ch.Store(nil)
			if c.handler != nil {
				_ = c.handler.OnClose(ctx, ch)
			}
			_ = ch.Close()
			// 流关闭后 QUIC 连接本身也要关，否则 idle timeout 之前一直占着
			_ = qconn.CloseWithError(quicgo.ApplicationErrorCode(0), "")
		}()
		if c.handler != nil {
			_ = c.handler.OnReady(ctx, ch)
		}
		stream.RunPump(ctx, ch, c.dispatch(ch), c.m)
	}()
	return nil
}

// dispatch 客户端入站分发：与服务端共用同一个 DispatchMessage 入口。
func (c *QuicClient) dispatch(ch *stream.Channel) func(context.Context, []byte) error {
	return func(ctx context.Context, raw []byte) error {
		return nrpc.DispatchMessage(nrpc.RouteArgs{
			Context: ctx,
			Data:    raw,
			Codec:   c.Codec,
			OnFn: func(ctx context.Context, raw []byte) error {
				// 客户端没有 bucket 体系：svr 传一个空实现（与 ws / tcp 客户端一致）
				return nrpc.HandleFn(ctx, nil, nil, emptyBucket{}, c.Connect, ch, raw)
			},
			OnData: func(ctx context.Context, raw []byte) error {
				if c.handler == nil {
					return nil
				}
				return c.handler.OnData(ctx, ch, raw)
			},
		})
	}
}

// ── trpc.ICall ───────────────────────────────────────────────────────

func (c *QuicClient) Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {
	ch := c.ch.Load()
	if ch == nil {
		return nil, fmt.Errorf("quic client not connected: %w", errConnClosed)
	}
	return ch.Call(ctx, header, mtd, args...)
}

func (c *QuicClient) Push(ctx context.Context, msg *message.Msg) error {
	ch := c.ch.Load()
	if ch == nil {
		return fmt.Errorf("quic client not connected: %w", errConnClosed)
	}
	return ch.Push(ctx, msg)
}

func (c *QuicClient) DefaultHeader() message.Header {
	if ch := c.ch.Load(); ch != nil {
		return ch.DefaultHeader()
	}
	return message.Header{}
}

func (c *QuicClient) GetAuthInfo() (*auth.AuthInfo, error) {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	if c.auth == nil {
		return nil, errors.New("quic client: auth info not set")
	}
	return c.auth, nil
}

// SetAuthInfo 保存身份：连接重建后仍可复用（服务端反调时用它填 Header）。
func (c *QuicClient) SetAuthInfo(a *auth.AuthInfo) error {
	if a == nil {
		return errors.New("quic client: nil auth info")
	}
	c.authMu.Lock()
	c.auth = a
	c.authMu.Unlock()
	return nil
}

// Ready 是否已建立连接（供上层/测试轮询）。
func (c *QuicClient) Ready() bool { return c.ch.Load() != nil }

func (c *QuicClient) Close() error {
	c.closeOnce.Do(func() { close(c.closeChan) })
	if ch := c.ch.Load(); ch != nil {
		return ch.Close()
	}
	return nil
}

// emptyBucket 客户端侧的 types.IBucket 空实现（与 ws / tcp 客户端一致）。
type emptyBucket struct{}

func (emptyBucket) Bucket(userId int64) *bucket.Bucket                    { return nil }
func (emptyBucket) Channel(userId int64) bucket.IChannel                  { return nil }
func (emptyBucket) Room(roomId int64) *bucket.Room                        { return nil }
func (emptyBucket) Broadcast(ctx context.Context, msg *message.Msg) error { return nil }

// 编译期断言：客户端必须满足 trpc.ICall 与 option 接口。
var (
	_ trpc.ICall            = (*QuicClient)(nil)
	_ option.IConnectOption = (*QuicClient)(nil)
)
