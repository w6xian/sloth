// Package tcp 是 WebSocket 之外的第二个传输实现。
//
// 它存在的意义不只是"多支持一种协议"，而是**检验传输层抽象**：
// ProtocolFactory 此前只有一个 ws 实现，而且连 ws 自己都没走它（connect.go 里
// 是判断 factory.Name()=="ws" 再走硬编码分支），抽象从未被真正使用过。
// 通常第一版抽象要到第二个实现时才暴露问题，这里已经暴露了四处，见各文件注释。
//
// 本包现在只保留"TCP 特有"的部分（accept 循环、连接限额、服务端/客户端外壳），
// 与字节流打交道的通用逻辑（分帧、读写泵、per-call 回包分发）已抽到
// nrpc/stream —— QUIC 的每条 stream 用的是同一份实现。
package tcp

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
	"github.com/w6xian/sloth/v4/metrics"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/nrpc"
	"github.com/w6xian/sloth/v4/nrpc/stream"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types/handler"
	"github.com/w6xian/sloth/v4/types/trpc"

	"github.com/gorilla/mux"
)

// TcpHandleMessage 是 handler.TcpHandleMessage 的别名。
//
// 接口本体定义在 types/handler：option.WithTcpHandleMessage 要引用它构造 option，
// 而 option 不能被传输包反向 import（会成环）。
type TcpHandleMessage = handler.TcpHandleMessage

// defaultQueueSize 每条连接各队列的默认容量（与 wsocket 一致）。
const defaultQueueSize = stream.DefaultQueueSize

type tcpMetrics struct {
	connGauge      *metrics.GaugeFunc
	connRejected   *metrics.Counter
	broadcastDrops *metrics.Counter
	// stream 读写泵用的计数器：指标名带 tcp，因此由本传输自己注册。
	stream stream.Metrics
}

// TcpServer TCP 服务端：实现 types.IServer，可直接接入 bucket 体系与上层 RPC。
type TcpServer struct {
	nrpc.RpcConn

	Buckets   []*bucket.Bucket
	bucketIdx uint32

	handler TcpHandleMessage
	// ctx 由 ListenAndServe 保存，作为所有连接的父 context
	ctx       context.Context
	ctxMu     sync.RWMutex
	connsMu   sync.Mutex
	conns     map[*TcpChannel]struct{}
	globalCnt atomic.Int64
	closed    bool
	// queueSize 每条连接的队列容量（option.WithChannelQueueSize 可配）
	queueSize int
	maxGlobal int64
	// maxLocal 本传输的限额（MaxConnsTCP），与 maxGlobal 是两道独立的闸
	maxLocal int64
	m         tcpMetrics
}

// ── option.IConnectOption ──────────────────────────────────────────────
// TCP 没有 URI、Origin、mux 路由这些 HTTP 概念，对应选项只能忽略。
// 这也是抽象的第二课：选项接口把 HTTP 专有项塞进了通用接口，
// 每种新传输都要被迫实现一堆"什么都做不了"的方法。

func (s *TcpServer) SetUriPath(path string) error                                { return nil }
func (s *TcpServer) SetRouter(router *mux.Router) error                          { return nil }
func (s *TcpServer) SetAddress(address string) error                             { return nil }
func (s *TcpServer) SetHeader(key string, value string) error                    { return nil }
func (s *TcpServer) SetOrigin(args ...string) error                              { return nil }
func (s *TcpServer) SetClientHandleMessage(h handler.IClientHandleMessage) error { return nil }
func (s *TcpServer) SetCodec(c codec.Codec)                                      { s.Codec = c }

// SetServerHandleMessage 对 TCP 无效：HTTP 版钩子的每个方法都带 *http.Request，
// TCP 没有 HTTP 握手可传（抽象泄漏，见 handler.TcpHandleMessage 注释）。
//
// 这里只能返回 nil（IConnectOption 的签名如此，option 层还会把返回值丢掉），
// 所以至少要打日志——否则用户以为回调挂上了，实际永远不触发。
func (s *TcpServer) SetServerHandleMessage(h handler.IServerHandleMessage) error {
	if h != nil {
		ctx := s.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		logger.Warnw(ctx, "SetServerHandleMessage is ignored on tcp transport, use option.WithTcpHandleMessage instead")
	}
	return nil
}

// SetTcpHandleMessage 注入 TCP 钩子。HTTP 版钩子（带 *http.Request）无法用于
// TCP，这里只接受 TCP 版钩子；传 HTTP 钩子会被忽略。
func (s *TcpServer) SetTcpHandleMessage(h TcpHandleMessage) { s.handler = h }

// SetChannelQueueSize 设置每条连接的队列容量（见 option.WithChannelQueueSize）。
func (s *TcpServer) SetChannelQueueSize(n int) {
	if n > 0 {
		s.queueSize = n
	}
}

