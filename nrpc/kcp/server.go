// Package kcp 是第四个传输实现：基于 KCP（UDP）的 sloth 链路。
//
// 与 QUIC 相比它接入得更省事，原因有两条：
//  1. kcp-go 直接给出 net.Listener / net.Conn（ARQ、重传、拥塞控制都在库内
//     完成），因此不需要 QUIC 那种"一次 Accept 拿到连接、真正承载 RPC 的是
//     流"的铺平适配（见 nrpc/quic/listener.go 的 streamConn）；
//  2. 它不需要 TLS：KCP 自带 BlockCrypt 做载荷加密，没有证书也能跑。
//
// 代价是加密方式与 FEC 参数必须**在建监听器时**就确定（它们决定包格式），
// 所以本传输实现的是 ListenerFactoryWithOptions 而不是普通的 ListenerFactory。
//
// 除此之外它与 TCP 传输几乎逐行相同：拿到一段 net.Conn 语义的字节流之后，
// 分帧、读写泵、per-call 回包分发全部复用 nrpc/stream。
package kcp

import (
	"context"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/codec"
	"github.com/w6xian/sloth/v4/logger"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/metrics"
	"github.com/w6xian/sloth/v4/nrpc"
	"github.com/w6xian/sloth/v4/nrpc/stream"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types/handler"
	"github.com/w6xian/sloth/v4/types/trpc"

	"github.com/gorilla/mux"
)

// KcpHandleMessage 非 HTTP 传输的连接事件钩子（与 TCP / QUIC 同一套）。
//
// 钩子描述的是"一条连接的生命周期"，与它跑在 TCP、QUIC 还是 KCP 上无关，
// 因此不再定义第三份。名字仍带 Tcp 只是历史原因。
type KcpHandleMessage = handler.TcpHandleMessage

// defaultQueueSize 每条连接各队列的默认容量（与其他传输一致）。
const defaultQueueSize = stream.DefaultQueueSize

type kcpMetrics struct {
	connGauge      *metrics.GaugeFunc
	connRejected   *metrics.Counter
	broadcastDrops *metrics.Counter
	// stream 读写泵用的计数器：指标名带 kcp，因此由本传输自己注册。
	stream stream.Metrics
}

// KcpServer KCP 服务端：实现 types.IServer，可接入 bucket 体系与上层 RPC。
type KcpServer struct {
	nrpc.RpcConn

	Buckets   []*bucket.Bucket
	bucketIdx uint32

	handler KcpHandleMessage
	// cfg KCP 参数（加密 / FEC / 调优），由 option.WithKCPConfig 注入。
	cfg       option.KCPConfig
	ctx       context.Context
	ctxMu     sync.RWMutex
	connsMu   sync.Mutex
	conns     map[*KcpChannel]struct{}
	globalCnt atomic.Int64
	closed    bool
	queueSize int
	maxGlobal int64
	// maxLocal 本传输的限额（MaxConnsKCP），与 maxGlobal 是两道独立的闸
	maxLocal int64
	m        kcpMetrics
}

// ── option.IConnectOption ──────────────────────────────────────────────
// KCP 没有 URI、Origin、mux 路由这些 HTTP 概念，对应选项只能忽略。

func (s *KcpServer) SetUriPath(path string) error                                { return nil }
func (s *KcpServer) SetRouter(router *mux.Router) error                          { return nil }
func (s *KcpServer) SetAddress(address string) error                             { return nil }
func (s *KcpServer) SetHeader(key string, value string) error                    { return nil }
func (s *KcpServer) SetOrigin(args ...string) error                              { return nil }
func (s *KcpServer) SetClientHandleMessage(h handler.IClientHandleMessage) error { return nil }
func (s *KcpServer) SetCodec(c codec.Codec)                                      { s.Codec = c }

// SetKCPConfig 注入 KCP 参数。
//
// 注意时序：底层监听器在传输实例之前创建（Connect.Listen 先 makeListenerFor
// 再 CreateServer），因此监听器用的是从同一组选项里解析出的配置
// （见 option.ResolveKCPConfig）。这里存下的配置只用于给 accept 出来的
// 每条连接做调优。
func (s *KcpServer) SetKCPConfig(c option.KCPConfig) { s.cfg = c }

// SetServerHandleMessage 对 KCP 无效：HTTP 版钩子的每个方法都带 *http.Request。
func (s *KcpServer) SetServerHandleMessage(h handler.IServerHandleMessage) error {
	if h != nil {
		ctx := s.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		logger.Warnw(ctx, "SetServerHandleMessage is ignored on kcp transport, use option.WithTcpHandleMessage instead")
	}
	return nil
}

