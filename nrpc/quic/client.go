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
	"github.com/w6xian/sloth/v4/codec"
	"github.com/w6xian/sloth/v4/logger"
	"github.com/w6xian/sloth/v4/metrics"
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
// 与 TCP 客户端一致（见 nrpc.ServeReconnect）：
//
//   - 首次拨号**同步**返回 error（保持原语义，调用方据此决定退出还是继续跑）；
//   - 无论首次成功与否，后台都会起一个重连循环：连不上（服务器还没起来、正在
//     维护）按 500ms → 30s 指数退避重试；连上之后又被断掉则立刻重连。
//
// 注意 QUIC 每次重拨最多要等 handshakeTimeout（UDP 没有 RST，地址不可达不会
// 立刻失败），所以"服务端还没起来"时两次尝试之间实际是 10s + 退避。
//
// 重连后要做的事由业务决定：身份会自动补到新连接上（见 serveOnce 里的
// authSnapshot），但**服务端 bucket 里的 channel 必须重新注册**——要在 handler
// 的 OnReady 里重新 Sign/Reg（不能在 OnReady 里同步发 RPC，pump 还没跑起来
// 会等不到回包，应另起 goroutine）。
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

	// 首次连接：失败同步返回，调用方能看到"现在连不上"。
	done, err := c.serveOnce(ctx)
	// 之后交给后台：服务器维护期间就在后台等着，起来后自动连上。
	go nrpc.ServeReconnect(ctx, nrpc.ReconnectConfig{
		Name:       "quic",
		Addr:       c.address,
		Dial:       c.serveOnce,
		Done:       done,
		CloseChan:  c.closeChan,
		Reconnects: quicClientReconnects,
		Logf:       c.log,
	})
	return err
}

// serveOnce 握手 + 开流 + 起读写循环，返回该连接结束时会关闭的信号。
//
// done 为 nil 表示这次没连上（调用方据此退避重试）。
func (c *QuicClient) serveOnce(ctx context.Context) (<-chan struct{}, error) {
	// 握手带超时：地址不可达时 UDP 不会像 TCP 那样快速失败（没有 RST），
	// 不设超时会一直等到 ctx 取消。
	dialCtx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	qconn, err := quicgo.DialAddr(dialCtx, c.address, withALPN(c.tlsConf), c.quicConf)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("quic dial %s: %w", c.address, err)
	}
	// 握手最长 10s，期间可能已被 Close()：这时别再开流，否则 Close 之后
	// 会残留一条没人管的 QUIC 连接（重连循环已退出）。
	select {
	case <-c.closeChan:
		_ = qconn.CloseWithError(quicgo.ApplicationErrorCode(0), "")
		return nil, errors.New("quic client closed")
	default:
	}
	// 开流另算一份超时预算：与握手共用同一个 ctx 时，握手耗掉 9.9s 会让
	// 开流只剩 0.1s，白失败一次。
	streamCtx, cancelStream := context.WithTimeout(ctx, handshakeTimeout)
	defer cancelStream()
	st, err := qconn.OpenStreamSync(streamCtx)
	if err != nil {
		_ = qconn.CloseWithError(quicgo.ApplicationErrorCode(0), "")
		return nil, fmt.Errorf("quic open stream %s: %w", c.address, err)
	}
	conn := &streamConn{Stream: st, local: qconn.LocalAddr(), remote: qconn.RemoteAddr()}

	ch := stream.NewChannel(c.Connect, conn, stream.ClientIP(conn.RemoteAddr()), c.queueSize)
	if c.WriteWait > 0 {
		ch.PWriteWait = c.WriteWait
	}
	if c.ReadWait > 0 {
		ch.PReadWait = c.ReadWait
	}
	// 先 SetAuthInfo 的场景、以及重连后恢复身份：把已存的身份补到新连接上。
	// 注意这只是本地身份——服务端 bucket 里的 channel 要靠业务重新 Reg 才会更新。
	if a := c.authSnapshot(); a != nil {
		_ = ch.SetLocalAuth(a)
	}
	c.ch.Store(ch)

	if c.handler != nil {
		if err := c.handler.OnConnect(ctx, c.address); err != nil {
			_ = ch.Close()
			_ = qconn.CloseWithError(quicgo.ApplicationErrorCode(0), "")
			c.ch.Store(nil)
			return nil, err
		}
	}
	done := make(chan struct{})
	go func() {
		defer func() {
			c.ch.Store(nil)
			if c.handler != nil {
				_ = c.handler.OnClose(ctx, ch)
			}
			_ = ch.Close()
			// 流关闭后 QUIC 连接本身也要关，否则 idle timeout 之前一直占着
			_ = qconn.CloseWithError(quicgo.ApplicationErrorCode(0), "")
			close(done)
		}()
		if c.handler != nil {
			_ = c.handler.OnReady(ctx, ch)
		}
		stream.RunPump(ctx, ch, c.dispatch(ch), c.m)
	}()
	return done, nil
}

// log 重连循环的日志出口（签名与 nrpc.ReconnectConfig.Logf 对齐）。
func (c *QuicClient) log(level logger.LogLevel, line string, args ...any) {
	logger.Logf(level, nil, line, args...)
}

// quicClientReconnects 客户端重连尝试次数（失败一次记一次）。
var quicClientReconnects = metrics.NewCounter(
	"sloth_quic_client_reconnects_total", "QUIC 客户端重连尝试次数")

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
//
// 除了存到 c.auth，还要同步到当前连接——服务端反调客户端方法时，业务代码
// 从 ctx 上取到的 channel 就是它。少这一步，客户端方法里的 GetAuthInfo
// 永远是 "user id is 0"（ws 客户端的连接自带身份，行为不一致）。
func (c *QuicClient) SetAuthInfo(a *auth.AuthInfo) error {
	if a == nil {
		return errors.New("quic client: nil auth info")
	}
	c.authMu.Lock()
	c.auth = a
	c.authMu.Unlock()
	if ch := c.ch.Load(); ch != nil {
		return ch.SetLocalAuth(a)
	}
	return nil
}

// authSnapshot 取身份快照（带锁），建新连接时用它把身份补到连接上。
func (c *QuicClient) authSnapshot() *auth.AuthInfo {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	if c.auth == nil {
		return nil
	}
	cp := *c.auth
	return &cp
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
