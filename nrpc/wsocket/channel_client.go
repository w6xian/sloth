package wsocket

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/nrpc"
	"github.com/w6xian/sloth/v3/types/auth"
	"github.com/w6xian/sloth/v3/types/trpc"

	"github.com/gorilla/websocket"
)

// 客户端对服务器的连接通道
// in fact, Client it's a user Connect session
type WsChannelClient struct {
	nrpc.RpcChannel
	Conn *websocket.Conn
	// closeOnce 保证底层连接只被关闭一次：
	// readPump/writePump 两个 goroutine 的 defer 与外部 Close 可能并发触发
	closeOnce sync.Once
}

// closeConn 幂等地关闭底层 WebSocket 连接（并发安全）
func (c *WsChannelClient) closeConn() {
	c.closeOnce.Do(func() {
		if c.Conn != nil {
			c.Conn.Close()
			c.Conn = nil
		}
	})
}

func NewWsChannelClient(connect trpc.ICallRpc, opts ...ChannelClientOption) (c *WsChannelClient) {
	c = new(WsChannelClient)
	c.Lock = sync.Mutex{}
	c.PSend = make(chan *message.Msg, 5)
	c.PRpcCaller = make(chan []byte, 10)
	c.PRpcBacker = make(chan []byte, 10)
	c.PRpcResult = make(chan []byte, 10)
	c.UserId = 0
	c.Conn = nil
	c.PWriteWait = 10 * time.Second
	c.PReadWait = 10 * time.Second
	c.Sign = ""
	c.Connect = connect
	c.PDefaultHeader = message.Header{}
	for _, opt := range opts {
		opt(c)
	}
	return
}

func (c *WsChannelClient) Logout() (err error) {
	c.RoomId = 0
	c.UserId = 0
	c.Sign = ""
	c.PAddr = ""
	c.PPort = 0
	return
}
func (c *WsChannelClient) Close() error {
	c.Lock.Lock()
	defer c.Lock.Unlock()
	c.closeConn()
	c.UserId = 0
	c.RoomId = 0
	c.PAddr = ""
	c.PPort = 0
	c.Sign = ""
	return nil
}

// Push 客户端 发送消息到服务器
func (c *WsChannelClient) Push(ctx context.Context, msg *message.Msg) (err error) {
	timer := time.NewTimer(c.PWriteWait)
	select {
	case c.PSend <- msg:
	case <-timer.C:
		return fmt.Errorf("rpc reply queue full")
	case <-ctx.Done():
		return ctx.Err()
	}
	return
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
