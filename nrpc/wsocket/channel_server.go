package wsocket

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/w6xian/sloth/v3/bucket"
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
	ch._userId = 0
	ch.PAddr = ""
	ch.PPort = 0
	ch.Sign = ""
	return nil
}

func NewWsChannelServer(connect trpc.ICallRpc, opts ...ChannelServerOption) (c *WsChannelServer) {
	c = new(WsChannelServer)
	c.Lock = sync.Mutex{}
	c.broadcast = make(chan *message.Msg, 10)
	c.PRpcCaller = make(chan []byte, 10)
	c.PRpcBacker = make(chan []byte, 10)
	c.PRpcResult = make(chan []byte, 10)
	c.Next(nil)
	c.Prev(nil)
	c.pongTimeout = 54 * time.Second
	c.PWriteWait = 10 * time.Second
	c.PReadWait = 10 * time.Second
	c._sign = ""
	c.Connect = connect
	c.errHandler = func(err error) {
		log.Println("Channel errHandler:", err.Error())
	}
	for _, opt := range opts {
		opt(c)
	}
	return
}

func (ch *WsChannelServer) OnError(f func(err error)) {
	ch.errHandler = f
}

func (ch *WsChannelServer) Push(ctx context.Context, msg *message.Msg) (err error) {
	timer := time.NewTimer(ch.PWriteWait)
	select {
	case ch.broadcast <- msg:
	case <-timer.C:
		return fmt.Errorf("rpc reply queue full")
	case <-ctx.Done():
		return ctx.Err()
	}
	return
}
