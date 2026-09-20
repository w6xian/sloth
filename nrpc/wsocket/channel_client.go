package wsocket

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v3/internal/errs"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/nrpc"
	"github.com/w6xian/sloth/v3/types/auth"
	"github.com/w6xian/sloth/v3/types/trpc"

	"github.com/gorilla/websocket"
)

// 客户端对服务器的连接通道
// in fact, Client it's a user Connect session
type WsChannelClient struct {
	nrpc.RpcChannel
	// Conn 由 connMu 保护：closeConn 会在 readPump/writePump 的 defer 中把它置 nil，
	// 而这两个协程本身又在读它，无保护即数据竞争（go test -race 可复现）。
	Conn   *websocket.Conn
	connMu sync.RWMutex
	// closeOnce 保证底层连接只被关闭一次：
	// readPump/writePump 两个 goroutine 的 defer 与外部 Close 可能并发触发
	closeOnce sync.Once
	closed    atomic.Bool
	// sliceSeq 本连接的分片序号，仅由 writePump 协程访问（无需加锁）
	sliceSeq uint32
	// queueSize 本连接各队列的容量（可配置，见 WithClientQueueSize）
	queueSize int
	// onQueueFull 队列满时的观测钩子（可为空），用于统计被背压丢弃的投递
	onQueueFull func()
}

// queueFull 报告一次"因队列满而投递失败"，供上层观测背压强度。
func (c *WsChannelClient) queueFull() {
	if c.onQueueFull != nil {
		c.onQueueFull()
	}
}

// getConn 并发安全地取当前底层连接（已关闭则为 nil）。
func (c *WsChannelClient) getConn() *websocket.Conn {
	c.connMu.RLock()
	conn := c.Conn
	c.connMu.RUnlock()
	return conn
}

// closeConn 幂等地关闭底层 WebSocket 连接（并发安全）
func (c *WsChannelClient) closeConn() {
	c.closeOnce.Do(func() {
		c.connMu.Lock()
		conn := c.Conn
		c.Conn = nil
		c.connMu.Unlock()
		c.closed.Store(true)
		if conn != nil {
			_ = conn.Close()
		}
	})
}

// IsClosed 报告底层连接是否已关闭（并发安全），供调用方做快速失败。
func (c *WsChannelClient) IsClosed() bool {
	return c.closed.Load()
}

// nextSliceName 返回本连接的下一个分片名（00-99 循环）。
//
// 原先使用全局计数器 + fmt.Sprintf：多核下共享一个 int32 造成原子竞争，
// 且每次发送都要分配字符串。分片名只用于同一条连接内连续分片的匹配，
// writePump 串行发送，因此连接内自增完全够用，且零分配、无竞争。
func (c *WsChannelClient) nextSliceName() string {
	c.sliceSeq++
	if c.sliceSeq > 99 {
		c.sliceSeq = 0
	}
	return sliceNames[c.sliceSeq]
}

func NewWsChannelClient(connect trpc.ICallRpc, opts ...ChannelClientOption) (c *WsChannelClient) {
	c = new(WsChannelClient)
	c.Lock = sync.Mutex{}
	c.queueSize = defaultChannelQueueSize
	c.UserId = 0
	c.Conn = nil
	c.PWriteWait = 10 * time.Second
	c.PReadWait = 10 * time.Second
	c.Sign = ""
	c.Connect = connect
	c.InitCalls() // per-call 回包分发表（SendData 依赖，未初始化会直接失败）
	c.PDefaultHeader = message.Header{}
	for _, opt := range opts {
		opt(c)
	}
	// 队列按最终配置创建（option 可能改过 queueSize / 注入了队列满钩子）
	c.PSend = make(chan *message.Msg, c.queueSize)
	c.PRpcCaller = make(chan []byte, c.queueSize)
	c.PRpcBacker = make(chan []byte, c.queueSize)
	c.PRpcResult = make(chan []byte, c.queueSize) // 已废弃：回包走 pending 分发
	return
}

func (c *WsChannelClient) Logout() (err error) {
	c.RoomId = 0
	c.UserId = 0
	c.Sign = ""
	c.PAddr = ""
	c.PPort = 0
	return
}
func (c *WsChannelClient) Close() error {
	c.Lock.Lock()
	defer c.Lock.Unlock()
	// 唤醒所有还在等回包的调用：否则它们要干等到超时（默认 10s）才返回
	c.CloseCalls()
	c.closeConn()
	c.UserId = 0
	c.RoomId = 0
	c.PAddr = ""
	c.PPort = 0
	c.Sign = ""
	return nil
}

// Push 客户端 发送消息到服务器
func (c *WsChannelClient) Push(ctx context.Context, msg *message.Msg) (err error) {
	// timer 必须 Stop：否则每次调用都会残留一个 10s 定时器，
	// 高频推送下 runtime 定时器堆会持续膨胀（原实现泄漏）。
	timer := time.NewTimer(c.PWriteWait)
	defer timer.Stop()
	select {
	case c.PSend <- msg:
	case <-timer.C:
		c.queueFull()
		return fmt.Errorf("rpc reply queue full: %w", errs.ErrQueueFull)
	case <-ctx.Done():
		return ctx.Err()
	}
	return
}

// login 登录
func (ch *WsChannelClient) GetAuthInfo() (*auth.AuthInfo, error) {
	return &auth.AuthInfo{
		UserId: ch.UserId,
		RoomId: ch.RoomId,
		Token:  ch.Sign,
	}, nil
}

func (ch *WsChannelClient) SetAuthInfo(auth *auth.AuthInfo) error {
	if auth == nil {
		return errors.New("auth is nil")
	}
	ch.UserId = auth.UserId
	ch.RoomId = auth.RoomId
	ch.Sign = auth.Token
	return nil
}

// types.IConnInfo
func (ch *WsChannelClient) GetUserId() int64 {
	return ch.UserId
}
func (ch *WsChannelClient) GetRoomId() int64 {
	return ch.RoomId
}
