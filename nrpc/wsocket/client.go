package wsocket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sync"
	"time"

	"github.com/w6xian/sloth/decoder/tlv"
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
func (ch *WsClient) Call(ctx context.Context, mtd string, args any) ([]byte, error) {
	ch.Lock.Lock()
	defer ch.Lock.Unlock()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var msg *message.JsonCallObject
	switch reflect.TypeOf(args).Kind() {
	case reflect.Slice:
		if data, ok := args.(tlv.TLVFrame); ok {
			msg = message.NewWsJsonCallObject(mtd, data)
			break
		}
		msg = message.NewWsJsonCallObject(mtd, args.([]byte))
	case reflect.String:
		msg = message.NewWsJsonCallObject(mtd, []byte(args.(string)))
	default:
		msg = message.NewWsJsonCallObject(mtd, serialize(args))
	}
	// 发送调用请求
	select {
	case <-ticker.C:
		return []byte{}, fmt.Errorf("call timeout")
	case ch.rpcCaller <- msg:
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	// fmt.Println("client call 107:", msg.Id)
	ticker.Reset(5 * time.Second)
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
			switch back.Type {
			case message.TextMessage:
				if back.Id == msg.Id && ok {
					if back.Error != "" {
						return []byte(""), errors.New(back.Error)
					}
					gettype := reflect.TypeOf(back.Data)

					//switch gettype.Kind() {
					switch gettype.Kind() {
					case reflect.String:
						return back.Data, nil
					case reflect.Slice:
						// []byte 64转字符串 json
						return back.Data, nil
					}
					return []byte{}, fmt.Errorf("unknown data type")
				}
			case message.BinaryMessage:
				if back.Id == msg.Id && ok {
					return back.Data, nil
				}
			}
			return []byte{}, fmt.Errorf("unknown message type")

		}
	}
}

func (ch *WsClient) CallBin(ctx context.Context, mtd string, args []byte) ([]byte, error) {
	ch.Lock.Lock()
	defer ch.Lock.Unlock()
	ticker := time.NewTicker(5 * time.Second)
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
	// fmt.Println("client call 107:", msg.Id)
	ticker.Reset(5 * time.Second)
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
			switch back.Type {
			case message.TextMessage:
				if back.Id == msg.Id && ok {
					if back.Error != "" {
						return []byte(""), errors.New(back.Error)
					}
					gettype := reflect.TypeOf(back.Data)

					//switch gettype.Kind() {
					switch gettype.Kind() {
					case reflect.String:
						return back.Data, nil
					case reflect.Slice:
						// []byte 64转字符串 json
						return back.Data, nil
					}
					return []byte{}, fmt.Errorf("unknown data type")
				}
			case message.BinaryMessage:
				if back.Id == msg.Id && ok {
					return back.Data, nil
				}
			}
			return []byte{}, fmt.Errorf("unknown message type")

		}
	}
}
