// Package tcp 是 WebSocket 之外的第二个传输实现。
//
// 它存在的意义不只是"多支持一种协议"，而是**检验传输层抽象**：
// ProtocolFactory 此前只有一个 ws 实现，而且连 ws 自己都没走它（connect.go 里
// 是判断 factory.Name()=="ws" 再走硬编码分支），抽象从未被真正使用过。
// 通常第一版抽象要到第二个实现时才暴露问题，这里已经暴露了三处，见各文件注释。
package tcp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/internal/codec"
	"github.com/w6xian/sloth/v3/internal/logger"
	"github.com/w6xian/sloth/v3/internal/metrics"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/nrpc"
	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/types/auth"
	"github.com/w6xian/sloth/v3/types/handler"
	"github.com/w6xian/sloth/v3/types/trpc"

	"github.com/gorilla/mux"
)

// TcpHandleMessage 是 handler.TcpHandleMessage 的别名。
//
// 接口本体定义在 types/handler：option.WithTcpHandleMessage 要引用它构造 option，
// 而 option 不能被传输包反向 import（会成环）。
type TcpHandleMessage = handler.TcpHandleMessage

// defaultQueueSize 每条连接各队列的默认容量（与 wsocket 一致）。
const defaultQueueSize = 10

type tcpMetrics struct {
	connGauge      *metrics.GaugeFunc
	connRejected   *metrics.Counter
	pumpRecovers   *metrics.Counter
	frameErrors    *metrics.Counter
	broadcastDrops *metrics.Counter
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
	m         tcpMetrics
}

// ── option.IConnectOption ──────────────────────────────────────────────
// TCP 没有 URI、Origin、mux 路由这些 HTTP 概念，对应选项只能忽略。
// 这也是抽象的第二课：选项接口把 HTTP 专有项塞进了通用接口，
// 每种新传输都要被迫实现一堆"什么都做不了"的方法。

func (s *TcpServer) SetUriPath(path string) error                                    { return nil }
func (s *TcpServer) SetRouter(router *mux.Router) error                              { return nil }
func (s *TcpServer) SetAddress(address string) error                                 { return nil }
func (s *TcpServer) SetHeader(key string, value string) error                        { return nil }
func (s *TcpServer) SetOrigin(args ...string) error                                  { return nil }
func (s *TcpServer) SetClientHandleMessage(h handler.IClientHandleMessage) error { return nil }
func (s *TcpServer) SetCodec(c codec.Codec)                                       { s.Codec = c }

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

// SetServerHandleMessage 注入 TCP 钩子。HTTP 版钩子（带 *http.Request）无法用于
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
	s.m.pumpRecovers = metrics.NewCounter("sloth_tcp_pump_recovers_total", "读写循环 panic 被兜住的次数")
	s.m.frameErrors = metrics.NewCounter("sloth_tcp_frame_errors_total", "帧解析失败次数（含 magic 错误与超长帧）")
	s.m.broadcastDrops = metrics.NewCounter("sloth_tcp_broadcast_drops_total", "广播时因连接队列满而丢弃的次数")
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
		ip := clientIP(conn.RemoteAddr())
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

