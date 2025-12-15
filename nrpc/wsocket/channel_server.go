package wsocket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/w6xian/sloth/bucket"
	"github.com/w6xian/sloth/message"
	"github.com/w6xian/sloth/nrpc"

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

	rpcCaller chan *message.JsonCallObject
	rpcBacker chan *message.JsonBackObject

	pongTimeout    time.Duration
	writeWait      time.Duration
	readWait       time.Duration
	maxMessageSize int64
	// ping period default eq 54s
	pingPeriod time.Duration
	// error handler
	errHandler func(err error)
	// rpc_io 记录当前连接的rpc调用次数
	rpc_io int
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

// login 登录
func (ch *WsChannelServer) GetAuthInfo() *nrpc.AuthInfo {
	return &nrpc.AuthInfo{
		UserId: ch._userId,
		RoomId: ch._room.Id,
		Token:  ch._sign,
	}
}

func (ch *WsChannelServer) SetAuthInfo(auth *nrpc.AuthInfo) error {
	return errors.New("server not support set auth info")
}

// logout 登出
func (ch *WsChannelServer) Logout() {
	ch._userId = 0
}

func NewWsChannelServer(size int, opts ...ChannelServerOption) (c *WsChannelServer) {
	c = new(WsChannelServer)
	c.Lock = sync.Mutex{}
	c.broadcast = make(chan *message.Msg, size)
	c.rpcCaller = make(chan *message.JsonCallObject, 10)
	c.rpcBacker = make(chan *message.JsonBackObject, 10)
	c.Next(nil)
	c.Prev(nil)
	c.pongTimeout = 54 * time.Second
	c.writeWait = 10 * time.Second
	c.readWait = 10 * time.Second
	c.maxMessageSize = 1024 * 1024
	c.pingPeriod = 54 * time.Second
	c._sign = ""
	c.errHandler = func(err error) {
		fmt.Println("Channel errHandler:", err.Error())
	}
	for _, opt := range opts {
		opt(c)
	}
	c.rpc_io = 0
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

// @call ReplySuccess 回复调用成功
func (c *WsChannelServer) ReplySuccess(id string, data []byte) error {
	if c.Conn == nil {
		return fmt.Errorf("conn is nil")
	}

	// fmt.Println("ReplySuccess id:", id, data)
	msg := message.NewWsJsonBackSuccess(id, data)
	select {
	case c.rpcBacker <- msg:
	default:
	}
	return nil
}
func (c *WsChannelServer) ReplyError(id string, err []byte) error {
	if c.Conn == nil {
		return fmt.Errorf("conn is nil")
	}
	// fmt.Println("ReplyError id:", id, err)
	msg := message.NewWsJsonBackError(id, err)
	select {
	case c.rpcBacker <- msg:
	default:
	}
	return nil
}

// 服务器调用客户端方法
func (ch *WsChannelServer) Call(ctx context.Context, mtd string, args ...[]byte) ([]byte, error) {
	ch.Lock.Lock()
	defer ch.Lock.Unlock()
	ticker := time.NewTicker(ch.writeWait)
	defer ticker.Stop()
	// fmt.Println("Call args|||||||||:", args)
	msg := message.NewWsJsonCallObject(mtd, args...)
	// fmt.Println("Call msg------:", msg)
	// 发送调用请求
	select {
	case <-ticker.C:
		return []byte{}, fmt.Errorf("call timeout")
	case ch.rpcCaller <- msg:
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
		case back, ok := <-ch.rpcBacker:
			// fmt.Println("Call back------:", back, ok, back.Id, msg.Id)
			if back.Id == msg.Id && ok {
				if back.Error != "" {
					return []byte{}, errors.New(back.Error)
				}
				return back.Data, nil
			}
		}
	}
}
