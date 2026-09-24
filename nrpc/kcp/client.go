package kcp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/codec"
	"github.com/w6xian/sloth/v4/logger"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/metrics"
	"github.com/w6xian/sloth/v4/nrpc"
	"github.com/w6xian/sloth/v4/nrpc/stream"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types/auth"
	"github.com/w6xian/sloth/v4/types/handler"
	"github.com/w6xian/sloth/v4/types/trpc"

	"github.com/gorilla/mux"
)

// KcpClientHandleMessage 客户端连接事件钩子（与 TCP / QUIC 同一套）。
type KcpClientHandleMessage = handler.TcpClientHandleMessage

// KcpClient KCP 客户端：实现 trpc.ICall，可直接接入 ServerRpc 的调用链。
type KcpClient struct {
	nrpc.RpcConn

	address string
	handler KcpClientHandleMessage
	// cfg KCP 参数，由 option.WithKCPConfig 注入；拨号时用它建连接。
	cfg       option.KCPConfig
	ch        atomic.Pointer[KcpChannel]
	authMu    sync.RWMutex
	auth      *auth.AuthInfo
	queueSize int
	closeOnce sync.Once
	closeChan chan struct{}
	m         stream.Metrics
}

// ── option.IConnectOption ──────────────────────────────────────────────

func (c *KcpClient) SetRouter(router *mux.Router) error { return nil }
func (c *KcpClient) SetUriPath(path string) error       { return nil }
func (c *KcpClient) SetAddress(address string) error {
	c.address = address
	return nil
}
func (c *KcpClient) SetHeader(key string, value string) error { return nil }
func (c *KcpClient) SetOrigin(origins ...string) error        { return nil }
func (c *KcpClient) SetCodec(cd codec.Codec)                  { c.Codec = cd }

func (c *KcpClient) SetKCPConfig(cfg option.KCPConfig) { c.cfg = cfg }

// SetTcpClientHandleMessage 只接受非 HTTP 版钩子（HTTP 版带 *http.Response）。
func (c *KcpClient) SetTcpClientHandleMessage(h KcpClientHandleMessage) { c.handler = h }

func (c *KcpClient) SetClientHandleMessage(h handler.IClientHandleMessage) error { return nil }

func (c *KcpClient) SetServerHandleMessage(h handler.IServerHandleMessage) error {
	return errors.New("SetServerHandleMessage is not implemented on kcp client")
}

func (c *KcpClient) SetChannelQueueSize(n int) {
	if n > 0 {
		c.queueSize = n
	}
}