func (s *TcpServer) acquireConn() bool {
	if s.maxGlobal <= 0 {
		return true
	}
	cur := s.globalCnt.Add(1)
	if cur > s.maxGlobal {
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

	runPump(ctx, ch, s.dispatch(ch), s.m)
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

// TcpChannel 一条 TCP 连接。服务端与客户端共用：两端都是
// "读帧 → dispatch / 写队列 → 写帧"，差异只在入站分发与身份语义。
type TcpChannel struct {
	nrpc.RpcChannel

	conn     net.Conn
	bcast    chan *message.Msg
	done     chan struct{}
	doneOnce sync.Once
	closed   atomic.Bool
	// head 本连接复用的帧头缓冲（readFrame 用）
	head headBuf
	// reader 带缓冲的读端：减少小帧场景下的 read 系统调用
	reader *bufio.Reader

	// bucket 链表字段（仅服务端语义；客户端恒为空）
	_room   *bucket.Room
	_next   bucket.IChannel
	_prev   bucket.IChannel
	_userId int64
	_sign   string

	errHandler func(err error)
	queueSize  int
}

func newTcpChannel(connect trpc.ICallRpc, conn net.Conn, ip string, queueSize int) *TcpChannel {
	if queueSize <= 0 {
		queueSize = defaultQueueSize
	}
	ch := new(TcpChannel)
	ch.Connect = connect
	ch.conn = conn
	ch.PAddr = ip
	ch.PWriteWait = 10 * time.Second
	ch.PReadWait = 10 * time.Second
	ch.bcast = make(chan *message.Msg, queueSize)
	ch.PRpcCaller = make(chan []byte, queueSize)
	ch.PRpcBacker = make(chan []byte, queueSize)
	ch.PSend = make(chan *message.Msg, queueSize)
	ch.done = make(chan struct{})
	ch.reader = bufio.NewReaderSize(conn, 4096)
	ch.queueSize = queueSize
	ch.errHandler = func(err error) {
		logger.Errorw(nil, "tcp channel error", "err", err)
	}
	ch.InitCalls() // per-call 回包分发表：SendData 依赖，未初始化会直接失败
	return ch
}

func (ch *TcpChannel) OnError(f func(err error)) { ch.errHandler = f }

func (ch *TcpChannel) IsClosed() bool { return ch.closed.Load() }

func (ch *TcpChannel) Next(n ...bucket.IChannel) bucket.IChannel {
	if len(n) > 0 {
		ch._next = n[0]
	}
	return ch._next
}

func (ch *TcpChannel) Prev(p ...bucket.IChannel) bucket.IChannel {
	if len(p) > 0 {
		ch._prev = p[0]
	}
	return ch._prev
}

func (ch *TcpChannel) Room(r ...*bucket.Room) *bucket.Room {
	if len(r) > 0 {
		ch._room = r[0]
	}
	return ch._room
}

func (ch *TcpChannel) UserId(u ...int64) int64 {
	if len(u) > 0 {
		ch._userId = u[0]
	}
	return ch._userId
}

func (ch *TcpChannel) Token(t ...string) string {
	if len(t) > 0 {
		ch._sign = t[0]
	}
	return ch._sign
}

func (ch *TcpChannel) Logout() { ch._userId = 0 }

// GetAuthInfo 服务端语义：身份由业务在登录 RPC 里通过 bucket.Put 写入。
func (ch *TcpChannel) GetAuthInfo() (*auth.AuthInfo, error) {
	if ch._userId == 0 {
		return nil, errors.New("tcp channel: user id is 0")
	}
	if ch._room == nil {
		return nil, errors.New("tcp channel: room is nil")
	}
	if ch._sign == "" {
		return nil, errors.New("tcp channel: sign is empty")
	}
	return &auth.AuthInfo{
		UserId: ch._userId,
		RoomId: ch._room.Id,
		Token:  ch._sign,
	}, nil
}

func (ch *TcpChannel) SetAuthInfo(a *auth.AuthInfo) error {
	return errors.New("tcp channel: server does not support set auth info")
}

// Push 投递一条推送消息（服务端广播 / 客户端上行都走这里）。
// 只入队，不直接写连接——写由 writePump 串行完成（net.Conn 不支持并发写）。
func (ch *TcpChannel) Push(ctx context.Context, msg *message.Msg) error {
	if ch.closed.Load() {
		return fmt.Errorf("tcp push on closed channel: %w", errConnClosed)
	}
	timer := time.NewTimer(ch.PWriteWait)
	defer timer.Stop()
	select {
	case ch.bcast <- msg:
		return nil
	case <-timer.C:
		return fmt.Errorf("tcp push queue full: %w", errQueueFull)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (ch *TcpChannel) Close() error {
	ch.Lock.Lock()
	defer ch.Lock.Unlock()

	ch.doneOnce.Do(func() { close(ch.done) })
	// 唤醒所有等回包的调用：否则它们要干等到超时才返回
	ch.CloseCalls()
	ch.closed.Store(true)
	if ch.conn != nil {
		_ = ch.conn.Close()
	}
	return nil
}

var (
	errConnClosed = errors.New("connection closed")
	errQueueFull  = errors.New("queue full")
)

// runPump 启动读写泵，阻塞到连接结束。
//
// 读：按 FN 帧分帧 → 交给 dispatch（服务端是 HandleFn/业务钩子，客户端是回包分发）。
// 写：串行消费 bcast / PRpcCaller / PRpcBacker —— 单一写者，无需加锁。
func runPump(ctx context.Context, ch *TcpChannel, dispatch func(context.Context, []byte) error, m tcpMetrics) {
	defer func() {
		if err := recover(); err != nil {
			m.pumpRecovers.Inc()
			logger.Errorw(ctx, "tcp pump recover", "err", err, "remote", ch.PAddr)
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		writePump(ctx, ch)
	}()

	readPump(ctx, ch, dispatch, m)

	// 读循环结束即连接结束：通知写循环退出并等它收尾
	_ = ch.Close()
	<-done
}

// readPump 读循环：分帧 + 分发。
func readPump(ctx context.Context, ch *TcpChannel, dispatch func(context.Context, []byte) error, m tcpMetrics) {
	for {
		frame, err := readFrame(ch.reader, ch.head[:])
		if err != nil {
			// 连接正常关闭（EOF）或超时都走这里；不区分，交给上层清理
			if ch.errHandler != nil && !errors.Is(err, net.ErrClosed) {
				ch.errHandler(err)
			}
			return
		}
		if err := dispatch(ctx, frame); err != nil {
			m.frameErrors.Inc()
			logger.Warnw(ctx, "tcp dispatch failed", "err", err, "remote", ch.PAddr)
		}
	}
}

// writePump 写循环：串行写出所有待发数据。
func writePump(ctx context.Context, ch *TcpChannel) {
	for {
		select {
		case <-ch.done:
			return
		case <-ctx.Done():
			return
		case msg := <-ch.bcast:
			if err := ch.write(msg.MarshalJSONFast()); err != nil {
				return
			}
		case payload := <-ch.PRpcCaller:
			// 本端主动发起的 RPC 请求
			if err := ch.write(payload); err != nil {
				return
			}
		case payload := <-ch.PRpcBacker:
			// 对端 RPC 调用的回包
			if err := ch.write(payload); err != nil {
				return
			}
		case msg := <-ch.PSend:
			if msg == nil {
				continue
			}
			if err := ch.write(msg.MarshalJSONFast()); err != nil {
				return
			}
		}
	}
}

// write 写一帧并刷新。FN 帧自带 length，对端按它分帧。
func (ch *TcpChannel) write(payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	if ch.PWriteWait > 0 {
		if err := ch.conn.SetWriteDeadline(time.Now().Add(ch.PWriteWait)); err != nil {
			return err
		}
	}
	_, err := ch.conn.Write(payload)
	return err
}

// clientIP 从远端地址里取 IP（去掉端口）。
func clientIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr.String()); err == nil {
		return host
	}
	return addr.String()
}
