package tcp

import (
	"context"
	"errors"
	"fmt"
	"net"
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

// TcpClientHandleMessage 客户端连接事件钩子（无 HTTP 依赖，理由同服务端钩子）。
// 接口本体在 types/handler：option 要用它构造 ConnectOption，
// 而 option 不能被传输包反向 import（会成环）。
type TcpClientHandleMessage = handler.TcpClientHandleMessage

// TcpClient TCP 客户端：实现 trpc.ICall，可直接接入 ServerRpc 的调用链。
type TcpClient struct {
	nrpc.RpcConn

	address   string
	handler   TcpClientHandleMessage
	ch        atomic.Pointer[TcpChannel]
	authMu    sync.RWMutex
	auth      *auth.AuthInfo
	queueSize int
	closeOnce sync.Once
	closeChan chan struct{}
	m         stream.Metrics
}

// ── option.IConnectOption ─────────────────────────────────────────────

func (c *TcpClient) SetRouter(router *mux.Router) error { return nil }
func (c *TcpClient) SetUriPath(path string) error       { return nil }
func (c *TcpClient) SetAddress(address string) error {
	c.address = address
	return nil
}
func (c *TcpClient) SetHeader(key string, value string) error { return nil }
func (c *TcpClient) SetOrigin(origins ...string) error        { return nil }
func (c *TcpClient) SetCodec(cd codec.Codec)                  { c.Codec = cd }

// SetClientHandleMessage 只接受 TCP 版钩子（HTTP 版钩子带 *http.Response，TCP 没有）。
func (c *TcpClient) SetTcpClientHandleMessage(h TcpClientHandleMessage) { c.handler = h }

func (c *TcpClient) SetClientHandleMessage(h handler.IClientHandleMessage) error { return nil }

func (c *TcpClient) SetServerHandleMessage(h handler.IServerHandleMessage) error {
	return errors.New("SetServerHandleMessage is not implemented on tcp client")
}

func (c *TcpClient) SetChannelQueueSize(n int) {
	if n > 0 {
		c.queueSize = n
	}
}

func NewTcpClient(connect trpc.ICallRpc, opts ...option.ConnectOption) *TcpClient {
	c := new(TcpClient)
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

func (c *TcpClient) registerMetrics() {
	c.m = stream.Metrics{
		PumpRecovers: metrics.NewCounter("sloth_tcp_client_pump_recovers_total", "客户端读写循环 panic 被兜住的次数"),
		FrameErrors:  metrics.NewCounter("sloth_tcp_client_frame_errors_total", "客户端帧解析失败次数"),
	}
}

// ListenAndServe 建立连接并服务它；之后由后台循环接管重连。
//
// 与 ws 版的行为差异在这里收敛掉了：
//
//   - 首次拨号**同步**返回 error（保持原语义，调用方据此决定退出还是继续跑）；
//   - 无论首次成功与否，后台都会起一个重连循环：连不上（服务器还没起来、正在
//     维护）按 500ms → 30s 指数退避重试；连上之后又被断掉则立刻重连。
//     这样"服务端重启 / 网络抖动"对客户端是自动恢复的，不需要业务自己轮询。
//
// 重连后要做的事由业务决定：身份会自动补到新连接上（见 serveOnce 里的
// authSnapshot），但**服务端 bucket 里的 channel 必须重新注册**——所以要在
// handler 的 OnReady 里重新 Sign/Reg（不能在 OnReady 里同步发 RPC，pump 还没
// 跑起来会等不到回包，应另起 goroutine）。
func (c *TcpClient) ListenAndServe(ctx context.Context) error {
	select {
	case <-c.closeChan:
		return errors.New("tcp client closed")
	default:
	}

	// 首次连接：失败同步返回，调用方能看到"现在连不上"。
	done, err := c.serveOnce(ctx)
	// 之后交给后台：服务器维护期间就在后台等着，起来后自动连上。
	// 循环本身是传输无关的公共实现（nrpc.ServeReconnect，quic 客户端同源）。
	go nrpc.ServeReconnect(ctx, nrpc.ReconnectConfig{
		Name:       "tcp",
		Addr:       c.address,
		Dial:       c.serveOnce,
		Done:       done,
		CloseChan:  c.closeChan,
		Reconnects: tcpClientReconnects,
		Logf:       c.log,
	})
	return err
}

// serveOnce 建一条连接并起它的读写循环，返回该连接结束时会关闭的信号。
//
// done 为 nil 表示这次没连上（调用方据此退避重试）。
func (c *TcpClient) serveOnce(ctx context.Context) (<-chan struct{}, error) {
	conn, err := net.Dial("tcp", c.address)
	if err != nil {
		return nil, fmt.Errorf("tcp dial %s: %w", c.address, err)
	}
	// 拨号期间可能已被 Close()：这条连接就别往上挂了，否则 Close 之后
	// 还会残留一条正在跑的连接（重连循环此时已经退出，没人会关它）。
	select {
	case <-c.closeChan:
		_ = conn.Close()
		return nil, errors.New("tcp client closed")
	default:
	}
	ch := newTcpChannel(c.Connect, conn, stream.ClientIP(conn.RemoteAddr()), c.queueSize)
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

// log 重连循环的日志出口（签名与 nrpc.ReconnectConfig.Logf 对齐）。
func (c *TcpClient) log(level logger.LogLevel, line string, args ...any) {
	logger.Logf(level, nil, line, args...)
}

// tcpClientReconnects 客户端重连尝试次数（失败一次记一次）。
var tcpClientReconnects = metrics.NewCounter(
	"sloth_tcp_client_reconnects_total", "TCP 客户端重连尝试次数")

// dispatch 客户端入站分发：与服务端共用同一个 DispatchMessage 入口，
// 因此"回包/服务端反调/裸数据"的判定规则两端完全一致。
func (c *TcpClient) dispatch(ch *TcpChannel) func(context.Context, []byte) error {
	return func(ctx context.Context, raw []byte) error {
		return nrpc.DispatchMessage(nrpc.RouteArgs{
			Context: ctx,
			Data:    raw,
			Codec:   c.Codec,
			OnFn: func(ctx context.Context, raw []byte) error {
				// 客户端没有 bucket 体系：svr 传一个空实现（与 ws 客户端一致）
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

func (c *TcpClient) Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {
	ch := c.ch.Load()
	if ch == nil {
		return nil, fmt.Errorf("tcp client not connected: %w", errConnClosed)
	}
	return ch.Call(ctx, header, mtd, args...)
}

func (c *TcpClient) Push(ctx context.Context, msg *message.Msg) error {
	ch := c.ch.Load()
	if ch == nil {
		return fmt.Errorf("tcp client not connected: %w", errConnClosed)
	}
	return ch.Push(ctx, msg)
}

func (c *TcpClient) DefaultHeader() message.Header {
	if ch := c.ch.Load(); ch != nil {
		return ch.DefaultHeader()
	}
	return message.Header{}
}

func (c *TcpClient) GetAuthInfo() (*auth.AuthInfo, error) {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	if c.auth == nil {
		return nil, errors.New("tcp client: auth info not set")
	}
	return c.auth, nil
}

// SetAuthInfo 保存身份：连接重建后仍可复用（服务端反调时用它填 Header）。
//
// 除了存到 c.auth，还要同步到当前连接——服务端反调客户端方法时，业务代码
// 从 ctx 上取到的 channel 就是它。少这一步，客户端方法里的 GetAuthInfo
// 永远是 "user id is 0"（ws 客户端的连接自带身份，行为不一致）。
func (c *TcpClient) SetAuthInfo(a *auth.AuthInfo) error {
	if a == nil {
		return errors.New("tcp client: nil auth info")
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
func (c *TcpClient) authSnapshot() *auth.AuthInfo {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	if c.auth == nil {
		return nil
	}
	cp := *c.auth
	return &cp
}

// Ready 是否已建立连接（传输无关的就绪判定，供上层/测试轮询）。
func (c *TcpClient) Ready() bool { return c.ch.Load() != nil }

func (c *TcpClient) Close() error {
	c.closeOnce.Do(func() { close(c.closeChan) })
	if ch := c.ch.Load(); ch != nil {
		return ch.Close()
	}
	return nil
}

// emptyBucket 客户端侧的 types.IBucket 空实现：客户端没有 bucket/房间体系，
// 仅为满足 HandleFn 的形参（与 wsocket.LocalClient 的做法一致）。
type emptyBucket struct{}

func (emptyBucket) Bucket(userId int64) *bucket.Bucket                    { return nil }
func (emptyBucket) Channel(userId int64) bucket.IChannel                  { return nil }
func (emptyBucket) Room(roomId int64) *bucket.Room                        { return nil }
func (emptyBucket) Broadcast(ctx context.Context, msg *message.Msg) error { return nil }

// 编译期断言：客户端必须满足 trpc.ICall 与 option 接口。
var (
	_ trpc.ICall            = (*TcpClient)(nil)
	_ option.IConnectOption = (*TcpClient)(nil)
)

// 未实现的能力（留待后续）：
//   - 心跳（Ping/Pong）：FN 帧没有心跳 action，需要协议层补一个。
//     没有心跳就没有"半开连接"探测——对端掉电时本端只能等 TCP 超时才重连；
//   - TLS：可在 Listen/Dial 处包一层 tls.Conn（与 wss 对称）。
//
// 已实现：断线自动重连（见 nrpc.ServeReconnect，退避策略与 ws 客户端一致）。
// 注意重连只恢复**本地身份**，服务端 bucket 里挂的 channel 要业务在
// handler 的 OnReady 里重新 Sign/Reg 才会更新——否则服务端反调会打到旧连接上。
