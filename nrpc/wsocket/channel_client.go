package wsocket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v3/actions"
	"github.com/w6xian/sloth/v3/decoder/fn"
	"github.com/w6xian/sloth/v3/internal/utils"
	"github.com/w6xian/sloth/v3/internal/utils/id"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/types/auth"
	"github.com/w6xian/sloth/v3/types/trpc"

	"github.com/gorilla/websocket"
)

// 客户端对服务器的连接通道
// in fact, Client it's a user Connect session
type WsChannelClient struct {
	send          chan *message.Msg
	rpcCaller     chan []byte
	rpcBacker     chan []byte
	rpcResult     chan []byte
	Connect       trpc.ICallRpc
	defaultHeader message.Header

	// 客户端的用户ID
	UserId int64
	// 在服务器中哪个房间
	RoomId int64
	//Sign 登录签名
	Sign    string
	conn    *websocket.Conn
	connTcp *net.TCPConn
	Lock    sync.Mutex
	addr    string
	port    int64
	// writeWait default eq 10s
	writeWait time.Duration
	// readWait default eq 10s
	readWait time.Duration
	// func
	rpc_io atomic.Int64
}

func NewWsChannelClient(connect trpc.ICallRpc, opts ...ChannelClientOption) (c *WsChannelClient) {
	c = new(WsChannelClient)
	c.Lock = sync.Mutex{}
	c.send = make(chan *message.Msg, 5)
	c.rpcCaller = make(chan []byte, 10)
	c.rpcBacker = make(chan []byte, 10)
	c.rpcResult = make(chan []byte, 10)
	c.UserId = 0
	c.conn = nil
	c.connTcp = nil
	c.writeWait = 10 * time.Second
	c.readWait = 10 * time.Second
	c.Sign = ""
	c.Connect = connect
	c.defaultHeader = message.Header{}
	for _, opt := range opts {
		opt(c)
	}
	return
}

func (c *WsChannelClient) DefaultHeader() message.Header {
	return c.defaultHeader
}

func (c *WsChannelClient) Logout() (err error) {
	c.RoomId = 0
	c.UserId = 0
	c.Sign = ""
	c.addr = ""
	c.port = 0
	return
}
func (c *WsChannelClient) Close() error {
	c.Lock.Lock()
	defer c.Lock.Unlock()
	if c.conn != nil {
		c.conn.Close()
	}
	if c.connTcp != nil {
		c.connTcp.Close()
	}
	c.UserId = 0
	c.RoomId = 0
	c.addr = ""
	c.port = 0
	c.Sign = ""
	return nil
}

// Push 客户端 发送消息到服务器
func (c *WsChannelClient) Push(ctx context.Context, msg *message.Msg) (err error) {
	if c.conn == nil {
		return
	}

	select {
	case c.send <- msg:
	case <-ctx.Done():
		return ctx.Err()
	}
	return
}

// @Reply
func (c *WsChannelClient) Send(ctx context.Context, id uint64, payload []byte, err error) error {
	if err != nil {
		return c.channel_result(ctx, actions.ACTION_REPLY_ERROR, id, []byte(err.Error()))
	}
	return c.channel_result(ctx, actions.ACTION_REPLY_SUCCESS, id, payload)
}

// implements @
func (c *WsChannelClient) Receive(ctx context.Context, payload []byte) error {
	timer := time.NewTimer(c.writeWait)
	select {
	case c.rpcResult <- payload:
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("rpc reply queue full")
	}
	return nil
}

func (c *WsChannelClient) channel_result(ctx context.Context, action byte, id uint64, data []byte) error {
	if c.conn == nil {
		return fmt.Errorf("conn is nil")
	}
	payload, err := fn.Encode(action, id, data)
	if err != nil {
		return err
	}
	timer := time.NewTimer(c.writeWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case c.rpcBacker <- payload:
	case <-timer.C:
		return fmt.Errorf("rpc reply queue full")
	}
	return nil
}

// login 登录
func (ch *WsChannelClient) GetAuthInfo() (*auth.AuthInfo, error) {
	return &auth.AuthInfo{
		UserId: ch.UserId,
		RoomId: ch.RoomId,
		Token:  ch.Sign,
	}, nil
}

func (ch *WsChannelClient) SetAuthInfo(auth *auth.AuthInfo) error {
	if auth == nil {
		return errors.New("auth is nil")
	}
	ch.UserId = auth.UserId
	ch.RoomId = auth.RoomId
	ch.Sign = auth.Token
	return nil
}

// types.IConnInfo
func (ch *WsChannelClient) GetUserId() int64 {
	return ch.UserId
}
func (ch *WsChannelClient) GetRoomId() int64 {
	return ch.RoomId
}

// Call 客户端 调用远程方法 同步调用
func (ch *WsChannelClient) Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {

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
	return ch.SendData(ctx, callId, payload)

}

// 服务器调用客户端方法
func (ch *WsChannelClient) SendData(ctx context.Context, msgId uint64, payload []byte) ([]byte, error) {
	ch.Lock.Lock()
	defer ch.Lock.Unlock()
	ticker := time.NewTicker(ch.writeWait)
	defer ticker.Stop()
	defer ch.rpc_io.Add(-1)
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