func NewTcpServer(server trpc.ICallRpc, opts ...option.ConnectOption) *TcpServer {
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
	s := new(TcpServer)
	s.Buckets = bs
	s.bucketIdx = uint32(len(bs))
	s.Connect = server
	s.WriteWait = opt.WriteWait
	s.ReadWait = opt.ReadWait
	s.maxGlobal = opt.MaxConnsGlobal
	s.maxLocal = opt.MaxConnsTCP
	s.conns = make(map[*TcpChannel]struct{})
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

func (s *TcpServer) registerMetrics() {
	s.m.connGauge = metrics.NewGaugeFunc(`sloth_tcp_connections`, "当前 TCP 活跃连接数", func() float64 {
		return float64(s.globalCnt.Load())
	})
	s.m.connRejected = metrics.NewCounter("sloth_tcp_conn_rejected_total", "因超过连接限额被拒的连接数")
	s.m.broadcastDrops = metrics.NewCounter("sloth_tcp_broadcast_drops_total", "广播时因连接队列满而丢弃的次数")
	s.m.stream = stream.Metrics{
		PumpRecovers: metrics.NewCounter("sloth_tcp_pump_recovers_total", "读写循环 panic 被兜住的次数"),
		FrameErrors:  metrics.NewCounter("sloth_tcp_frame_errors_total", "帧解析失败次数（含 magic 错误与超长帧）"),
	}
}

// ListenAndServe 保存父 context。
//
// 抽象第三课：ws 版这个方法名是"监听并服务"，但它其实只注册 HTTP 路由，
// 真正的 listen 由上层 net.Listen + http.Serve 完成；TCP 版则必须在拿到
// net.Listener 之后才能 Accept。同一个方法名在两种传输下语义不同。
func (s *TcpServer) ListenAndServe(ctx context.Context) error {
	s.ctxMu.Lock()
	s.ctx = ctx
	s.ctxMu.Unlock()
	return nil
}

func (s *TcpServer) baseCtx() context.Context {
	s.ctxMu.RLock()
	ctx := s.ctx
	s.ctxMu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// Serve 在给定 listener 上接受连接，直到 listener 关闭。
// 由 Connect.Serve 在单独的 goroutine 里调用。
func (s *TcpServer) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			// listener 被关闭（正常退出路径）
			return err
		}
		ip := stream.ClientIP(conn.RemoteAddr())
		if !s.acquireConn() {
			s.m.connRejected.Inc()
			logger.Warnw(nil, "tcp conn rejected: limit reached", "remote", conn.RemoteAddr())
			_ = conn.Close()
			continue
		}
		ch := newTcpChannel(s.Connect, conn, ip, s.queueSize)
		s.addConn(ch)
		go s.serveConn(ch)
	}
}

// acquireConn 占一个连接名额，超过限额返回 false。
//
// 两道闸：全局（MaxConnsGlobal）与本传输（MaxConnsTCP），取更严的那个——
// 与 ws 传输的语义一致（见 wsocket.WsServer.acquireConn）。
// 单 IP 限额目前只对 ws 生效：它靠 HTTP 请求头取 IP，这里要另做按 IP 计数。
func (s *TcpServer) acquireConn() bool {
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

func (s *TcpServer) releaseConn() { s.globalCnt.Add(-1) }

func (s *TcpServer) addConn(ch *TcpChannel) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	if s.closed {
		ch.Close()
		return
	}
	s.conns[ch] = struct{}{}
}

func (s *TcpServer) removeConn(ch *TcpChannel) {
	s.connsMu.Lock()
	delete(s.conns, ch)
	s.connsMu.Unlock()
}

// serveConn 服务一条连接：钩子校验 → 起读写泵 → 退出时清理 bucket 与计数。
func (s *TcpServer) serveConn(ch *TcpChannel) {
	ctx, cancel := context.WithCancel(s.baseCtx())
	defer cancel()
	// 退出路径必须无条件清理：连接可能在登录前就断开，
	// 漏删会让 bucket/房间里残留已死连接（ws 版为此专门修过一次）。
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
			logger.Warnw(ctx, "tcp OnConnect rejected", "remote", ch.PAddr, "err", err)
			return
		}
		go func() { _ = s.handler.OnReady(ctx, s, ch) }()
	}

	stream.RunPump(ctx, ch, s.dispatch(ch), s.m.stream)
}

// dispatch 返回本连接入站帧的处理函数：与 ws 走同一个 nrpc.DispatchMessage，
// 因此路由判定、回包分发、业务 handler 调用全都是同一套代码。
func (s *TcpServer) dispatch(ch *TcpChannel) func(context.Context, []byte) error {
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
func (s *TcpServer) Close() error {
	s.connsMu.Lock()
	chs := make([]*TcpChannel, 0, len(s.conns))
	for ch := range s.conns {
		chs = append(chs, ch)
	}
	s.conns = make(map[*TcpChannel]struct{})
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

func (s *TcpServer) Bucket(userId int64) *bucket.Bucket {
	if len(s.Buckets) == 0 {
		return nil
	}
	return bucket.Pick(s.Buckets, userId)
}

func (s *TcpServer) Channel(userId int64) bucket.IChannel {
	if b := s.Bucket(userId); b != nil {
		return b.Channel(userId)
	}
	return nil
}

// Room 返回找到的第一个房间分片（保持接口单值语义）。
// 需要覆盖全房间请用 Rooms：成员按 userId 分散在各分片里。
func (s *TcpServer) Room(roomId int64) *bucket.Room {
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
func (s *TcpServer) Rooms(roomId int64) []*bucket.Room {
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

func (s *TcpServer) AllBuckets() []*bucket.Bucket { return s.Buckets }

// Broadcast 向所有分片的所有房间广播。
func (s *TcpServer) Broadcast(ctx context.Context, msg *message.Msg) error {
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

// TcpChannel 一条 TCP 连接：直接复用公共字节流实现（见 nrpc/stream）。
// QUIC 的每条 stream 用的是同一个 stream.Channel——两者在 sloth 眼里都是
// "一段按 FN 帧分帧的字节流"，没有理由维护两份实现。
type TcpChannel = stream.Channel

func newTcpChannel(connect trpc.ICallRpc, conn net.Conn, ip string, queueSize int) *TcpChannel {
	return stream.NewChannel(connect, conn, ip, queueSize)
}

var (
	errConnClosed = stream.ErrConnClosed
	errQueueFull  = stream.ErrQueueFull
)
