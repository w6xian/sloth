package nrpc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v4/actions"
	"github.com/w6xian/sloth/v4/decoder/fn"
	"github.com/w6xian/sloth/v4/internal/codec"
	"github.com/w6xian/sloth/v4/internal/errs"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/types/trpc"
)

type RpcChannel struct {
	// 客户端的用户ID
	UserId int64
	// 在服务器中哪个房间
	RoomId int64
	//Sign 登录签名
	Sign string
	// PRpcCaller 待发送的 RPC 请求帧，由 writePump 消费
	PRpcCaller chan []byte
	// PRpcBacker 待发送的 RPC 回包帧，由 writePump 消费
	PRpcBacker chan []byte
	// PRpcResult 已废弃的共享回包队列：回包现在按 callId 直接分发给等待者
	// （见 SendData/Receive）。保留字段仅为兼容旧引用，框架不再读写它。
	PRpcResult     chan []byte
	PSend          chan *message.Msg
	Lock           sync.Mutex
	Connect        trpc.ICallRpc
	PDefaultHeader message.Header
	PAddr          string
	PPort          int64
	// writeWait default eq 10s
	PWriteWait time.Duration
	// readWait default eq 10s
	PReadWait time.Duration

	// pending 是本连接上"已发出、等回包"的调用表：callId → 容量 1 的回包通道。
	// 有了 per-call 通道，多个调用互不干扰，SendData 不再需要全程加锁——
	// 这是同连接并发的前提（原实现所有调用共用一个回包队列，必须串行）。
	pending   map[uint64]chan []byte
	pendingMu sync.Mutex
	// nextCallId 连接内自增的调用号。callId 只用于本连接内匹配回包，
	// 本就不需要全局唯一；原实现走 snowflake，为它付了全局锁的代价。
	nextCallId atomic.Uint64
	// callDone 连接关闭时关闭，用于唤醒所有仍在等回包的调用（避免干等到超时）
	callDone     chan struct{}
	callDoneOnce sync.Once
}

func (c *RpcChannel) DefaultHeader() message.Header {
	return c.PDefaultHeader
}

// Call 客户端 调用远程方法 同步调用
func (cc *RpcChannel) Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {

	msg := message.GetCallJCO()
	msg.Header = header
	msg.Method = mtd
	msg.Args = args
	// 手写 JSON 编码（零反射），等价于 json.Marshal(msg)
	payload := msg.MarshalJSONFast()
	message.PutCallJCO(msg)

	// callId 只需在本连接内唯一（回包按它在 pending 表里匹配），
	// 连接内自增即可：比 snowflake 少一次全局锁，也不会撞号。
	callId := cc.nextCallId.Add(1)
	co := codec.UseCodec(codec.CODEC_CODER_FN)
	payload, err := co.Encode(actions.ACTION_CALL, callId, payload)
	if err != nil {
		return nil, err
	}
	return cc.SendData(ctx, callId, payload)

}

// InitCalls 初始化 per-call 回包分发表，必须在 channel 构造函数中调用一次。
// 导出是因为嵌入 RpcChannel 的 WsChannelServer/WsChannelClient 在另一个包里。
func (c *RpcChannel) InitCalls() {
	if c.pending == nil {
		c.pending = make(map[uint64]chan []byte, 16)
	}
	if c.callDone == nil {
		c.callDone = make(chan struct{})
	}
}

// CloseCalls 唤醒并清空所有等待中的调用，幂等。
// 连接关闭时必须调用：否则等待者只能干等到超时（默认 10s）才返回。
func (c *RpcChannel) CloseCalls() {
	c.callDoneOnce.Do(func() {
		if c.callDone != nil {
			close(c.callDone)
		}
	})
	c.pendingMu.Lock()
	for id, ch := range c.pending {
		delete(c.pending, id)
		close(ch)
	}
	c.pendingMu.Unlock()
}

func (c *RpcChannel) addPending(id uint64, reply chan []byte) error {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	// 连接已关闭：不再接受新调用，直接快速失败
	select {
	case <-c.callDone:
		return fmt.Errorf("connection closed: %w", errs.ErrConnClosed)
	default:
	}
	c.pending[id] = reply
	return nil
}

func (c *RpcChannel) removePending(id uint64) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

// deliverReply 把回包交给对应的等待者，返回是否有人接。
// 投递是非阻塞的（通道容量 1，且只有该调用会读）：
// 接不到说明调用已超时/被取消，或这是一条重复回包。
func (c *RpcChannel) deliverReply(id uint64, payload []byte) bool {
	if c.pending == nil {
		return false
	}
	c.pendingMu.Lock()
	reply, ok := c.pending[id]
	c.pendingMu.Unlock()
	if !ok {
		return false
	}
	select {
	case reply <- payload:
		return true
	default:
		return false
	}
}

