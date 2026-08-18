package sloth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/decoder"
	"github.com/w6xian/sloth/v3/decoder/ag"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/types"
)

type ClientRpc struct {
	Serve   types.IServer
	Encoder func(any) ([]byte, error)
	Decoder func([]byte) ([]byte, error)
	Header  message.Header
}

// LinkClientFunc 链接客户端  请用：DefaultServer 代替
// deprecated: use DefaultServer instead
func LinkClientFunc(opts ...IRpcOption) *ClientRpc {
	return DefaultServer(opts...)
}

func DefaultServer(opts ...IRpcOption) *ClientRpc {

	cli := &ClientRpc{
		Encoder: ag.Encoder,
		Decoder: ag.Decoder,
		Header:  message.Header{},
	}
	for _, opt := range opts {
		opt(cli)
	}

	return cli
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
func GetBucket(ctx context.Context) (types.IBucket, error) {
	bucket, ok := ctx.Value(BucketKey).(types.IBucket)
	if !ok {
		return nil, fmt.Errorf("bucket not found")
	}
	return bucket, nil
}
func GetHeader(ctx context.Context) (message.Header, error) {
	header, ok := ctx.Value(HeaderKey).(message.Header)
	if !ok {
		return message.Header{}, fmt.Errorf("header not found")
	}
	return header, nil
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
	args, err := decoder.EncodeArgs(arg, c.Encoder)
	if err != nil {
		return nil, err
	}

	resp, err := ch.Call(ctx, c.Header.Clone(), mtd, args...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// @call clientNet
func (c *ClientRpc) CallNet(ctx context.Context, proxyService int64, msgId uint64, data []byte) ([]byte, error) {
	if c.Serve == nil {
		return nil, errors.New("server not found")
	}
	b := c.Serve.Bucket(proxyService)
	ch := b.Channel(proxyService)
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	resp, err := ch.SendData(ctx, msgId, data)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *ClientRpc) CallWithHeader(ctx context.Context, header message.Header, userId int64, mtd string, arg ...any) ([]byte, error) {
	if c.Serve == nil {
		return nil, errors.New("server not found")
	}
	b := c.Serve.Bucket(userId)
	ch := b.Channel(userId)
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	args, err := decoder.EncodeArgs(arg, c.Encoder)
	if err != nil {
		return nil, err
	}

	usePoolHeader := false
	mergedHeader := header
	if len(c.Header) != 0 {
		usePoolHeader = true
		mergedHeader = message.GetHeader()
		for k, v := range c.Header {
			mergedHeader[k] = v
		}
		for k, v := range header {
			mergedHeader[k] = v
		}
	}
	if usePoolHeader {
		defer message.PutHeader(mergedHeader)
	}

	resp, err := ch.Call(ctx, mergedHeader, mtd, args...)
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
	}
}

// defaultCallTimeout 单次 RPC 调用超时。底层 SendData 已有 writeWait/readWait(默认 10s)
// 兜底不会无限阻塞，这里取更短的值，控制批量调用的总耗时。
const defaultCallTimeout = 5 * time.Second

// callRoomConcurrency CallRoom 并发调用上限，防止房间成员过多时 goroutine 爆炸。
const callRoomConcurrency = 64

func (c *ClientRpc) CallRoom(ctx context.Context, roomId int64, mtd string, arg ...any) ([]byte, error) {
	if c.Serve == nil {
		return nil, errors.New("server not found")
	}
	room := c.Serve.Room(roomId)
	if room == nil || room.IsDrop() {
		return nil, errors.New("room not found")
	}
	args, err := decoder.EncodeArgs(arg, c.Encoder)
	if err != nil {
		return nil, err
	}

	// 并发调用 + 每成员独立超时：总耗时 ≈ 最慢单次调用，而非 成员数×超时。
	sem := make(chan struct{}, callRoomConcurrency)
	var wg sync.WaitGroup
	room.Range(func(ch bucket.IChannel) bool {
		if ch == nil {
			return true
		}
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			callCtx, cancel := context.WithTimeout(ctx, defaultCallTimeout)
			defer cancel()
			if _, err := ch.Call(callCtx, c.Header.Clone(), mtd, args...); err != nil {
				log.Printf("room call err:%s", err.Error())
			}
		})
		return true
	})
	wg.Wait()

	return []byte{}, nil
}

func (c *ClientRpc) Room(ctx context.Context, roomId int64, action int, data string) {
	if c.Serve == nil {
		return
	}
	room := c.Serve.Room(roomId)
	if room == nil {
		return
	}
	if room.IsDrop() {
		return
	}
	cmd := message.CmdReq{
		Id:     decoder.NextId(),
		Ts:     time.Now().Unix(),
		Action: action,
		Data:   data,
	}
	msg := message.NewTextMessage(cmd.Bytes())
	room.Broadcast(ctx, msg)
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
		log.Printf("broadcast err:%s", err.Error())
		return
	}
}
