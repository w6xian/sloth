package stream

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/logger"
	"github.com/w6xian/sloth/v4/metrics"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/nrpc"
	"github.com/w6xian/sloth/v4/types/auth"
	"github.com/w6xian/sloth/v4/types/trpc"
)

// DefaultQueueSize 每条连接各队列的默认容量（与 wsocket 一致）。
const DefaultQueueSize = 10

var (
	// ErrConnClosed 连接已关闭：对已关闭的连接发起 RPC / Push 时返回它，
	// 调用方可用 errors.Is 判定后决定要不要重建连接。
	ErrConnClosed = errors.New("connection closed")
	// ErrQueueFull 待发队列已满（对端消费不过来）。
	ErrQueueFull = errors.New("queue full")
)

// Metrics 读写泵需要的两个计数器。
//
// 指标命名由各传输自己决定（tcp 注册 sloth_tcp_*、quic 注册 sloth_quic_*），
// 这里只要求结构：把 counter 传进来，泵负责在对应时机自增。
// 之所以不在这里直接注册，是因为同一种传输的服务端与客户端各有一组指标，
// 名字必须带传输名才不会撞。
type Metrics struct {
	PumpRecovers *metrics.Counter
	FrameErrors  *metrics.Counter
}

// Channel 一条字节流连接（TCP 连接 / QUIC stream 都用它）。
// 服务端与客户端共用：两端都是"读帧 → dispatch / 写队列 → 写帧"，
// 差异只在入站分发与身份语义。
type Channel struct {
	nrpc.RpcChannel

	conn     net.Conn
	bcast    chan *message.Msg
	done     chan struct{}
	doneOnce sync.Once
	closed   atomic.Bool
	// head 本连接复用的帧头缓冲（ReadFrame 用）
	head headBuf
	// reader 带缓冲的读端：减少小帧场景下的 read 系统调用
	reader *bufio.Reader

	// bucket 链表字段（仅服务端语义；客户端恒为空）
	_room   *bucket.Room
	_next   bucket.IChannel
	_prev   bucket.IChannel
	_userId int64
	_sign   string

	errHandler func(err error)
	queueSize  int
}

// NewChannel 在一条 net.Conn 上建通道。conn 可以是 TCP 连接，
// 也可以是 QUIC 的一条 stream——只要有 net.Conn 语义即可。
func NewChannel(connect trpc.ICallRpc, conn net.Conn, ip string, queueSize int) *Channel {
	if queueSize <= 0 {
		queueSize = DefaultQueueSize
	}
	ch := new(Channel)
	ch.Connect = connect
	ch.conn = conn
	ch.PAddr = ip
	ch.PWriteWait = 10 * time.Second
	ch.PReadWait = 10 * time.Second
	ch.bcast = make(chan *message.Msg, queueSize)
	ch.PRpcCaller = make(chan []byte, queueSize)
	ch.PRpcBacker = make(chan []byte, queueSize)
	ch.PSend = make(chan *message.Msg, queueSize)
	ch.done = make(chan struct{})
	ch.reader = bufio.NewReaderSize(conn, 4096)
	ch.queueSize = queueSize
	ch.errHandler = func(err error) {
		logger.Errorw(nil, "stream channel error", "err", err)
	}
	ch.InitCalls() // per-call 回包分发表：SendData 依赖，未初始化会直接失败
	return ch
}

func (ch *Channel) OnError(f func(err error)) { ch.errHandler = f }

func (ch *Channel) IsClosed() bool { return ch.closed.Load() }

func (ch *Channel) Next(n ...bucket.IChannel) bucket.IChannel {
	if len(n) > 0 {
		ch._next = n[0]
	}
	return ch._next
}

func (ch *Channel) Prev(p ...bucket.IChannel) bucket.IChannel {
	if len(p) > 0 {
		ch._prev = p[0]
	}
	return ch._prev
}

func (ch *Channel) Room(r ...*bucket.Room) *bucket.Room {
	if len(r) > 0 {
		ch._room = r[0]
	}
	return ch._room
}

func (ch *Channel) UserId(u ...int64) int64 {
	if len(u) > 0 {
		ch._userId = u[0]
	}
	return ch._userId
}

func (ch *Channel) Token(t ...string) string {
	if len(t) > 0 {
		ch._sign = t[0]
	}
	return ch._sign
}

func (ch *Channel) Logout() { ch._userId = 0 }

