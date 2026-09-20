package sloth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/decoder"
	"github.com/w6xian/sloth/v3/decoder/ag"
	"github.com/w6xian/sloth/v3/internal/errs"
	"github.com/w6xian/sloth/v3/internal/logger"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/types"
)

// ClientRpc 是「打给客户端」的 RPC 调用端，由服务端程序持有
// （按 userId 单推、CallRoom 房间推、CallBucket 全服推）。
//
// 名字里的 Client 指的是**调用目标**，不是持有者；对应入口是 DefaultServer()，
// 建连接用 ServerConn(server)。另见 ServerRpc 的说明。
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

// DefaultServer 返回服务端程序使用的 RPC 调用端（*ClientRpc，调用目标是客户端）。
// 名字里的 Server 指使用者，ClientRpc 里的 Client 指调用目标，别按"谁持有"理解，
// 详见 ClientRpc 的注释。
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

// channelClosed 报告连接是否已关闭。
//
// 连接层（WsChannelServer/WsChannelClient）可选实现 IsClosed()：
// bucket 中可能残留一条已被关闭但尚未被 readPump 清理的连接，
// 直接向它发起 RPC 会一直阻塞到 writeWait/readWait(默认 10s) 才失败，
// 调用方（尤其是批量调用）会被整体拖慢。这里先做一次无锁快速判定。
func channelClosed(ch bucket.IChannel) bool {
	if ch == nil {
		return true
	}
	cc, ok := ch.(interface{ IsClosed() bool })
	return ok && cc.IsClosed()
}

