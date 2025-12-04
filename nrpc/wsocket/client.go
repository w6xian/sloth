package wsocket

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/w6xian/sloth/message"

	"github.com/gorilla/websocket"
)

// in fact, Client it's a user Connect session
type WsClient struct {
	send      chan *message.Msg
	rpcCaller chan *message.JsonCallObject
	rpcBacker chan *message.JsonBackObject

	UserId  int64
	RoomId  int64
	conn    *websocket.Conn
	connTcp *net.TCPConn
	Lock    sync.Mutex
}

func NewWsClient(userId int64, size int) (c *WsClient) {
	c = new(WsClient)
	c.Lock = sync.Mutex{}
	c.send = make(chan *message.Msg, size)
	c.rpcCaller = make(chan *message.JsonCallObject, size)
	c.rpcBacker = make(chan *message.JsonBackObject, size)
	c.UserId = userId
	c.conn = nil
	c.connTcp = nil
	return
}
func (c *WsClient) Login(roomId int64, userId int64) (err error) {
	c.Lock.Lock()
	defer c.Lock.Unlock()
	c.RoomId = roomId
	c.UserId = userId
	return
}

func (c *WsClient) Logout() (err error) {
	c.RoomId = 0
	return
}

func (c *WsClient) Push(msg *message.Msg) (err error) {
	if c.conn == nil {
		return
	}

	select {
	case c.send <- msg:
	default:
	}
	return
}

func (c *WsClient) ReplySuccess(id string, data []byte) error {
	if c.conn == nil {
		return fmt.Errorf("conn is nil")
	}
	msg := message.NewWsJsonBackSuccess(id, data)
	select {
	case c.rpcBacker <- msg:
	default:
	}
	return nil
}
func (c *WsClient) ReplyError(id string, err []byte) error {
	if c.conn == nil {
		return fmt.Errorf("conn is nil")
	}
	msg := message.NewWsJsonBackError(id, err)
	select {
	case c.rpcBacker <- msg:
	default:
	}
	return nil
}

func (ch *WsClient) Call(ctx context.Context, mtd string, args any) ([]byte, error) {
	ch.Lock.Lock()
	defer ch.Lock.Unlock()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	msg := message.NewWsJsonCallObject(mtd, serialize(args))
	// 发送调用请求
	select {
	case <-ticker.C:
		return []byte{}, fmt.Errorf("call timeout")
	case ch.rpcCaller <- msg:
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	ticker.Reset(5 * time.Second)
	// 等待调用结果
	for {
		select {
		case <-ticker.C:
			return []byte{}, fmt.Errorf("reply timeout")
		case back, ok := <-ch.rpcBacker:
			if back.Id == msg.Id && ok {
				if back.Error != "" {
					return []byte{}, fmt.Errorf(back.Error)
				}
				return []byte(back.Data), nil
			}
		case <-ctx.Done():
			return []byte{}, ctx.Err()
		}
	}
}
