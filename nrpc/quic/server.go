// Package quic 是第三个传输实现：基于 QUIC（UDP）的 sloth 链路。
//
// 它在传输层抽象上又逼出两处问题，都记在各文件注释里：
//  1. 底层监听器不能再是 net.Listen("tcp") —— QUIC 跑在 UDP 上，
//     而且一次 Accept 拿到的是"连接"，真正承载 RPC 的是连接上的"流"
//     （见 listener.go）；
//  2. TLS 不再是可选项：QUIC 的加密由 TLS 1.3 承担，没有证书就没有 QUIC。
//
// 除此之外它与 TCP 传输几乎逐行相同：拿到一段 net.Conn 语义的字节流之后，
// 分帧、读写泵、per-call 回包分发全部复用 nrpc/stream。
package quic

import (
	"context"
	"errors"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/codec"
	"github.com/w6xian/sloth/v4/logger"
	"github.com/w6xian/sloth/v4/metrics"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/nrpc"
	"github.com/w6xian/sloth/v4/nrpc/stream"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types/handler"
	"github.com/w6xian/sloth/v4/types/trpc"

	"github.com/gorilla/mux"
)

// QuicHandleMessage 非 HTTP 传输的连接事件钩子（与 TCP 同一套）。
//
// 复用 TCP 那套接口而不是再定义一份：钩子描述的是"一条连接的生命周期"，
// 与它是跑在 TCP 还是 QUIC 上无关。名字仍带 Tcp 只是历史原因，
// 见 types/handler.TcpHandleMessage 的注释。
type QuicHandleMessage = handler.TcpHandleMessage

var (
	// errNoTLSCert QUIC 必须带证书：没有 TLS 就无法完成 QUIC 握手。
	errNoTLSCert  = errors.New("quic requires TLS: set sloth.WithTLSConfig(...) or sloth.WithTLSCertKey(certFile, keyFile)")
	errConnClosed = stream.ErrConnClosed
)

type quicMetrics struct {
	connGauge      *metrics.GaugeFunc
	connRejected   *metrics.Counter
	broadcastDrops *metrics.Counter
	// stream 读写泵用的计数器：指标名带 quic，因此由本传输自己注册。
	stream stream.Metrics
}

// QuicServer QUIC 服务端：实现 types.IServer，可接入 bucket 体系与上层 RPC。
type QuicServer struct {
	nrpc.RpcConn

	Buckets   []*bucket.Bucket
	bucketIdx uint32

	handler QuicHandleMessage
	// ctx 由 ListenAndServe 保存，作为所有连接的父 context
	ctx       context.Context
	ctxMu     sync.RWMutex
	connsMu   sync.Mutex
	conns     map[*stream.Channel]struct{}
	globalCnt atomic.Int64
	closed    bool
	// queueSize 每条连接的队列容量（option.WithChannelQueueSize 可配）
	queueSize int
	maxGlobal int64
	// maxLocal 本传输的限额（MaxConnsQUIC），与 maxGlobal 是两道独立的闸
	maxLocal int64
	m        quicMetrics
}

// ── option.IConnectOption ──────────────────────────────────────────────
// 与 TCP 一样：没有 URI / Origin / mux 路由这些 HTTP 概念，对应选项只能忽略。

func (s *QuicServer) SetUriPath(path string) error                                { return nil }
func (s *QuicServer) SetRouter(router *mux.Router) error                          { return nil }
func (s *QuicServer) SetAddress(address string) error                             { return nil }
func (s *QuicServer) SetHeader(key string, value string) error                    { return nil }
func (s *QuicServer) SetOrigin(args ...string) error                              { return nil }
func (s *QuicServer) SetClientHandleMessage(h handler.IClientHandleMessage) error { return nil }
func (s *QuicServer) SetCodec(c codec.Codec)                                      { s.Codec = c }

// SetServerHandleMessage 对 QUIC 无效：HTTP 版钩子的每个方法都带 *http.Request，
// QUIC 没有 HTTP 握手可传。返回 nil 但打日志，避免用户以为回调已生效。
func (s *QuicServer) SetServerHandleMessage(h handler.IServerHandleMessage) error {
	if h != nil {
		ctx := s.baseCtx()
		logger.Warnw(ctx, "SetServerHandleMessage is ignored on quic transport, use option.WithTcpHandleMessage instead")
	}
	return nil
}

// SetTcpHandleMessage 注入连接钩子。方法名沿用了 TCP 那套可选接口，
// 因此 option.WithTcpHandleMessage 对 TCP 与 QUIC **同时生效**。
func (s *QuicServer) SetTcpHandleMessage(h QuicHandleMessage) { s.handler = h }

// SetChannelQueueSize 设置每条连接的队列容量（见 option.WithChannelQueueSize）。
func (s *QuicServer) SetChannelQueueSize(n int) {
	if n > 0 {
		s.queueSize = n
	}
}

