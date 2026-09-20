package wsocket

import (
	"context"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/w6xian/sloth/v4/internal/logger"
	"github.com/w6xian/sloth/v4/nrpc"
)

// defaultWorkerQueueSize 每个 worker 的默认待处理队列长度。
const defaultWorkerQueueSize = 256

// inboundJob 一条已解码、待执行的入站消息。
type inboundJob struct {
	server      *WsServer
	ch          *WsChannelServer
	req         *http.Request
	ctx         context.Context
	messageType byte
	data        []byte
	// shard 决定这条消息固定交给哪个 worker（同一连接恒定，保证按序执行）
	shard uint32
}

var jobPool = sync.Pool{New: func() any { return new(inboundJob) }}

// dispatchPool 入站消息执行池。
//
// 原实现在 readPump 里内联执行 handler：一条慢消息会堵住该连接的所有后续入站，
// 而且没有任何背压信号——慢客户端能把内存拖爆，快客户端被无关地拖慢。
//
// 现在 readPump 只做解码与投递，业务执行交给 worker：
//   - 每条连接按其 shard 固定到一个 worker：同一连接的消息仍严格按序执行
//     （业务不必担心乱序），不同连接之间并行；
//   - 每个 worker 的队列有界，满了就阻塞投递 —— 背压传导到读端，
//     内存不会无界增长。RPC 请求不能丢（丢了调用方只能干等超时），
//     所以这里选择阻塞而非丢弃，并用 waited 指标暴露背压强度。
type dispatchPool struct {
	queues []chan *inboundJob
	wg     sync.WaitGroup
	closed atomic.Bool
	// waited 因目标队列已满而阻塞等待的次数：持续上涨说明 handler 成了瓶颈
	waited atomic.Int64
}

func newDispatchPool(workers, queueSize int) *dispatchPool {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
		if workers < 4 {
			workers = 4
		}
	}
	if queueSize <= 0 {
		queueSize = defaultWorkerQueueSize
	}
	p := &dispatchPool{queues: make([]chan *inboundJob, workers)}
	p.wg.Add(workers)
	for i := range p.queues {
		p.queues[i] = make(chan *inboundJob, queueSize)
		go p.loop(p.queues[i])
	}
	return p
}

func (p *dispatchPool) loop(q <-chan *inboundJob) {
	defer p.wg.Done()
	for job := range q {
		job.server.dispatch(job)
		jobPool.Put(job)
	}
}

// submit 投递一条入站消息。队列满时阻塞（背压）；池已关闭或 ctx 取消返回 false。
func (p *dispatchPool) submit(ctx context.Context, job *inboundJob) bool {
	if p.closed.Load() {
		return false
	}
	q := p.queues[int(job.shard)%len(p.queues)]
	select {
	case q <- job:
		return true
	default:
	}
	// 队列满：阻塞等待空位。这里不丢弃请求——慢总比丢包后调用方干等超时好。
	p.waited.Add(1)
	select {
	case q <- job:
		return true
	case <-ctx.Done():
		return false
	}
}

// close 关闭所有队列并等待 worker 退出。
func (p *dispatchPool) close() {
	if !p.closed.CompareAndSwap(false, true) {
		return
	}
	for _, q := range p.queues {
		close(q)
	}
	p.wg.Wait()
}

// dispatch 执行一条入站消息：路由 → 业务 handler / RPC 调用。
//
// 单独加 recover：业务 handler 里的 panic 不应打挂 worker，
// 更不该连带整个服务端进程退出（原实现 panic 会击穿 readPump 直到进程崩溃）。
func (s *WsServer) dispatch(job *inboundJob) {
	defer func() {
		if r := recover(); r != nil {
			s.m.dispatchRecovers.Inc()
			s.log(logger.Error, "dispatch panic: %v", r)
		}
	}()
	ch := job.ch
	err := nrpc.DispatchMessage(nrpc.RouteArgs{
		Context: job.ctx,
		Request: job.req,
		Data:    job.data,
		// 用户通过 option.WithCodec 注入的 codec（nil = 按帧内容自动识别）
		Codec: s.Codec,
		OnFn: func(ctx context.Context, raw []byte) error {
			return nrpc.HandleFn(ctx, job.req, nil, s, s.Connect, ch, raw)
		},
		OnData: func(ctx context.Context, raw []byte) error {
			if s.handler == nil {
				return nil
			}
			return s.handler.OnData(ctx, job.req, s, ch, int(job.messageType), raw)
		},
	})
	if err != nil && s.handler != nil {
		s.handler.OnError(job.ctx, job.req, s, ch, err)
	}
}

// submitInbound 把一条入站消息交给 worker 池。
// 未启用 worker 池时退回内联执行（行为与改造前一致）。
func (s *WsServer) submitInbound(ctx context.Context, ch *WsChannelServer, r *http.Request, messageType byte, data []byte) bool {
	if s.pool == nil {
		s.dispatch(&inboundJob{server: s, ch: ch, req: r, ctx: ctx, messageType: messageType, data: data})
		return true
	}
	job := jobPool.Get().(*inboundJob)
	job.server, job.ch, job.req, job.ctx = s, ch, r, ctx
	job.messageType, job.data, job.shard = messageType, data, ch.shard
	if !s.pool.submit(ctx, job) {
		jobPool.Put(job)
		return false
	}
	return true
}