// GetAuthInfo 服务端语义：身份由业务在登录 RPC 里通过 bucket.Put 写入。
func (ch *Channel) GetAuthInfo() (*auth.AuthInfo, error) {
	if ch._userId == 0 {
		return nil, errors.New("stream channel: user id is 0")
	}
	if ch._room == nil {
		return nil, errors.New("stream channel: room is nil")
	}
	if ch._sign == "" {
		return nil, errors.New("stream channel: sign is empty")
	}
	return &auth.AuthInfo{
		UserId: ch._userId,
		RoomId: ch._room.Id,
		Token:  ch._sign,
	}, nil
}

func (ch *Channel) SetAuthInfo(a *auth.AuthInfo) error {
	return errors.New("stream channel: server does not support set auth info")
}

// Push 投递一条推送消息（服务端广播 / 客户端上行都走这里）。
// 只入队，不直接写连接——写由 writePump 串行完成（net.Conn 不支持并发写）。
func (ch *Channel) Push(ctx context.Context, msg *message.Msg) error {
	if ch.closed.Load() {
		return fmt.Errorf("push on closed channel: %w", ErrConnClosed)
	}
	timer := time.NewTimer(ch.PWriteWait)
	defer timer.Stop()
	select {
	case ch.bcast <- msg:
		return nil
	case <-timer.C:
		return fmt.Errorf("push queue full: %w", ErrQueueFull)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (ch *Channel) Close() error {
	ch.Lock.Lock()
	defer ch.Lock.Unlock()

	ch.doneOnce.Do(func() { close(ch.done) })
	// 唤醒所有等回包的调用：否则它们要干等到超时才返回
	ch.CloseCalls()
	ch.closed.Store(true)
	if ch.conn != nil {
		_ = ch.conn.Close()
	}
	return nil
}

// RunPump 启动读写泵，阻塞到连接结束。
//
// 读：按 FN 帧分帧 → 交给 dispatch（服务端是 HandleFn/业务钩子，客户端是回包分发）。
// 写：串行消费 bcast / PRpcCaller / PRpcBacker —— 单一写者，无需加锁。
func RunPump(ctx context.Context, ch *Channel, dispatch func(context.Context, []byte) error, m Metrics) {
	defer func() {
		if err := recover(); err != nil {
			if m.PumpRecovers != nil {
				m.PumpRecovers.Inc()
			}
			logger.Errorw(ctx, "stream pump recover", "err", err, "remote", ch.PAddr)
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		writePump(ctx, ch)
	}()

	readPump(ctx, ch, dispatch, m)

	// 读循环结束即连接结束：通知写循环退出并等它收尾
	_ = ch.Close()
	<-done
}

// readPump 读循环：分帧 + 分发。
func readPump(ctx context.Context, ch *Channel, dispatch func(context.Context, []byte) error, m Metrics) {
	for {
		frame, err := ReadFrame(ch.reader, ch.head[:])
		if err != nil {
			// 连接正常关闭（EOF）或超时都走这里；不区分，交给上层清理
			if ch.errHandler != nil && !errors.Is(err, net.ErrClosed) {
				ch.errHandler(err)
			}
			return
		}
		if err := dispatch(ctx, frame); err != nil {
			if m.FrameErrors != nil {
				m.FrameErrors.Inc()
			}
			logger.Warnw(ctx, "stream dispatch failed", "err", err, "remote", ch.PAddr)
		}
	}
}

// writePump 写循环：串行写出所有待发数据。
func writePump(ctx context.Context, ch *Channel) {
	for {
		select {
		case <-ch.done:
			return
		case <-ctx.Done():
			return
		case msg := <-ch.bcast:
			if err := ch.write(msg.MarshalJSONFast()); err != nil {
				return
			}
		case payload := <-ch.PRpcCaller:
			// 本端主动发起的 RPC 请求
			if err := ch.write(payload); err != nil {
				return
			}
		case payload := <-ch.PRpcBacker:
			// 对端 RPC 调用的回包
			if err := ch.write(payload); err != nil {
				return
			}
		case msg := <-ch.PSend:
			if msg == nil {
				continue
			}
			if err := ch.write(msg.MarshalJSONFast()); err != nil {
				return
			}
		}
	}
}

// write 写一帧并刷新。FN 帧自带 length，对端按它分帧。
func (ch *Channel) write(payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	if ch.PWriteWait > 0 {
		if err := ch.conn.SetWriteDeadline(time.Now().Add(ch.PWriteWait)); err != nil {
			return err
		}
	}
	_, err := ch.conn.Write(payload)
	return err
}

// ClientIP 从远端地址里取 IP（去掉端口）。
func ClientIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr.String()); err == nil {
		return host
	}
	return addr.String()
}
