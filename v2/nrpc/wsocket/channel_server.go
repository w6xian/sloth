package wsocket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v2/actions"
	"github.com/w6xian/sloth/v2/bucket"
	"github.com/w6xian/sloth/v2/decoder"
	"github.com/w6xian/sloth/v2/internal/logger"
	"github.com/w6xian/sloth/v2/internal/utils"
	"github.com/w6xian/sloth/v2/message"
	"github.com/w6xian/sloth/v2/types/auth"
	"github.com/w6xian/sloth/v2/types/trpc"

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
	releaseConn func()
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
	rpc_io int64

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
	if ch.releaseConn != nil {
		ch.releaseConn()
		ch.releaseConn = nil
	}
	if ch.Conn != nil {
		ch.Conn.Close()
	}
	if ch.connTcp != nil {
		ch.connTcp.Close()
	}
	ch._userId = 0
	return nil
}

func (s *WsChannelServer) log(level logger.LogLevel, line string, args ...any) {
	if s.Connect == nil {
		fmt.Println("WsServer Connect is nil")
		return
	}
	s.Connect.Log(level, "[WsChannelServer]"+line, args...)
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
		fmt.Println("Channel errHandler:", err.Error())
	}
	for _, opt := range opts {
		opt(c)
	}
	atomic.StoreInt64(&c.rpc_io, 0)
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
	default:
	}
	return
}

func (c *WsChannelServer) getCallObj() *message.JsonCallObject {
	req := c.callObjPool.Get()
	if req == nil {
		return &message.JsonCallObject{}
	}
	return req.(*message.JsonCallObject)
}

func (c *WsChannelServer) putCallObj(req *message.JsonCallObject) {
	if req == nil {
		return
	}
	req.Id = ""
	req.Action = 0
	req.Type = 0
	req.Header = nil
	req.Method = ""
	req.Data = nil
	req.Error = ""
	req.Args = nil
	c.callObjPool.Put(req)
}

func (c *WsChannelServer) getBackObj() *message.JsonBackObject {
	req := c.backObjPool.Get()
	if req == nil {
		return &message.JsonBackObject{}
	}
	return req.(*message.JsonBackObject)
}

func (c *WsChannelServer) putBackObj(req *message.JsonBackObject) {
	if req == nil {
		return
	}
	req.Context = nil
	req.Id = ""
	req.Type = 0
	req.Header = nil
	req.Action = 0
	req.Data = nil
	req.Error = ""
	req.Args = nil
	c.backObjPool.Put(req)
}

// @call ReplySuccess 回复调用成功
func (c *WsChannelServer) ReplySuccess(id string, data []byte) error {
	if c.Conn == nil {
		return fmt.Errorf("conn is nil")
	}

	msg := c.getBackObj()
	msg.Id = id
	msg.Action = actions.ACTION_REPLY
	msg.Type = message.TextMessage
	msg.Data = data
	msg.Error = ""
	msg.Header = nil
	msg.Args = nil

	payload := utils.Serialize(msg)
	c.putBackObj(msg)

	select {
	case c.rpcBacker <- payload:
	default:
	}
	return nil
}
func (c *WsChannelServer) ReplyError(id string, err []byte) error {
	if c.Conn == nil {
		return fmt.Errorf("conn is nil")
	}

	msg := c.getBackObj()
	msg.Id = id
	msg.Action = actions.ACTION_REPLY
	msg.Type = message.TextMessage
	msg.Data = nil
	if err != nil {
		msg.Error = string(err)
	} else {
		msg.Error = ""
	}
	msg.Header = nil
	msg.Args = nil

	payload := utils.Serialize(msg)
	c.putBackObj(msg)

	select {
	case c.rpcBacker <- payload:
	default:
	}
	return nil
}

// 服务器调用客户端方法
func (ch *WsChannelServer) Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {

	// fmt.Println("--------------channel_server.go")
	ch.Lock.Lock()
	defer ch.Lock.Unlock()
	ch.log(logger.Debug, "Call mtd:%s, args:%v", mtd, args)
	ticker := time.NewTicker(ch.writeWait)
	defer ticker.Stop()

	msg := ch.getCallObj()
	msg.Id = fmt.Sprintf("%d", decoder.NextId())
	msg.Action = actions.ACTION_CALL
	msg.Type = message.TextMessage
	msg.Header = header
	msg.Method = mtd
	if len(args) > 0 {
		msg.Data = args[0]
	}
	if len(args) > 1 {
		msg.Args = args[1:]
	}
	payload := utils.Serialize(msg)
	callId := msg.Id
	ch.putCallObj(msg)

	// 发送调用请求
	select {
	case <-ticker.C:
		return []byte{}, fmt.Errorf("call timeout")
	case ch.rpcCaller <- payload:
		atomic.AddInt64(&ch.rpc_io, 1)
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
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
			back := ch.getBackObj()
			if err := utils.Deserialize(raw, back); err != nil {
				ch.putBackObj(back)
				return []byte{}, err
			}
			if back.Id != callId {
				ch.putBackObj(back)
				continue
			}
			if back.Error != "" {
				errStr := back.Error
				ch.putBackObj(back)
				return []byte{}, errors.New(errStr)
			}
			data := back.Data
			ch.putBackObj(back)
			return data, nil
		}
	}
}
