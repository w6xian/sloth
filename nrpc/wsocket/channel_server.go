package wsocket

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/errs"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/nrpc"
	"github.com/w6xian/sloth/v4/types/auth"
	"github.com/w6xian/sloth/v4/types/trpc"

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
	// errHandler 连接级错误钩子（区别于全局的 handler.OnError）：
	// 只在"真异常"上触发——非预期关闭、分帧/解码失败；
	// 预期断开（对端主动 close、EOF、读超时）走业务的 OnClose，不触发它，
	// 否则每次客户端正常下线都会回调一次。
	// 读循环与 OnError 可能并发（OnError 通常在 OnReady 里注册），用原子指针保护。
	errHandler atomic.Pointer[func(err error)]
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
	// 默认空实现：读循环已按"预期断开 / 真异常"分级打印，且带上了 trace 与 ip。
	// 这里若再打印就只能是 nil ctx（拿不到 ctx），又会变成 trace=- 的无上下文 ERR。
	c.setErrHandler(nil)
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

// OnError 注册连接级错误钩子。
//
// 触发范围（刻意收窄）：
//   - 非预期关闭（IsUnexpectedCloseError 为 true 的那一支）
//   - 分帧/解码失败（receiveMessage 报错）
//
// 不触发：对端主动 close、EOF、读超时——它们是预期断开，走业务的 OnClose。
//
// 钩子运行在 readPump 协程上，因此：**不要阻塞**（会堵住这条连接的所有后续入站），
// **不要在钩子里 ch.Close() 或改动 bucket 成员**（readPump 的 defer 正在做清理，重入即竞态）。
// 传 nil 等价于恢复默认（静默）。
func (ch *WsChannelServer) OnError(f func(err error)) {
	ch.setErrHandler(f)
}

// setErrHandler 落库（nil 归一为空实现，调用点不必再判空）。
func (ch *WsChannelServer) setErrHandler(f func(err error)) {
	if f == nil {
		f = func(err error) {}
	}
	ch.errHandler.Store(&f)
}

// fireErr 触发连接级错误钩子；未注册时是空实现，零开销。
func (ch *WsChannelServer) fireErr(err error) {
	if h := ch.errHandler.Load(); h != nil && *h != nil {
		(*h)(err)
	}
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
