package wsocket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/w6xian/sloth/v2/actions"
	"github.com/w6xian/sloth/v2/decoder"
	"github.com/w6xian/sloth/v2/internal/logger"
	"github.com/w6xian/sloth/v2/internal/utils"
	"github.com/w6xian/sloth/v2/message"
	"github.com/w6xian/sloth/v2/types/auth"
	"github.com/w6xian/sloth/v2/types/trpc"

	"github.com/gorilla/websocket"
)

// 客户端对服务器的连接通道
// in fact, Client it's a user Connect session
type WsChannelClient struct {
	send      chan *message.Msg
	rpcCaller chan []byte
	rpcBacker chan []byte
	rpcResult chan []byte
	Connect   trpc.ICallRpc

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

	callObjPool sync.Pool
	backObjPool sync.Pool
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
	for _, opt := range opts {
		opt(c)
	}
	c.rpc_io = 0
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
func (s *WsChannelClient) log(level logger.LogLevel, line string, args ...any) {
	if s.Connect == nil {
		fmt.Println("WsServer Connect is nil")
		return
	}
	s.Connect.Log(level, "[WsChannelClient]"+line, args...)
}

func (c *WsChannelClient) getCallObj() *message.JsonCallObject {
	req := c.callObjPool.Get()
	if req == nil {
		return &message.JsonCallObject{}
	}
	return req.(*message.JsonCallObject)
}

func (c *WsChannelClient) putCallObj(req *message.JsonCallObject) {
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

func (c *WsChannelClient) getBackObj() *message.JsonBackObject {
	req := c.backObjPool.Get()
	if req == nil {
		return &message.JsonBackObject{}
	}
	return req.(*message.JsonBackObject)
}

func (c *WsChannelClient) putBackObj(req *message.JsonBackObject) {
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

func (c *WsChannelClient) Logout() (err error) {
	c.RoomId = 0
	c.UserId = 0
	c.Sign = ""
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
	default:
	}
	return
}

func (c *WsChannelClient) ReplySuccess(id string, data []byte) error {
	if c.conn == nil {
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
func (c *WsChannelClient) ReplyError(id string, err []byte) error {
	if c.conn == nil {
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
	ch.Lock.Lock()
	defer ch.Lock.Unlock()
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

	ch.log(logger.Debug, "Call WsClient------: id=%s method=%s", callId, mtd)

	select {
	case <-ticker.C:
		return []byte{}, fmt.Errorf("call timeout")
	case ch.rpcCaller <- payload:
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
				return []byte(""), errors.New(errStr)
			}
			data := back.Data
			ch.putBackObj(back)
			return data, nil
		}
	}
}

func (ch *WsChannelClient) CallAsync(ctx context.Context, header message.Header, mtd string, args ...[]byte) (chan *message.JsonBackObject, error) {
	ch.Lock.Lock()
	respChan := make(chan *message.JsonBackObject, 1)

	ticker := time.NewTicker(ch.writeWait)

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

	ch.log(logger.Debug, "CallAsync WsClient------: id=%s method=%s", callId, mtd)

	select {
	case <-ticker.C:
		ticker.Stop()
		ch.Lock.Unlock()
		close(respChan)
		return nil, fmt.Errorf("call timeout")
	case ch.rpcCaller <- payload:
	case <-ctx.Done():
		ticker.Stop()
		ch.Lock.Unlock()
		close(respChan)
		return nil, ctx.Err()
	default:
	}

	ticker.Reset(ch.readWait)

	go func() {
		defer ticker.Stop()
		defer ch.Lock.Unlock()
		defer close(respChan)

		for {
			select {
			case <-ctx.Done():
				respChan <- message.NewWsJsonBackError(callId, []byte(ctx.Err().Error()))
				return
			case <-ticker.C:
				respChan <- message.NewWsJsonBackError(callId, []byte("reply timeout"))
				return
			case raw, ok := <-ch.rpcResult:
				if !ok {
					respChan <- message.NewWsJsonBackError(callId, []byte("rpc result closed"))
					return
				}
				var back message.JsonBackObject
				if err := utils.Deserialize(raw, &back); err != nil {
					respChan <- message.NewWsJsonBackError(callId, []byte(err.Error()))
					return
				}
				if back.Id != callId {
					continue
				}
				respChan <- &back
				return
			}
		}
	}()

	return respChan, nil
}