// SetTcpHandleMessage 注入连接钩子（与 TCP / QUIC 同一个入口）。
func (s *KcpServer) SetTcpHandleMessage(h KcpHandleMessage) { s.handler = h }

// SetChannelQueueSize 设置每条连接的队列容量。
func (s *KcpServer) SetChannelQueueSize(n int) {
	if n > 0 {
		s.queueSize = n
	}
}

func NewKcpServer(server trpc.ICallRpc, opts ...option.ConnectOption) *KcpServer {
	bsNum := max(1, runtime.NumCPU())
	opt := server.Options()
	bs := make([]*bucket.Bucket, bsNum)
	for i := 0; i < bsNum; i++ {
		bs[i] = bucket.NewBucket(
			bucket.WithChannelSize(opt.ChannelSize),
			bucket.WithRoomSize(opt.RoomSize),
			bucket.WithRoutineAmount(opt.RoutineAmount),
			bucket.WithRoutineSize(opt.RoutineSize),
		)
	}
	s := new(KcpServer)
	s.Buckets = bs
	s.bucketIdx = uint32(len(bs))
	s.Connect = server
	s.WriteWait = opt.WriteWait
	s.ReadWait = opt.ReadWait
	s.maxGlobal = opt.MaxConnsGlobal
	s.maxLocal = opt.MaxConnsKCP
	s.conns = make(map[*KcpChannel]struct{})
	s.queueSize = opt.ChannelQueueSize
	if s.queueSize <= 0 {
		s.queueSize = defaultQueueSize
	}
	if s.WriteWait <= 0 {
		s.WriteWait = 10 * time.Second
	}
	if s.ReadWait <= 0 {
		s.ReadWait = 10 * time.Second
	}
	s.registerMetrics()
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *KcpServer) registerMetrics() {
	s.m.connGauge = metrics.NewGaugeFunc(`sloth_kcp_connections`, "当前 KCP 活跃连接数", func() float64 {
		return float64(s.globalCnt.Load())
	})
	s.m.connRejected = metrics.NewCounter("sloth_kcp_conn_rejected_total", "因超过连接限额被拒的连接数")
	s.m.broadcastDrops = metrics.NewCounter("sloth_kcp_broadcast_drops_total", "广播时因连接队列满而丢弃的次数")
	s.m.stream = stream.Metrics{
		PumpRecovers: metrics.NewCounter("sloth_kcp_pump_recovers_total", "读写循环 panic 被兜住的次数"),
		FrameErrors:  metrics.NewCounter("sloth_kcp_frame_errors_total", "帧解析失败次数（含 magic 错误与超长帧）"),
	}
}

// ListenAndServe 保存父 context（真正的 accept 在 Serve 里，拿到 listener 之后）。
func (s *KcpServer) ListenAndServe(ctx context.Context) error {
	s.ctxMu.Lock()
	s.ctx = ctx
	s.ctxMu.Unlock()
	return nil
}

func (s *KcpServer) baseCtx() context.Context {
	s.ctxMu.RLock()
	ctx := s.ctx
	s.ctxMu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// Serve 在给定 listener 上接受连接，直到 listener 关闭。
func (s *KcpServer) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			// listener 被关闭（正常退出路径）
			return err
		}
		ip := stream.ClientIP(conn.RemoteAddr())
		if !s.acquireConn() {
			s.m.connRejected.Inc()
			logger.Warnw(nil, "kcp conn rejected: limit reached", "remote", conn.RemoteAddr())
			_ = conn.Close()
			continue
		}
		// 调优参数按连接生效（窗口 / nodelay / 流模式等）。
		tune(conn, s.cfg)
		ch := newKcpChannel(s.Connect, conn, ip, s.queueSize)
		s.addConn(ch)
		go s.serveConn(ch)
	}
}

// acquireConn 占一个连接名额，超过限额返回 false。
//
// 两道闸：全局（MaxConnsGlobal）与本传输（MaxConnsKCP），取更严的那个。
func (s *KcpServer) acquireConn() bool {
	if s.maxGlobal <= 0 && s.maxLocal <= 0 {
		return true
	}
	cur := s.globalCnt.Add(1)
	if (s.maxGlobal > 0 && cur > s.maxGlobal) || (s.maxLocal > 0 && cur > s.maxLocal) {
		s.globalCnt.Add(-1)
		return false
	}
	return true
}

func (s *KcpServer) releaseConn() { s.globalCnt.Add(-1) }

func (s *KcpServer) addConn(ch *KcpChannel) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	if s.closed {
		ch.Close()
		return
	}
	s.conns[ch] = struct{}{}
}

func (s *KcpServer) removeConn(ch *KcpChannel) {
	s.connsMu.Lock()
	delete(s.conns, ch)
	s.connsMu.Unlock()
}