func NewQuicServer(server trpc.ICallRpc, opts ...option.ConnectOption) *QuicServer {
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
	s := new(QuicServer)
	s.Buckets = bs
	s.bucketIdx = uint32(len(bs))
	s.Connect = server
	s.WriteWait = opt.WriteWait
	s.ReadWait = opt.ReadWait
	s.maxGlobal = opt.MaxConnsGlobal
	s.maxLocal = opt.MaxConnsQUIC
	s.conns = make(map[*stream.Channel]struct{})
	s.queueSize = opt.ChannelQueueSize
	if s.queueSize <= 0 {
		s.queueSize = stream.DefaultQueueSize
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

func (s *QuicServer) registerMetrics() {
	s.m.connGauge = metrics.NewGaugeFunc(`sloth_quic_connections`, "当前 QUIC 活跃连接数", func() float64 {
		return float64(s.globalCnt.Load())
	})
	s.m.connRejected = metrics.NewCounter("sloth_quic_conn_rejected_total", "因超过连接限额被拒的连接数")
	s.m.broadcastDrops = metrics.NewCounter("sloth_quic_broadcast_drops_total", "广播时因连接队列满而丢弃的次数")
	s.m.stream = stream.Metrics{
		PumpRecovers: metrics.NewCounter("sloth_quic_pump_recovers_total", "读写循环 panic 被兜住的次数"),
		FrameErrors:  metrics.NewCounter("sloth_quic_frame_errors_total", "帧解析失败次数（含 magic 错误与超长帧）"),
	}
}

// ListenAndServe 保存父 context（真正的 accept 在 Serve 里）。
func (s *QuicServer) ListenAndServe(ctx context.Context) error {
	s.ctxMu.Lock()
	s.ctx = ctx
	s.ctxMu.Unlock()
	return nil
}

func (s *QuicServer) baseCtx() context.Context {
	s.ctxMu.RLock()
	ctx := s.ctx
	s.ctxMu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// Serve 在给定 listener 上接受连接，直到 listener 关闭。
// Accept 拿到的是 QUIC 连接上的一条流（见 Listener 注释）。
func (s *QuicServer) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			// listener 被关闭（正常退出路径）
			return err
		}
		ip := stream.ClientIP(conn.RemoteAddr())
		if !s.acquireConn() {
			s.m.connRejected.Inc()
			logger.Warnw(nil, "quic conn rejected: limit reached", "remote", conn.RemoteAddr())
			_ = conn.Close()
			continue
		}
		ch := stream.NewChannel(s.Connect, conn, ip, s.queueSize)
		s.addConn(ch)
		go s.serveConn(ch)
	}
}

// acquireConn 占一个连接名额，超过限额返回 false。
//
// 与 TCP 传输同一套语义：全局（MaxConnsGlobal）与本传输（MaxConnsQUIC）
// 两道闸，取更严的那个。单 IP 限额仅 ws 生效（见 TcpServer.acquireConn）。
func (s *QuicServer) acquireConn() bool {
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

func (s *QuicServer) releaseConn() { s.globalCnt.Add(-1) }

func (s *QuicServer) addConn(ch *stream.Channel) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	if s.closed {
		ch.Close()
		return
	}
	s.conns[ch] = struct{}{}
}

func (s *QuicServer) removeConn(ch *stream.Channel) {
	s.connsMu.Lock()
	delete(s.conns, ch)
	s.connsMu.Unlock()
}

// serveConn 服务一条流：钩子校验 → 起读写泵 → 退出时清理 bucket 与计数。
func (s *QuicServer) serveConn(ch *stream.Channel) {
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
			logger.Warnw(ctx, "quic OnConnect rejected", "remote", ch.PAddr, "err", err)
			return
		}
		go func() { _ = s.handler.OnReady(ctx, s, ch) }()
	}

	stream.RunPump(ctx, ch, s.dispatch(ch), s.m.stream)
}

// dispatch 本连接入站帧的处理函数：与 ws / tcp 走同一个 nrpc.DispatchMessage。
func (s *QuicServer) dispatch(ch *stream.Channel) func(context.Context, []byte) error {
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

// Close 优雅关闭：关闭所有活跃连接与 bucket worker。
func (s *QuicServer) Close() error {
	s.connsMu.Lock()
	chs := make([]*stream.Channel, 0, len(s.conns))
	for ch := range s.conns {
		chs = append(chs, ch)
	}
	s.conns = make(map[*stream.Channel]struct{})
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

func (s *QuicServer) Bucket(userId int64) *bucket.Bucket {
	if len(s.Buckets) == 0 {
		return nil
	}
	return bucket.Pick(s.Buckets, userId)
}

func (s *QuicServer) Channel(userId int64) bucket.IChannel {
	if b := s.Bucket(userId); b != nil {
		return b.Channel(userId)
	}
	return nil
}

// Room 返回找到的第一个房间分片（保持接口单值语义）。
// 需要覆盖全房间请用 Rooms：成员按 userId 分散在各分片里。
func (s *QuicServer) Room(roomId int64) *bucket.Room {
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
func (s *QuicServer) Rooms(roomId int64) []*bucket.Room {
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

func (s *QuicServer) AllBuckets() []*bucket.Bucket { return s.Buckets }

// Broadcast 向所有分片的所有房间广播。
func (s *QuicServer) Broadcast(ctx context.Context, msg *message.Msg) error {
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