// @call client
func (c *ClientRpc) Call(ctx context.Context, userId int64, mtd string, arg ...any) ([]byte, error) {
	serve := c.getServe()
	if serve == nil {
		return nil, fmt.Errorf("server not found: %w", errs.ErrNotServing)
	}
	b := serve.Bucket(userId)
	ch := b.Channel(userId)
	if ch == nil {
		return nil, fmt.Errorf("channel not found: %w", errs.ErrNoChannel)
	}
	if channelClosed(ch) {
		return nil, fmt.Errorf("channel closed: %w", errs.ErrConnClosed)
	}
	args, err := decoder.EncodeArgs(arg, c.Encoder)
	if err != nil {
		return nil, err
	}

	// 每次调用一条独立 trace：写入 Header 随请求发给客户端（键 X-Trace-Id），
	// 使两端日志可跨进程串联。
	hdr, put := callHeader(ctx, c.Header, nil, "")
	defer put()

	resp, err := ch.Call(ctx, hdr, mtd, args...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// newTrace 返回 ctx 上已有的 trace id，没有则新生成一个（不修改 ctx，避免额外分配）。
func newTrace(ctx context.Context) string {
	if id := logger.TraceID(ctx); id != "" {
		return id
	}
	return logger.NewTraceID()
}

// callHeader 组装单次 RPC 调用的请求头：共享头 + 调用方头 + 本次 trace id。
// ServerRpc / ClientRpc 两个方向共用，避免各自复制一份后漏改。
//
// 三个容易踩的点集中在这里，调用方不必重复处理：
//  1. shared 是 RPC 对象上的共享 map（c.Header），并发调用同时写会 race，
//     必须拷贝出副本再写；
//  2. header 是调用方传进来的 map，同样不能直接写，否则污染调用方后续复用；
//  3. 需要合并两份时从 sync.Pool 取对象，返回的 put 必须在本次调用结束后调用，
//     归还后不得再持有该引用（Clone 出来的是普通 map，put 为空函数）。
//
// trace 传空串表示按 ctx 取/生成一条新的；批量调用（CallRoom/CallBucket）
// 传入固定 trace 可让整批共用同一条，便于对端按 trace 聚合日志。
func callHeader(ctx context.Context, shared, header message.Header, trace string) (message.Header, func()) {
	if trace == "" {
		trace = newTrace(ctx)
	}
	var merged message.Header
	put := func() {}
	if len(shared) != 0 {
		// 两份都要：取池对象合并，省一次 map 分配
		merged = message.GetHeader()
		for k, v := range shared {
			merged[k] = v
		}
		for k, v := range header {
			merged[k] = v
		}
		put = func() { message.PutHeader(merged) }
	} else {
		// 只有调用方的头：直接拷贝（Header.Clone 对 nil 也返回可用的空 map）
		merged = header.Clone()
	}
	merged.Set(logger.TraceHeader, trace)
	return merged, put
}

// @call clientNet
func (c *ClientRpc) CallNet(ctx context.Context, proxyService int64, msgId uint64, data []byte) ([]byte, error) {
	serve := c.getServe()
	if serve == nil {
		return nil, fmt.Errorf("server not found: %w", errs.ErrNotServing)
	}
	b := serve.Bucket(proxyService)
	ch := b.Channel(proxyService)
	if ch == nil {
		return nil, fmt.Errorf("channel not found: %w", errs.ErrNoChannel)
	}
	if channelClosed(ch) {
		return nil, fmt.Errorf("channel closed: %w", errs.ErrConnClosed)
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
		return nil, fmt.Errorf("server not found: %w", errs.ErrNotServing)
	}
	b := serve.Bucket(userId)
	ch := b.Channel(userId)
	if ch == nil {
		return nil, fmt.Errorf("channel not found: %w", errs.ErrNoChannel)
	}
	if channelClosed(ch) {
		return nil, fmt.Errorf("channel closed: %w", errs.ErrConnClosed)
	}
	args, err := decoder.EncodeArgs(arg, c.Encoder)
	if err != nil {
		return nil, err
	}

	// 与 Call 一致：合并共享头与调用方头，并注入本次调用的 trace。
	hdr, put := callHeader(ctx, c.Header, header, "")
	defer put()

	resp, err := ch.Call(ctx, hdr, mtd, args...)
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
		return nil, fmt.Errorf("server not found: %w", errs.ErrNotServing)
	}
	room := serve.Room(roomId)
	if room == nil || room.IsDrop() {
		return nil, errors.New("room not found")
	}
	args, err := decoder.EncodeArgs(arg, c.Encoder)
	if err != nil {
		return nil, err
	}

	// 一次房间调用共用一条 trace id：服务端日志可关联到同一次批量调用
	trace := newTrace(ctx)

	// 并发调用 + 每成员独立超时：总耗时 ≈ 最慢单次调用，而非 成员数×超时。
	sem := make(chan struct{}, callRoomConcurrency)
	var wg sync.WaitGroup
	room.Range(func(ch bucket.IChannel) bool {
		if ch == nil {
			return true
		}
		// 跳过已关闭连接：否则每个死连接都要白等到 defaultCallTimeout
		if channelClosed(ch) {
			return true
		}
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			callCtx, cancel := context.WithTimeout(ctx, defaultCallTimeout)
			defer cancel()
			// 每 goroutine 独立拷贝后再写 trace：共享 c.Header 直接写会有 race
			hdr, put := callHeader(ctx, c.Header, nil, trace)
			defer put()
			if _, err := ch.Call(callCtx, hdr, mtd, args...); err != nil {
				logger.Errorw(callCtx, "room call failed", "method", mtd, "err", err, "trace", trace)
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
		return nil, fmt.Errorf("server not found: %w", errs.ErrNotServing)
	}
	args, err := decoder.EncodeArgs(arg, c.Encoder)
	if err != nil {
		return nil, err
	}
	// 一次全服调用共用一条 trace id
	trace := newTrace(ctx)

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
			// 跳过已关闭连接：全服调用下死连接会被逐个等到超时，拖垮整批
			if channelClosed(ch) {
				return true
			}
			// 在调用线程内同步拷贝 header 快照：Header 是 map（引用语义），
			// 若在 goroutine 内才 Clone，调用方于 CallBucket 执行期间修改 c.Header
			// 仍会与 Clone 的读产生 race；提前拷贝后 goroutine 只读自己的副本，
			// 配合调用方"改 header → 调用 → 返回后再改"的串行模式即完全安全。
			// 拷贝出来的对象（池对象或 Clone）由该 goroutine 归还。
			hdr, put := callHeader(ctx, c.Header, nil, trace)
			sem <- struct{}{}
			wg.Go(func() {
				defer func() { <-sem }()
				defer put()
				callCtx, cancel := context.WithTimeout(ctx, defaultCallTimeout)
				defer cancel()
				if _, err := ch.Call(callCtx, hdr, mtd, args...); err != nil {
					if n := callBucketErrLog.Add(1); n == 1 || n%128 == 0 {
						logger.Errorw(callCtx, "bucket call failed", "method", mtd,
							"err", err, "failures", n, "trace", trace)
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
		logger.Errorw(ctx, "broadcast failed", "err", err)
		return
	}
}
