package wsocket

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v3/actions"
	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/decoder/fn"
	"github.com/w6xian/sloth/v3/internal/utils"
	"github.com/w6xian/sloth/v3/internal/utils/id"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/types/auth"
	"github.com/w6xian/sloth/v3/types/trpc"

	"github.com/gorilla/websocket"
)

// 服务器端对客户端的连接通道
// in fact, Channel it's a user Connect session
type WsChannelServer struct {
	Lock      sync.Mutex
	_room     *bucket.Room
	_next     bucket.IChannel
	_prev     bucket.IChannel
	broadcast chan *message.Msg
	_userId   int64
	_sign     string
	Conn      *websocket.Conn
	connTcp   *net.TCPConn
	Connect   trpc.ICallRpc
	rpcCaller chan []byte
	rpcBacker chan []byte
	rpcResult chan []byte

	pongTimeout    time.Duration
	writeWait      time.Duration
	readWait       time.Duration
	maxMessageSize int64
	// ping period default eq 54s
	pingPeriod time.Duration
	// error handler
	errHandler func(err error)
	// rpc_io 记录当前连接的rpc调用次数
	rpc_io atomic.Int64

	callObjPool sync.Pool
	backObjPool sync.Pool
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

	if ch.Conn != nil {
		ch.Conn.Close()
	}
	if ch.connTcp != nil {
		ch.connTcp.Close()
	}
	ch._userId = 0
	return nil
}

func NewWsChannelServer(connect trpc.ICallRpc, opts ...ChannelServerOption) (c *WsChannelServer) {
	c = new(WsChannelServer)
	c.Lock = sync.Mutex{}
	c.broadcast = make(chan *message.Msg, 10)
	c.rpcCaller = make(chan []byte, 10)
	c.rpcBacker = make(chan []byte, 10)
	c.rpcResult = make(chan []byte, 10)
	c.Next(nil)
	c.Prev(nil)
	c.pongTimeout = 54 * time.Second
	c.writeWait = 10 * time.Second
	c.readWait = 10 * time.Second
	c.maxMessageSize = 1024 * 1024
	c.pingPeriod = 54 * time.Second
	c._sign = ""
	c.Connect = connect
	c.errHandler = func(err error) {
		log.Println("Channel errHandler:", err.Error())
	}
	for _, opt := range opts {
		opt(c)
	}
	c.rpc_io.Store(0)
	c.callObjPool = sync.Pool{
		New: func() any {
			return &message.JsonCallObject{}
		},
	}
	c.backObjPool = sync.Pool{
		New: func() any {
			return &message.JsonBackObject{}
		},
	}
	return
}

func (ch *WsChannelServer) OnError(f func(err error)) {
	ch.errHandler = f
}

func (ch *WsChannelServer) Push(ctx context.Context, msg *message.Msg) (err error) {
	select {
	case ch.broadcast <- msg:
	case <-ctx.Done():
		return ctx.Err()
	}
	return
}

// @call ReplySuccess 回复调用成功
func (c *WsChannelServer) NetReply(id uint64, payload []byte, err error) error {
	if err != nil {
		return c.ReplyError(id, []byte(err.Error()))
	}
	return c.ReplySuccess(id, payload)
}

// @call ReplySuccess 回复调用成功
func (c *WsChannelServer) ReplySuccess(id uint64, data []byte) error {
	if c.Conn == nil {
		return fmt.Errorf("conn is nil")
	}

	payload, err := fn.Encode(actions.ACTION_REPLY_SUCCESS, id, data)
	if err != nil {
		return err
	}

	timer := time.NewTimer(c.writeWait)
	defer timer.Stop()
	select {
	case c.rpcBacker <- payload:
	case <-timer.C:
		return fmt.Errorf("rpc reply queue full")
	}
	return nil
}
func (c *WsChannelServer) ReplyError(id uint64, data []byte) error {
	if c.Conn == nil {
		return fmt.Errorf("conn is nil")
	}

	payload, err := fn.Encode(actions.ACTION_REPLY_ERROR, id, data)
	if err != nil {
		return err
	}

	timer := time.NewTimer(c.writeWait)
	defer timer.Stop()
	select {
	case c.rpcBacker <- payload:
	case <-timer.C:
		return fmt.Errorf("rpc reply queue full")
	}
	return nil
}

// 服务器调用客户端方法
func (ch *WsChannelServer) Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {
	ch.Lock.Lock()
	defer ch.Lock.Unlock()
	ticker := time.NewTicker(ch.writeWait)
	defer ticker.Stop()

	msg := getCallObj()
	msg.Header = header
	msg.Method = mtd
	msg.Args = args
	payload := utils.Serialize(msg)

	putCallObj(msg)
	callId := uint64(id.NextId(1))
	payload, err := fn.Encode(actions.ACTION_CALL, callId, payload)
	if err != nil {
		return nil, err
	}

	// 发送调用请求
	select {
	case <-ticker.C:
		return []byte{}, fmt.Errorf("call timeout")
	case ch.rpcCaller <- payload:
		ch.rpc_io.Add(1)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	ticker.Reset(ch.readWait)
	// 等待调用结果
	for {
		select {
		case <-ctx.Done():
			return []byte{}, ctx.Err()
		case <-ticker.C:
			return []byte{}, fmt.Errorf("reply timeout")
		case raw, ok := <-ch.rpcResult:
			if !ok {
				return []byte{}, fmt.Errorf("rpc result closed")
			}
			action, aerr := fn.Action(raw)
			if aerr != nil {
				return []byte{}, aerr
			}
			switch action {
			case actions.ACTION_REPLY_SUCCESS:
				if fn.Id(raw) != callId {
					continue
				}
				return fn.Data(raw), nil
			case actions.ACTION_REPLY_ERROR:
				if fn.Id(raw) != callId {
					continue
				}
				return []byte{}, errors.New(string(fn.Data(raw)))
			default:
				return []byte{}, fmt.Errorf("action not match")
			}
		}
	}
}

// 服务器调用客户端方法
func (ch *WsChannelServer) CallNet(ctx context.Context, msgId uint64, payload []byte) ([]byte, error) {
	ch.Lock.Lock()
	defer ch.Lock.Unlock()

	ticker := time.NewTicker(ch.writeWait)
	defer ticker.Stop()

	// 发送调用请求
	select {
	case <-ticker.C:
		return []byte{}, fmt.Errorf("call timeout")
	case ch.rpcCaller <- payload:
		ch.rpc_io.Add(1)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	ticker.Reset(ch.readWait)
	// 等待调用结果
	for {
		select {
		case <-ctx.Done():
			return []byte{}, ctx.Err()
		case <-ticker.C:
			return []byte{}, fmt.Errorf("reply timeout")
		case raw, ok := <-ch.rpcResult:
			if !ok {
				return []byte{}, fmt.Errorf("rpc result closed")
			}
			action, aerr := fn.Action(raw)
			if aerr != nil {
				return []byte{}, aerr
			}
			switch action {
			case actions.ACTION_REPLY_SUCCESS:
				if fn.Id(raw) != msgId {
					continue
				}
				return fn.Data(raw), nil
			case actions.ACTION_REPLY_ERROR:
				if fn.Id(raw) != msgId {
					continue
				}
				return []byte{}, errors.New(string(fn.Data(raw)))
			default:
				return []byte{}, fmt.Errorf("action not match")
			}
		}
	}
}