func NewKcpClient(connect trpc.ICallRpc, opts ...option.ConnectOption) *KcpClient {
	c := new(KcpClient)
	c.Connect = connect
	c.address = "127.0.0.1:8080"
	c.closeChan = make(chan struct{})

	opt := connect.Options()
	c.WriteWait = opt.WriteWait
	c.ReadWait = opt.ReadWait
	c.queueSize = opt.ChannelQueueSize
	if c.queueSize <= 0 {
		c.queueSize = defaultQueueSize
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

func (c *KcpClient) registerMetrics() {
	c.m = stream.Metrics{
		PumpRecovers: metrics.NewCounter("sloth_kcp_client_pump_recovers_total", "客户端读写循环 panic 被兜住的次数"),
		FrameErrors:  metrics.NewCounter("sloth_kcp_client_frame_errors_total", "客户端帧解析失败次数"),
	}
}

// ListenAndServe 建立连接并服务它；之后由后台循环接管重连。
//
// 与 TCP 客户端一致：首次拨号同步返回 error，之后无论成败都交给后台重连循环
// （500ms → 30s 指数退避）。UDP 下这点更重要：对端重启不会给本端任何通知，
// 只能靠重连把连接重新拉起来。
func (c *KcpClient) ListenAndServe(ctx context.Context) error {
	select {
	case <-c.closeChan:
		return errors.New("kcp client closed")
	default:
	}

	done, err := c.serveOnce(ctx)
	go nrpc.ServeReconnect(ctx, nrpc.ReconnectConfig{
		Name:       "kcp",
		Addr:       c.address,
		Dial:       c.serveOnce,
		Done:       done,
		CloseChan:  c.closeChan,
		Reconnects: kcpClientReconnects,
		Logf:       c.log,
	})
	return err
}

// serveOnce 拨一条连接并起它的读写循环，返回该连接结束时会关闭的信号。
func (c *KcpClient) serveOnce(ctx context.Context) (<-chan struct{}, error) {
	conn, err := Dial(c.address, c.cfg)
	if err != nil {
		return nil, err
	}
	// 拨号期间可能已被 Close()
	select {
	case <-c.closeChan:
		_ = conn.Close()
		return nil, errors.New("kcp client closed")
	default:
	}
	ch := newKcpChannel(c.Connect, conn, stream.ClientIP(conn.RemoteAddr()), c.queueSize)
	if c.WriteWait > 0 {
		ch.PWriteWait = c.WriteWait
	}
	if c.ReadWait > 0 {
		ch.PReadWait = c.ReadWait
	}
	if a := c.authSnapshot(); a != nil {
		_ = ch.SetLocalAuth(a)
	}
	c.ch.Store(ch)

	if c.handler != nil {
		if err := c.handler.OnConnect(ctx, c.address); err != nil {
			_ = conn.Close()
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
			close(done)
		}()
		if c.handler != nil {
			_ = c.handler.OnReady(ctx, ch)
		}
		stream.RunPump(ctx, ch, c.dispatch(ch), c.m)
	}()
	return done, nil
}

// log 重连循环的日志出口。
func (c *KcpClient) log(level logger.LogLevel, line string, args ...any) {
	logger.Logf(level, nil, line, args...)
}

// kcpClientReconnects 客户端重连尝试次数（失败一次记一次）。
var kcpClientReconnects = metrics.NewCounter(
	"sloth_kcp_client_reconnects_total", "KCP 客户端重连尝试次数")

// dispatch 客户端入站分发：与服务端共用同一个 DispatchMessage 入口。
func (c *KcpClient) dispatch(ch *KcpChannel) func(context.Context, []byte) error {
	return func(ctx context.Context, raw []byte) error {
		return nrpc.DispatchMessage(nrpc.RouteArgs{
			Context: ctx,
			Data:    raw,
			Codec:   c.Codec,
			OnFn: func(ctx context.Context, raw []byte) error {
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

func (c *KcpClient) Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {
	ch := c.ch.Load()
	if ch == nil {
		return nil, fmt.Errorf("kcp client not connected: %w", errConnClosed)
	}
	return ch.Call(ctx, header, mtd, args...)
}

func (c *KcpClient) Push(ctx context.Context, msg *message.Msg) error {
	ch := c.ch.Load()
	if ch == nil {
		return fmt.Errorf("kcp client not connected: %w", errConnClosed)
	}
	return ch.Push(ctx, msg)
}

func (c *KcpClient) DefaultHeader() message.Header {
	if ch := c.ch.Load(); ch != nil {
		return ch.DefaultHeader()
	}
	return message.Header{}
}

func (c *KcpClient) GetAuthInfo() (*auth.AuthInfo, error) {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	if c.auth == nil {
		return nil, errors.New("kcp client: auth info not set")
	}
	return c.auth, nil
}

// SetAuthInfo 保存身份：连接重建后仍可复用。
func (c *KcpClient) SetAuthInfo(a *auth.AuthInfo) error {
	if a == nil {
		return errors.New("kcp client: nil auth info")
	}
	c.authMu.Lock()
	c.auth = a
	c.authMu.Unlock()
	if ch := c.ch.Load(); ch != nil {
		return ch.SetLocalAuth(a)
	}
	return nil
}

// authSnapshot 取身份快照（带锁），建新连接时用它补身份。
func (c *KcpClient) authSnapshot() *auth.AuthInfo {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	if c.auth == nil {
		return nil
	}
	cp := *c.auth
	return &cp
}

// Ready 是否已建立连接。
func (c *KcpClient) Ready() bool { return c.ch.Load() != nil }

func (c *KcpClient) Close() error {
	c.closeOnce.Do(func() { close(c.closeChan) })
	if ch := c.ch.Load(); ch != nil {
		return ch.Close()
	}
	return nil
}

// emptyBucket 客户端侧的 types.IBucket 空实现。
type emptyBucket struct{}

func (emptyBucket) Bucket(userId int64) *bucket.Bucket                    { return nil }
func (emptyBucket) Channel(userId int64) bucket.IChannel                  { return nil }
func (emptyBucket) Room(roomId int64) *bucket.Room                        { return nil }
func (emptyBucket) Broadcast(ctx context.Context, msg *message.Msg) error { return nil }

// 编译期断言：客户端必须满足 trpc.ICall 与 option 接口。
var (
	_ trpc.ICall            = (*KcpClient)(nil)
	_ option.IConnectOption = (*KcpClient)(nil)
)
