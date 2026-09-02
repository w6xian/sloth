package sloth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/decoder"
	"github.com/w6xian/sloth/v3/decoder/ag"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/types"
)

type ClientRpc struct {
	// mu 保护 Serve 字段：Serve() 在服务 goroutine 中写入，
	// Call/CallRoom 等可能在另一 goroutine 读取
	mu      sync.RWMutex
	Serve   types.IServer
	Encoder func(any) ([]byte, error)
	Decoder func([]byte) ([]byte, error)
	Header  message.Header
}

// setServe 在服务启动（initWsServerInstance）时写入服务端实例
func (c *ClientRpc) setServe(s types.IServer) {
	c.mu.Lock()
	c.Serve = s
	c.mu.Unlock()
}

// getServe 返回服务端实例（调用方持引用在锁外使用）
func (c *ClientRpc) getServe() types.IServer {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Serve
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
	serve := c.getServe()
	if serve == nil {
		return nil, errors.New("server not found")
	}
	b := serve.Bucket(userId)
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
	serve := c.getServe()
	if serve == nil {
		return nil, errors.New("server not found")
	}
	b := serve.Bucket(proxyService)
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
	serve := c.getServe()
	if serve == nil {
		return nil, errors.New("server not found")
	}
	b := serve.Bucket(userId)
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
	serve := c.getServe()
	if serve == nil {
		return
	}
	b := serve.Bucket(userId)
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
	serve := c.getServe()
	if serve == nil {
		return nil, errors.New("server not found")
	}
	room := serve.Room(roomId)
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

// callBucketErrLog 全服批量调用失败日志采样计数：失败连接往往成批出现（如断连），
// 全量打印会形成日志风暴，故仅首次与每满 128 次失败记录一条。
var callBucketErrLog atomic.Uint64

// CallBucket 对服务端所有在线连接发起一次方法调用（全服推送 RPC）。
// 遍历各 bucket 的连接唯一映射（RangeChannels）而非房间成员：同一连接即使同时在
// 多个房间也只被调用一次，且未入任何房间的在线连接也不会漏掉。
// 并发受信号量限制，每连接独立超时（defaultCallTimeout），单点失败不中断整体。
func (c *ClientRpc) CallBucket(ctx context.Context, mtd string, arg ...any) ([]byte, error) {
	serve := c.getServe()
	if serve == nil {
		return nil, errors.New("server not found")
	}
	args, err := decoder.EncodeArgs(arg, c.Encoder)
	if err != nil {
		return nil, err
	}

	sem := make(chan struct{}, callRoomConcurrency)
	var wg sync.WaitGroup
	for _, b := range serve.AllBuckets() {
		if b == nil {
			continue
		}
		b.RangeChannels(func(ch bucket.IChannel) bool {
			if ch == nil {
				return true
			}
			// 在调用线程内同步拷贝 header 快照：Header 是 map（引用语义），
			// 若在 goroutine 内才 Clone，调用方于 CallBucket 执行期间修改 c.Header
			// 仍会与 Clone 的读产生 race；提前拷贝后 goroutine 只读自己的副本，
			// 配合调用方"改 header → 调用 → 返回后再改"的串行模式即完全安全。
			header := c.Header.Clone()
			sem <- struct{}{}
			wg.Go(func() {
				defer func() { <-sem }()
				callCtx, cancel := context.WithTimeout(ctx, defaultCallTimeout)
				defer cancel()
				if _, err := ch.Call(callCtx, header, mtd, args...); err != nil {
					if n := callBucketErrLog.Add(1); n == 1 || n%128 == 0 {
						log.Printf("bucket call err:%s", err.Error())
					}
				}
			})
			return true
		})
	}
	wg.Wait()

	return []byte{}, nil
}

func (c *ClientRpc) Room(ctx context.Context, roomId int64, action int, data string) {
	serve := c.getServe()
	if serve == nil {
		return
	}
	room := serve.Room(roomId)
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
	serve := c.getServe()
	if serve == nil {
		return
	}
	cmd := message.CmdReq{
		Id:     decoder.NextId(),
		Ts:     time.Now().Unix(),
		Action: action,
		Data:   data,
	}
	msg := message.NewTextMessage(cmd.Bytes())
	if err := serve.Broadcast(ctx, msg); err != nil {
		log.Printf("broadcast err:%s", err.Error())
		return
	}
}