// serveConn 服务一条连接：钩子校验 → 起读写泵 → 退出时清理 bucket 与计数。
//
// UDP 没有连接状态：对端掉电时本端收不到任何通知，deadline 到了才断开
// （读超时由 ReadWait 决定），不像 TCP 那样能靠 FIN/RST 立刻感知。
func (s *KcpServer) serveConn(ch *KcpChannel) {
	ctx, cancel := context.WithCancel(s.baseCtx())
	defer cancel()
	defer func() {
		if b := bucket.Pick(s.Buckets, ch.UserId()); b != nil {
			b.DeleteChannel(ch)
		}
		s.removeConn(ch)
		s.releaseConn()
		ch.Close()
		if s.handler != nil {
			_ = s.handler.OnClose(ctx, s, ch)
		}
	}()

	if s.handler != nil {
		if err := s.handler.OnConnect(ctx, ch.PAddr); err != nil {
			logger.Warnw(ctx, "kcp OnConnect rejected", "remote", ch.PAddr, "err", err)
			return
		}
		go func() { _ = s.handler.OnReady(ctx, s, ch) }()
	}

	stream.RunPump(ctx, ch, s.dispatch(ch), s.m.stream)
}

// dispatch 返回本连接入站帧的处理函数：与其他传输走同一个 nrpc.DispatchMessage。
func (s *KcpServer) dispatch(ch *KcpChannel) func(context.Context, []byte) error {
	return func(ctx context.Context, raw []byte) error {
		return nrpc.DispatchMessage(nrpc.RouteArgs{
			Context: ctx,
			Data:    raw,
			Codec:   s.Codec,
			OnFn: func(ctx context.Context, raw []byte) error {
				return nrpc.HandleFn(ctx, nil, nil, s, s.Connect, ch, raw)
			},
			OnData: func(ctx context.Context, raw []byte) error {
				if s.handler == nil {
					return nil
				}
				return s.handler.OnData(ctx, s, ch, raw)
			},
		})
	}
}

// Close 优雅关闭：停止接收新连接并关闭所有活跃连接与 bucket worker。
func (s *KcpServer) Close() error {
	s.connsMu.Lock()
	chs := make([]*KcpChannel, 0, len(s.conns))
	for ch := range s.conns {
		chs = append(chs, ch)
	}
	s.conns = make(map[*KcpChannel]struct{})
	s.closed = true
	s.connsMu.Unlock()

	for _, ch := range chs {
		_ = ch.Close()
	}
	for _, b := range s.Buckets {
		if b != nil {
			b.Close()
		}
	}
	return nil
}

// ── types.IServer / types.IBucket ─────────────────────────────────────

func (s *KcpServer) Bucket(userId int64) *bucket.Bucket {
	if len(s.Buckets) == 0 {
		return nil
	}
	return bucket.Pick(s.Buckets, userId)
}

func (s *KcpServer) Channel(userId int64) bucket.IChannel {
	if b := s.Bucket(userId); b != nil {
		return b.Channel(userId)
	}
	return nil
}

// Room 返回找到的第一个房间分片（保持接口单值语义）。
func (s *KcpServer) Room(roomId int64) *bucket.Room {
	for _, b := range s.Buckets {
		if b == nil {
			continue
		}
		if room := b.Room(roomId); room != nil {
			return room
		}
	}
	return nil
}

// Rooms 返回该房间在所有分片上的 Room 对象。
func (s *KcpServer) Rooms(roomId int64) []*bucket.Room {
	rooms := make([]*bucket.Room, 0, len(s.Buckets))
	for _, b := range s.Buckets {
		if b == nil {
			continue
		}
		if room := b.Room(roomId); room != nil && !room.IsDrop() {
			rooms = append(rooms, room)
		}
	}
	return rooms
}

func (s *KcpServer) AllBuckets() []*bucket.Bucket { return s.Buckets }

// Broadcast 向所有分片的所有房间广播。
func (s *KcpServer) Broadcast(ctx context.Context, msg *message.Msg) error {
	var dropped int
	for _, b := range s.Buckets {
		if b == nil {
			continue
		}
		dropped += b.BroadcastAll(ctx, msg)
	}
	if dropped > 0 {
		s.m.broadcastDrops.Add(int64(dropped))
	}
	return nil
}

// ── 连接通道 ─────────────────────────────────────────────────────────

// KcpChannel 一条 KCP 连接：直接复用公共字节流实现（见 nrpc/stream）。
type KcpChannel = stream.Channel

func newKcpChannel(connect trpc.ICallRpc, conn net.Conn, ip string, queueSize int) *KcpChannel {
	return stream.NewChannel(connect, conn, ip, queueSize)
}

var (
	errConnClosed = stream.ErrConnClosed
	errQueueFull  = stream.ErrQueueFull
)
