package wsocket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/w6xian/sloth/group"
	"github.com/w6xian/sloth/message"

	"github.com/gorilla/websocket"
)

// in fact, Channel it's a user Connect session
type Channel struct {
	Lock      sync.Mutex
	_room     *group.Room
	_next     group.IChannel
	_prev     group.IChannel
	broadcast chan *message.Msg
	_userId   int64
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
}

func (ch *Channel) Next(n ...group.IChannel) group.IChannel {
	if len(n) > 0 {
		ch._next = n[0]
	}
	return ch._next
}

func (ch *Channel) Prev(p ...group.IChannel) group.IChannel {
	if len(p) > 0 {
		ch._prev = p[0]
	}
	return ch._prev
}
func (ch *Channel) Room(r ...*group.Room) *group.Room {
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

func NewChannel(size int, opts ...ChannelOption) (c *Channel) {
	c = new(Channel)
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
	c.errHandler = func(err error) {
		fmt.Println("Channel errHandler:", err.Error())
	}
	for _, opt := range opts {
		opt(c)
	}
	return
}

func (ch *Channel) OnError(f func(err error)) {
	ch.errHandler = f
}

func (ch *Channel) Push(ctx context.Context, msg *message.Msg) (err error) {
	select {
	case ch.broadcast <- msg:
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return
}

// @call ReplySuccess 回复调用成功
func (c *Channel) ReplySuccess(id uint64, data []byte) error {
	if c.Conn == nil {
		return fmt.Errorf("conn is nil")
	}

	// 判断data 是否是string类型
	// 如果是string类型，就设置msgType为TextMessage
	msgType := TextMessage

	msg := message.NewWsJsonBackSuccess(id, data, msgType)
	select {
	case c.rpcBacker <- msg:
	default:
	}
	return nil
}
func (c *Channel) ReplyError(id uint64, err []byte) error {
	if c.Conn == nil {
		return fmt.Errorf("conn is nil")
	}
	msg := message.NewWsJsonBackError(id, err)
	select {
	case c.rpcBacker <- msg:
	default:
	}
	return nil
}

// 服务器调用客户端方法
func (ch *Channel) Call(ctx context.Context, mtd string, args []byte) ([]byte, error) {
	ch.Lock.Lock()
	defer ch.Lock.Unlock()
	ticker := time.NewTicker(ch.writeWait)
	defer ticker.Stop()

	msg := message.NewWsJsonCallObject(mtd, args)

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
			if back.Id == msg.Id && ok {
				if back.Error != "" {
					return []byte{}, errors.New(back.Error)
				}
				return back.Data, nil
			}
		}
	}
}
