package sloth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/w6xian/sloth/bucket"
	"github.com/w6xian/sloth/decoder"
	"github.com/w6xian/sloth/message"
	"github.com/w6xian/sloth/nrpc"
	"github.com/w6xian/tlv"
)

var once sync.Once
var ClientObjc *ClientRpc

type ClientRpc struct {
	Serve   IServer
	Encoder func(any) ([]byte, error)
	Decoder func([]byte) ([]byte, error)
}

// LinkClientFunc 链接客户端  请用：DefaultServer 代替
// deprecated: use DefaultServer instead
func LinkClientFunc(opts ...IRpcOption) *ClientRpc {
	return DefaultServer(opts...)
}

func DefaultServer(opts ...IRpcOption) *ClientRpc {
	once.Do(func() {
		ClientObjc = &ClientRpc{
			Encoder: tlv.DefaultEncoder,
			Decoder: tlv.DefaultDecoder,
		}
		for _, opt := range opts {
			opt(ClientObjc)
		}
	})
	return ClientObjc
}

func (c *ClientRpc) SetEncoder(encoder Encoder) {
	c.Encoder = encoder
}

func (c *ClientRpc) SetDecoder(decoder Decoder) {
	c.Decoder = decoder
}

func GetChannel(ctx context.Context) (bucket.IChannel, error) {
	ch, ok := ctx.Value(ChannelKey).(bucket.IChannel)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}
	return ch, nil
}
func GetBucket(ctx context.Context) (nrpc.IBucket, error) {
	bucket, ok := ctx.Value(BucketKey).(nrpc.IBucket)
	if !ok {
		return nil, fmt.Errorf("bucket not found")
	}
	return bucket, nil
}

// @call client
func (c *ClientRpc) Call(ctx context.Context, userId int64, mtd string, arg ...any) ([]byte, error) {
	if c.Serve == nil {
		return nil, errors.New("server not found")
	}
	b := c.Serve.Bucket(userId)
	ch := b.Channel(userId)
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	args := [][]byte{}
	for _, v := range arg {
		b, err := c.Encoder(v)
		if err != nil {
			return nil, err
		}
		args = append(args, b)
	}

	resp, err := ch.Call(ctx, mtd, args...)
	// fmt.Println("Call resp::::::", resp, err)
	if err != nil {
		return nil, err
	}
	// fmt.Println("Call resp:", resp)
	// 解码
	resp, err = c.Decoder(resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ClientRpc) Channel(ctx context.Context, userId int64, action int, data string) {
	if c.Serve == nil {
		return
	}
	b := c.Serve.Bucket(userId)
	ch := b.Channel(userId)
	if ch == nil {
		return
	}
	cmd := message.CmdReq{
		Id:     decoder.NextId(),
		Ts:     time.Now().Unix(),
		Action: action,
		Data:   data,
	}
	msg := message.NewTextMessage(cmd.Bytes())
	if err := ch.Push(ctx, msg); err != nil {
		fmt.Println("Connect layer Push() error", err)
	}
}

func (c *ClientRpc) Room(ctx context.Context, roomId int64, action int, data string) {
	if c.Serve == nil {
		return
	}
	room := c.Serve.Room(roomId)
	if room == nil {
		return
	}
	if room.Drop {
		return
	}
	cmd := message.CmdReq{
		Id:     decoder.NextId(),
		Ts:     time.Now().Unix(),
		Action: action,
		Data:   data,
	}
	msg := message.NewTextMessage(cmd.Bytes())
	// fmt.Println("Connect layer Push() roomId", roomId)
	room.Push(ctx, msg)
}

func (c *ClientRpc) Broadcast(ctx context.Context, action int, data string) {
	if c.Serve == nil {
		return
	}
	cmd := message.CmdReq{
		Id:     decoder.NextId(),
		Ts:     time.Now().Unix(),
		Action: action,
		Data:   data,
	}
	msg := message.NewTextMessage(cmd.Bytes())
	if err := c.Serve.Broadcast(ctx, msg); err != nil {
		return
	}
}
