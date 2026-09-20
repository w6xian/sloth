package wsocket

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/internal/errs"
	"github.com/w6xian/sloth/v3/internal/logger"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/nrpc"
	"github.com/w6xian/sloth/v3/types/auth"
	"github.com/w6xian/sloth/v3/types/trpc"

	"github.com/gorilla/websocket"
)

// 服务器端对客户端的连接通道
// in fact, Channel it's a user Connect session
type WsChannelServer struct {
	nrpc.RpcChannel
	_room       *bucket.Room
	_next       bucket.IChannel
	_prev       bucket.IChannel
	broadcast   chan *message.Msg
	_userId     int64
	_sign       string
	Conn        *websocket.Conn
	pongTimeout time.Duration
	// error handler
	errHandler func(err error)
	// done 由 Close 一次性关闭，writePump 监听它实现服务端主动优雅断开
	done     chan struct{}
	doneOnce sync.Once
	// closed 标记连接是否已关闭，供调用方（如 bucket/ClientRpc）并发安全地快速失败，
	// 避免向一条已经关闭的连接发起 RPC 后白白阻塞到写超时。
	closed atomic.Bool
	// sliceSeq 本连接的分片序号，仅由 writePump 协程访问（无需加锁）
	sliceSeq uint32
	// shard 本连接绑定的入站 worker 序号（建连时确定，终身不变）。
	// 同一连接的所有消息都交给同一个 worker，因此业务看到的是按序到达。
	shard uint32
	// queueSize 本连接各队列的容量（可配置，见 WithServerQueueSize）
	queueSize int
	// onQueueFull 队列满时的观测钩子（可为空），用于统计被背压丢弃的投递
	onQueueFull func()
}

// queueFull 报告一次"因队列满而投递失败"，供上层观测背压强度。
func (ch *WsChannelServer) queueFull() {
	if ch.onQueueFull != nil {
		ch.onQueueFull()
	}
}

// IsClosed 报告连接是否已关闭（并发安全）。
func (ch *WsChannelServer) IsClosed() bool {
	return ch.closed.Load()
}

// nextSliceName 返回本连接的下一个分片名（00-99 循环）。
// 全局计数器版本存在多核原子竞争 + 每次 fmt.Sprintf 分配，这里改为连接内自增 + 查表。
func (ch *WsChannelServer) nextSliceName() string {
	ch.sliceSeq++
	if ch.sliceSeq > 99 {
		ch.sliceSeq = 0
	}
	return sliceNames[ch.sliceSeq]
}

func (ch *WsChannelServer) Next(n ...bucket.IChannel) bucket.IChannel {
	if len(n) > 0 {
		ch._next = n[0]
	}
	return ch._next
}

func (ch *WsChannelServer) Prev(p ...bucket.IChannel) bucket.IChannel {
	if len(p) > 0 {
		ch._prev = p[0]
	}
	return ch._prev
}
func (ch *WsChannelServer) Room(r ...*bucket.Room) *bucket.Room {
	if len(r) > 0 {
		ch._room = r[0]
	}
	return ch._room
}

func (ch *WsChannelServer) UserId(u ...int64) int64 {
	if len(u) > 0 {
		ch._userId = u[0]
	}
	return ch._userId
}
func (ch *WsChannelServer) Token(t ...string) string {
	if len(t) > 0 {
		ch._sign = t[0]
	}
	return ch._sign
}

// login 登录
func (ch *WsChannelServer) GetAuthInfo() (*auth.AuthInfo, error) {
	if ch._userId == 0 {
		return nil, errors.New("user id is 0")
	}
	if ch._room == nil {
		return nil, errors.New("room is nil")
	}
	if ch._sign == "" {
		return nil, errors.New("sign is empty")
	}
	return &auth.AuthInfo{
		UserId: ch._userId,
		RoomId: ch._room.Id,
		Token:  ch._sign,
	}, nil
}

func (ch *WsChannelServer) SetAuthInfo(auth *auth.AuthInfo) error {
	return errors.New("server not support set auth info")
}

// logout 登出
func (ch *WsChannelServer) Logout() {
	ch._userId = 0
}

func (ch *WsChannelServer) Close() error {
	ch.Lock.Lock()
	defer ch.Lock.Unlock()

	ch.doneOnce.Do(func() { close(ch.done) })
	// 唤醒所有还在等回包的调用：否则它们要干等到超时（默认 10s）才返回
	ch.CloseCalls()
	ch.closed.Store(true)
	if ch.Conn != nil {
		ch.Conn.Close()
	}
	// 注意：此处不清空 _userId/PAddr/PPort/Sign。
	// readPump 的 defer 在 Close 之后仍需读取 ch.UserId()/ch.PAddr() 完成
	// bucket 移除与连接计数释放；且 writePump/readPump 的 defer 会并发调用 Close，
	// 清理字段会引入数据竞争。WsChannelServer 为每连接新建、不复用，无需清理。
	return nil
}

func NewWsChannelServer(connect trpc.ICallRpc, opts ...ChannelServerOption) (c *WsChannelServer) {
	c = new(WsChannelServer)
	c.Lock = sync.Mutex{}
	c.queueSize = defaultChannelQueueSize
	c.done = make(chan struct{})
	c.InitCalls() // per-call 回包分发表（SendData 依赖，未初始化会直接失败）
	c.Next(nil)
	c.Prev(nil)
	c.pongTimeout = 54 * time.Second
	c.PWriteWait = 10 * time.Second
	c.PReadWait = 10 * time.Second
	c._sign = ""
	c.Connect = connect
	c.errHandler = func(err error) {
		logger.Errorw(nil, "channel error handler", "err", err)
	}
	for _, opt := range opts {
		opt(c)
	}
	// 队列按最终配置创建（option 可能改过 queueSize / 注入了队列满钩子）
	c.broadcast = make(chan *message.Msg, c.queueSize)
	c.PRpcCaller = make(chan []byte, c.queueSize)
	c.PRpcBacker = make(chan []byte, c.queueSize)
	c.PRpcResult = make(chan []byte, c.queueSize) // 已废弃：回包走 pending 分发
	return
}

func (ch *WsChannelServer) OnError(f func(err error)) {
	ch.errHandler = f
}

func (ch *WsChannelServer) Push(ctx context.Context, msg *message.Msg) (err error) {
	// 必须 Stop：原实现每个 timer 都会存活到 PWriteWait(10s) 才被回收，
	// 高频推送下定时器大量堆积（隐性泄漏）。
	timer := time.NewTimer(ch.PWriteWait)
	defer timer.Stop()
	select {
	case ch.broadcast <- msg:
	case <-timer.C:
		ch.queueFull()
		return fmt.Errorf("rpc reply queue full: %w", errs.ErrQueueFull)
	case <-ctx.Done():
		return ctx.Err()
	}
	return
}