// SendData 发出一次 RPC 并等待它的回包。
//
// 原实现全程持 cc.Lock，且所有调用共享 PRpcResult 一个回包队列：并发时调用者
// 会读到别人的回包（只能丢弃再等），所以当初必须串行加锁——同一条连接上所有
// 调用严格串行，QPS 被锁死在 1/RTT（浏览器只有一条 ws 连接时尤其致命）。
//
// 现在每个调用在 pending 里有自己的回包通道：
//   - 不持锁，同连接可并发任意多个调用，QPS 从 1/RTT 变为 N/RTT；
//   - 回包直达等待者，不再排队等 readPump 消费，消除队头阻塞。
func (cc *RpcChannel) SendData(ctx context.Context, msgId uint64, payload []byte) ([]byte, error) {
	if cc.pending == nil {
		return nil, fmt.Errorf("rpc channel not initialized: %w", errs.ErrConnClosed)
	}
	reply := make(chan []byte, 1)
	if err := cc.addPending(msgId, reply); err != nil {
		return nil, err
	}
	defer cc.removePending(msgId)

	// 用 Timer 而非 Ticker：Ticker 通道会缓存到期信号，Reset 不排空它，
	// 写入阶段遗留的信号会让等待阶段立刻"假超时"。
	writeTimeout := timeoutOr(cc.PWriteWait)
	timer := time.NewTimer(writeTimeout)
	select {
	case cc.PRpcCaller <- payload:
	case <-ctx.Done():
		timer.Stop()
		return nil, ctx.Err()
	case <-cc.callDone:
		timer.Stop()
		return nil, fmt.Errorf("connection closed: %w", errs.ErrConnClosed)
	case <-timer.C:
		return nil, fmt.Errorf("call timeout: %w after %s", errs.ErrTimeout, writeTimeout)
	}
	// 复用 timer 前必须排空已到期的信号（Stop 返回 false 表示信号已发出/已被取走）
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	replyTimeout := timeoutOr(cc.PReadWait)
	timer.Reset(replyTimeout)
	select {
	case raw := <-reply:
		timer.Stop()
		return parseReply(raw)
	case <-ctx.Done():
		timer.Stop()
		return nil, ctx.Err()
	case <-cc.callDone:
		timer.Stop()
		return nil, fmt.Errorf("connection closed: %w", errs.ErrConnClosed)
	case <-timer.C:
		return nil, fmt.Errorf("reply timeout: %w after %s", errs.ErrTimeout, replyTimeout)
	}
}

// parseReply 拆回包帧：成功帧返回数据，错误帧转成 error。
func parseReply(raw []byte) ([]byte, error) {
	action, err := fn.Action(raw)
	if err != nil {
		return nil, err
	}
	switch action {
	case actions.ACTION_REPLY_SUCCESS:
		return fn.Data(raw), nil
	case actions.ACTION_REPLY_ERROR:
		return nil, errors.New(string(fn.Data(raw)))
	default:
		return nil, fmt.Errorf("action not match")
	}
}

// timeoutOr 超时配置为 0（未设置）时的兜底值，避免 timer 立即触发把正常调用判成超时。
func timeoutOr(d time.Duration) time.Duration {
	if d <= 0 {
		return defaultTimeout
	}
	return d
}

// @Reply
func (c *RpcChannel) Send(ctx context.Context, id uint64, payload []byte, err error) error {
	if err != nil {
		return c.channel_result(ctx, actions.ACTION_REPLY_ERROR, id, []byte(err.Error()))
	}
	return c.channel_result(ctx, actions.ACTION_REPLY_SUCCESS, id, payload)
}

// Receive 收到一个 RPC 回包帧，按 callId 分发给对应的等待者。
//
// 原实现把回包塞进共享队列 PRpcResult，队列满时 readPump 会被阻塞长达
// PWriteWait(10s)——一个慢调用就能卡死整条连接的入站（队头阻塞）。
// 现在回包直达等待者（容量 1 的非阻塞投递），Receive 永不阻塞 readPump。
func (c *RpcChannel) Receive(ctx context.Context, payload []byte) error {
	action, id, _, err := fn.Decode(payload)
	if err != nil {
		return err
	}
	if action != actions.ACTION_REPLY_SUCCESS && action != actions.ACTION_REPLY_ERROR {
		return fmt.Errorf("unexpected action %d in reply frame", action)
	}
	if !c.deliverReply(id, payload) {
		// 找不到等待者：调用已超时/被取消，或这是一条重复回包。
		// 只计数不报错——这是对端行为，不该让本端处理流程失败。
		rpcOrphanReplies.Inc()
	}
	return nil
}

func (c *RpcChannel) channel_result(ctx context.Context, action byte, id uint64, data []byte) error {

	co := codec.UseCodec(codec.CODEC_CODER_FN)
	payload, err := co.Encode(action, id, data)
	if err != nil {
		return err
	}
	timer := time.NewTimer(c.PWriteWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case c.PRpcBacker <- payload:
	case <-timer.C:
		return fmt.Errorf("rpc reply queue full: %w", errs.ErrQueueFull)
	}
	return nil
}

type RpcConn struct {
	Connect         trpc.ICallRpc
	WriteWait       time.Duration
	ReadWait        time.Duration
	PongWait        time.Duration
	PingPeriod      time.Duration
	MaxMessageSize  int64
	ReadBufferSize  int
	WriteBufferSize int
	BroadcastSize   int
	SliceSize       int64
	Header          map[string]string
	KeepAlive       bool
	Codec           codec.Codec
}

func (rc *RpcConn) SetCodec(c codec.Codec) {
	rc.Codec = c
}
