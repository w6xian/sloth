package wsocket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/w6xian/sloth/internal/logger"
	"github.com/w6xian/sloth/message"
	"github.com/w6xian/sloth/nrpc"

	"github.com/gorilla/websocket"
)

// 客户端对服务器的连接通道
// in fact, Client it's a user Connect session
type WsChannelClient struct {
	send      chan *message.Msg
	rpcCaller chan *message.JsonCallObject
	rpcBacker chan *message.JsonBackObject
	Connect   nrpc.ICallRpc

	// 客户端的用户ID
	UserId int64
	// 在服务器中哪个房间
	RoomId int64
	//Sign 登录签名
	Sign    string
	conn    *websocket.Conn
	connTcp *net.TCPConn
	Lock    sync.Mutex

	// writeWait default eq 10s
	writeWait time.Duration
	// readWait default eq 10s
	readWait time.Duration
	// func
	rpc_io int
}

func NewWsChannelClient(connect nrpc.ICallRpc, opts ...ChannelClientOption) (c *WsChannelClient) {
	c = new(WsChannelClient)
	c.Lock = sync.Mutex{}
	c.send = make(chan *message.Msg, 5)
	c.rpcCaller = make(chan *message.JsonCallObject, 10)
	c.rpcBacker = make(chan *message.JsonBackObject, 10)
	c.UserId = 0
	c.conn = nil
	c.connTcp = nil
	c.writeWait = 10 * time.Second
	c.readWait = 10 * time.Second
	c.Sign = ""
	for _, opt := range opts {
		opt(c)
	}
	c.rpc_io = 0
	return
}
func (s *WsChannelClient) log(level logger.LogLevel, line string, args ...any) {
	s.Connect.Log(level, "[WsChannelClient]"+line, args...)
}

func (c *WsChannelClient) Logout() (err error) {
	c.RoomId = 0
	c.UserId = 0
	c.Sign = ""
	return
}

// Push 客户端 发送消息到服务器
func (c *WsChannelClient) Push(ctx context.Context, msg *message.Msg) (err error) {
	if c.conn == nil {
		return
	}

	select {
	case c.send <- msg:
	default:
	}
	return
}

func (c *WsChannelClient) ReplySuccess(id string, data []byte) error {
	if c.conn == nil {
		return fmt.Errorf("conn is nil")
	}
	// fmt.Println("ReplySuccess WsClient id:", id, data)
	msg := message.NewWsJsonBackSuccess(id, data)
	select {
	case c.rpcBacker <- msg:
	default:
	}
	return nil
}
func (c *WsChannelClient) ReplyError(id string, err []byte) error {
	if c.conn == nil {
		return fmt.Errorf("conn is nil")
	}
	// fmt.Println("ReplyError WsChannelClient id:", id, err)
	msg := message.NewWsJsonBackError(id, err)
	select {
	case c.rpcBacker <- msg:
	default:
	}
	return nil
}

// login 登录
func (ch *WsChannelClient) GetAuthInfo() *nrpc.AuthInfo {
	return &nrpc.AuthInfo{
		UserId: ch.UserId,
		RoomId: ch.RoomId,
		Token:  ch.Sign,
	}
}

func (ch *WsChannelClient) SetAuthInfo(auth *nrpc.AuthInfo) error {
	if auth == nil {
		return errors.New("auth is nil")
	}
	ch.UserId = auth.UserId
	ch.RoomId = auth.RoomId
	ch.Sign = auth.Token
	return nil
}

// Call 客户端 调用远程方法
func (ch *WsChannelClient) Call(ctx context.Context, mtd string, args ...[]byte) ([]byte, error) {
	ch.Lock.Lock()
	defer ch.Lock.Unlock()
	ticker := time.NewTicker(ch.writeWait)
	defer ticker.Stop()
	msg := message.NewWsJsonCallObject(mtd, args...)
	// 发送调用请求
	ch.log(logger.Debug, "Call WsClient------: %s", msg)
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
				// fmt.Println("client call back.Error:", back.Id, msg.Id, back.Error)
				if back.Error != "" {
					return []byte(""), errors.New(back.Error)
				}
				return back.Data, nil
			}
			return []byte{}, fmt.Errorf("unknown message type")

		}
	}
}
