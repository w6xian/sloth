package wsocket

import (
	"context"
	"errors"
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

	// writeWait default eq 10s
	writeWait time.Duration
	// readWait default eq 10s
	readWait time.Duration
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
	c.writeWait = 10 * time.Second
	c.readWait = 10 * time.Second
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

func (c *WsClient) Push(ctx context.Context, msg *message.Msg) (err error) {
	if c.conn == nil {
		return
	}

	select {
	case c.send <- msg:
	default:
	}
	return
}

func (c *WsClient) ReplySuccess(id uint64, data []byte) error {
	if c.conn == nil {
		return fmt.Errorf("conn is nil")
	}
	msgType := TextMessage
	msg := message.NewWsJsonBackSuccess(id, data, msgType)
	select {
	case c.rpcBacker <- msg:
	default:
	}
	return nil
}
func (c *WsClient) ReplyError(id uint64, err []byte) error {
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

// Call 客户端 调用远程方法
func (ch *WsClient) Call(ctx context.Context, mtd string, args []byte) ([]byte, error) {
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
			// fmt.Println("client call ticker.C:", ticker.C)
			return []byte{}, fmt.Errorf("reply timeout")
		case back, ok := <-ch.rpcBacker:
			// fmt.Println("client call back:", back.Id, msg.Id, back.Type, ok)
			if back.Id == msg.Id && ok {
				if back.Error != "" {
					return []byte(""), errors.New(back.Error)
				}
				return back.Data, nil
			}
			return []byte{}, fmt.Errorf("unknown message type")

		}
	}
}
