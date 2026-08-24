package nrpc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/w6xian/sloth/v3/actions"
	"github.com/w6xian/sloth/v3/internal/codec"
	"github.com/w6xian/sloth/v3/internal/utils"
	"github.com/w6xian/sloth/v3/internal/utils/id"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/types/trpc"
)

type RpcChannel struct {
	// 客户端的用户ID
	UserId int64
	// 在服务器中哪个房间
	RoomId int64
	//Sign 登录签名
	Sign           string
	PRpcCaller     chan []byte
	PRpcBacker     chan []byte
	PRpcResult     chan []byte
	PSend          chan *message.Msg
	Lock           sync.Mutex
	Connect        trpc.ICallRpc
	PDefaultHeader message.Header
	PAddr          string
	PPort          int64
	// writeWait default eq 10s
	PWriteWait time.Duration
	// readWait default eq 10s
	PReadWait time.Duration
}

func (c *RpcChannel) DefaultHeader() message.Header {
	return c.PDefaultHeader
}

// Call 客户端 调用远程方法 同步调用
func (cc *RpcChannel) Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {

	msg := message.GetCallJCO()
	msg.Header = header
	msg.Method = mtd
	msg.Args = args
	payload := utils.Serialize(msg)
	message.PutCallJCO(msg)

	callId := uint64(id.NextId(1))
	co := codec.UseCodec(codec.CODEC_CODER_FN)
	payload, err := co.Encode(actions.ACTION_CALL, callId, payload)
	if err != nil {
		return nil, err
	}
	return cc.SendData(ctx, callId, payload)

}

// 服务器调用客户端方法
func (cc *RpcChannel) SendData(ctx context.Context, msgId uint64, payload []byte) ([]byte, error) {
	cc.Lock.Lock()
	defer cc.Lock.Unlock()
	return CallFuncWithResult(ctx, msgId, payload, DataChannel{Read: cc.PRpcResult, Write: cc.PRpcCaller}, TimeOut{Read: cc.PReadWait, Write: cc.PWriteWait})
}

// @Reply
func (c *RpcChannel) Send(ctx context.Context, id uint64, payload []byte, err error) error {
	if err != nil {
		return c.channel_result(ctx, actions.ACTION_REPLY_ERROR, id, []byte(err.Error()))
	}
	return c.channel_result(ctx, actions.ACTION_REPLY_SUCCESS, id, payload)
}

// implements @
func (c *RpcChannel) Receive(ctx context.Context, payload []byte) error {
	timer := time.NewTimer(c.PWriteWait)
	select {
	case c.PRpcResult <- payload:
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("rpc reply queue full")
	}
	return nil
}

func (c *RpcChannel) channel_result(ctx context.Context, action byte, id uint64, data []byte) error {

	co := codec.UseCodec(codec.CODEC_CODER_FN)
	payload, err := co.Encode(action, id, data)
	if err != nil {
		return err
	}
	timer := time.NewTimer(c.PWriteWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case c.PRpcBacker <- payload:
	case <-timer.C:
		return fmt.Errorf("rpc reply queue full")
	}
	return nil
}

type RpcConn struct {
	Connect         trpc.ICallRpc
	WriteWait       time.Duration
	ReadWait        time.Duration
	PongWait        time.Duration
	PingPeriod      time.Duration
	MaxMessageSize  int64
	ReadBufferSize  int
	WriteBufferSize int
	BroadcastSize   int
	SliceSize       int64
	Header          map[string]string
	KeepAlive       bool
	Codec           codec.Codec
}

func (rc *RpcConn) SetCodec(c codec.Codec) {
	rc.Codec = c
}
