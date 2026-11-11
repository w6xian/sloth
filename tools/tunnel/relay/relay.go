package main

// relay 核心逻辑。
//
// 角色：部署在云端固定 IP。agent 从家里主动 ws 拨上来（tunnel.Register），
// relay 校验 token 后把"隧道 ID → agent 连接"绑定；公网端口上来了访客连接，
// 就通过该 agent 的推送通道转发数据。
//
// 数据面（v1 全部走云端中继，fire-and-forget 推送，顺序由同一条 ws 连接保证）：
//
//	访客 ──TCP──▶ relay ──ch.Push──▶ agent ──TCP──▶ 内网 file-server
//	访客 ◀─TCP── relay ◀─client.Send─ agent ◀─TCP── 内网 file-server
//
// connID 由 relay 全局分配（visitor 侧与 agent 侧各一份登记表），
// OPEN 帧携带隧道 ID，agent 据此知道拨哪个本地目标。

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/tools/tunnel/proto"
)

// tunnel 一条隧道的运行时状态。
type tunnel struct {
	cfg TunnelCfg
	mu  sync.Mutex
	ch  bucket.IChannel // 接管该隧道的 agent 连接；nil = 离线

	// udp 模式专用：公网 UDP socket 与"访客五元组 → 会话"索引。
	// UDP 无连接，每个访客地址（ip:port）对应一条会话，各自一个 connID，
	// agent 侧为它拨一条"已连接"的本地 UDP socket，报文语义一对一保留。
	udpMu       sync.Mutex
	udpPC       net.PacketConn
	udpSessions map[string]*udpSession // key: 访客地址字符串
}

func (t *tunnel) setUDP(pc net.PacketConn) {
	t.udpMu.Lock()
	t.udpPC = pc
	t.udpSessions = make(map[string]*udpSession)
	t.udpMu.Unlock()
}

// udpSessionFor 查找（或创建）访客地址对应的会话。
// 返回 (会话, 是否新建)。新建时调用方负责推 OPEN 帧。
func (t *tunnel) udpSessionFor(addr *net.UDPAddr) (*udpSession, bool) {
	key := addr.String()
	t.udpMu.Lock()
	defer t.udpMu.Unlock()
	s := t.udpSessions[key]
	if s != nil {
		return s, false
	}
	s = &udpSession{tun: t, addr: addr}
	t.udpSessions[key] = s
	return s, true
}

func (t *tunnel) dropUDPSessionLocked(s *udpSession) {
	delete(t.udpSessions, s.addr.String())
}

// udpPC 返回该隧道的公网 UDP socket（非 udp 模式为 nil）。
func (t *tunnel) udpPCConn() net.PacketConn {
	t.udpMu.Lock()
	defer t.udpMu.Unlock()
	return t.udpPC
}

// udpSession 单个访客地址的 UDP 会话。last 是最近一次收到访客数据报的时间。
type udpSession struct {
	id   uint32
	tun  *tunnel
	addr *net.UDPAddr
	last atomic.Int64 // unix 秒
}

func (s *udpSession) touch() { s.last.Store(time.Now().Unix()) }

func (s *udpSession) idleFor(now int64) int64 {
	last := s.last.Load()
	if last == 0 {
		last = now
	}
	return now - last
}

func (t *tunnel) setChannel(ch bucket.IChannel) {
	t.mu.Lock()
	t.ch = ch
	t.mu.Unlock()
}

func (t *tunnel) channel() bucket.IChannel {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ch
}

func (t *tunnel) online() bool { return t.channel() != nil }

// visitorConn 公网侧的一条访客连接。
// raw 承担写出；r 承担读出（http 模式下会前置已偷看的请求头字节）。
type visitorConn struct {
	id  uint32
	tun *tunnel
	raw net.Conn
	r   io.Reader
}

// Relay relay 实例。
type Relay struct {
	cfg     Config
	tunnels map[string]*tunnel

	mu       sync.Mutex
	conns    map[uint32]*visitorConn // 全部在途 TCP 访客连接，connID 全局唯一
	udpConns map[uint32]*udpSession  // 全部在途 UDP 会话，connID 全局唯一
	nextID   atomic.Uint32

	lns   []net.Listener   // 启动期拉起的公网 TCP 监听，shutdown 时统一关闭
	udpPC []net.PacketConn // 启动期拉起的公网 UDP 监听，shutdown 时统一关闭
}

// NewRelay 校验配置并构造 Relay。
func NewRelay(cfg Config) (*Relay, error) {
	r := &Relay{
		cfg:      cfg,
		tunnels:  make(map[string]*tunnel, len(cfg.Tunnels)),
		conns:    make(map[uint32]*visitorConn),
		udpConns: make(map[uint32]*udpSession),
	}
	for _, tc := range cfg.Tunnels {
		switch tc.Mode {
		case "tcp":
			if tc.Listen == "" {
				return nil, fmt.Errorf("tunnel %s: tcp mode requires listen", tc.ID)
			}
		case "http":
			if tc.Listen == "" || tc.Host == "" {
				return nil, fmt.Errorf("tunnel %s: http mode requires listen and host", tc.ID)
			}
		case "udp":
			if tc.Listen == "" {
				return nil, fmt.Errorf("tunnel %s: udp mode requires listen", tc.ID)
			}
			if tc.Host != "" {
				return nil, fmt.Errorf("tunnel %s: udp mode does not use host", tc.ID)
			}
		default:
			return nil, fmt.Errorf("tunnel %s: unknown mode %q", tc.ID, tc.Mode)
		}
		if tc.ID == "" || tc.Token == "" {
			return nil, fmt.Errorf("tunnel (listen=%s): id and token are required", tc.Listen)
		}
		if _, dup := r.tunnels[tc.ID]; dup {
			return nil, fmt.Errorf("tunnel id %q duplicated", tc.ID)
		}
		r.tunnels[tc.ID] = &tunnel{cfg: tc}
	}
	return r, nil
}

// Start 拉起全部公网监听（tcp/udp 每隧道一个；http 按监听地址合并后按 Host 分发），
// 并启动 UDP 会话空闲回收。
func (r *Relay) Start(ctx context.Context) error {
	httpAddrs := make(map[string][]*tunnel)
	for _, t := range r.tunnels {
		switch t.cfg.Mode {
		case "tcp":
			ln, err := net.Listen("tcp", t.cfg.Listen)
			if err != nil {
				return fmt.Errorf("tcp tunnel %s: listen %s: %w", t.cfg.ID, t.cfg.Listen, err)
			}
			r.lns = append(r.lns, ln)
			sloth.Infow(ctx, "tcp tunnel listening", "tunnel", t.cfg.ID, "addr", t.cfg.Listen)
			go r.acceptTCP(ctx, t, ln)
		case "udp":
			pc, err := net.ListenPacket("udp", t.cfg.Listen)
			if err != nil {
				return fmt.Errorf("udp tunnel %s: listen %s: %w", t.cfg.ID, t.cfg.Listen, err)
			}
			r.udpPC = append(r.udpPC, pc)
			t.setUDP(pc)
			sloth.Infow(ctx, "udp tunnel listening", "tunnel", t.cfg.ID, "addr", t.cfg.Listen)
			go r.acceptUDP(ctx, t, pc)
		case "http":
			httpAddrs[t.cfg.Listen] = append(httpAddrs[t.cfg.Listen], t)
		}
	}
	for addr, ts := range httpAddrs {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("http tunnel: listen %s: %w", addr, err)
		}
		r.lns = append(r.lns, ln)
		for _, t := range ts {
			sloth.Infow(ctx, "http tunnel route", "tunnel", t.cfg.ID,
				"host", t.cfg.Host, "addr", addr)
		}
		go r.acceptHTTP(ctx, ln, ts)
	}
	go r.udpJanitor(ctx)
	return nil
}

// Shutdown 关闭全部公网监听与在途访客连接。
func (r *Relay) Shutdown() {
	for _, ln := range r.lns {
		_ = ln.Close()
	}
	for _, pc := range r.udpPC {
		_ = pc.Close()
	}
	r.mu.Lock()
	conns := r.conns
	r.conns = make(map[uint32]*visitorConn)
	r.udpConns = make(map[uint32]*udpSession)
	r.mu.Unlock()
	for _, vc := range conns {
		_ = vc.raw.Close()
	}
}

// ---- 公网侧：接受访客 ----

func (r *Relay) acceptTCP(ctx context.Context, t *tunnel, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// 读出与写出同为 conn（无预读字节）
		go r.openVisitor(ctx, t, conn, conn, nil)
	}
}

func (r *Relay) acceptHTTP(ctx context.Context, ln net.Listener, ts []*tunnel) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go r.handleHTTPConn(ctx, conn, ts)
	}
}

// ---- UDP 侧：接受访客数据报 ----

const (
	udpIdleTimeout  = 60 * time.Second // 会话空闲回收：超过即回 CLOSE 并删除
	udpJanitorEvery = 15 * time.Second
)

// acceptUDP 收访客数据报：按访客五元组查/建会话（新建则先推 OPEN），
// 再把数据报作为一帧 DATA 推给 agent。agent 离线时直接丢包——
// UDP 本就没有连接语义，客户端的上层重传/重试机制负责恢复。
func (r *Relay) acceptUDP(ctx context.Context, t *tunnel, pc net.PacketConn) {
	// 65535：先完整收下数据报，超过单帧容量的丢弃（拆分会破坏报文边界），
	// 而不是无声截断。
	buf := make([]byte, 65535)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		if n > proto.ChunkSize {
			sloth.Errorw(ctx, "udp datagram too large, dropped",
				"tunnel", t.cfg.ID, "remote", from, "size", n, "max", proto.ChunkSize)
			continue
		}
		ch := t.channel()
		if ch == nil {
			continue
		}
		s, isNew := t.udpSessionFor(from.(*net.UDPAddr))
		if isNew {
			s.id = r.nextID.Add(1)
			r.mu.Lock()
			r.udpConns[s.id] = s
			r.mu.Unlock()
			// OPEN 帧先于任何 DATA 帧（同一 goroutine 顺序推送）
			if !r.pushFrame(ctx, ch, proto.OpOpen, s.id, []byte(t.cfg.ID)) {
				r.dropUDPSession(ctx, s)
				continue
			}
			sloth.Infow(ctx, "udp session opened", "tunnel", t.cfg.ID,
				"conn", s.id, "remote", s.addr.String())
		}
		s.touch()
		if !r.pushFrame(ctx, ch, proto.OpData, s.id, buf[:n]) {
			r.dropUDPSession(ctx, s)
		}
	}
}

// getUDPSession 按 connID 查 UDP 会话。
func (r *Relay) getUDPSession(id uint32) *udpSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.udpConns[id]
}

// dropUDPSession 从两级索引（全局 connID 表 + 隧道的五元组表）移除会话。
func (r *Relay) dropUDPSession(ctx context.Context, s *udpSession) {
	r.mu.Lock()
	_, ok := r.udpConns[s.id]
	delete(r.udpConns, s.id)
	r.mu.Unlock()
	if !ok {
		return
	}
	s.tun.udpMu.Lock()
	s.tun.dropUDPSessionLocked(s)
	s.tun.udpMu.Unlock()
	sloth.Infow(ctx, "udp session closed", "tunnel", s.tun.cfg.ID,
		"conn", s.id, "remote", s.addr.String())
}

// udpJanitor 周期回收空闲 UDP 会话：回 CLOSE 让 agent 释放本地 socket。
func (r *Relay) udpJanitor(ctx context.Context) {
	ticker := time.NewTicker(udpJanitorEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := time.Now().Unix()
		r.mu.Lock()
		var expired []*udpSession
		for _, s := range r.udpConns {
			if s.idleFor(now) > int64(udpIdleTimeout/time.Second) {
				expired = append(expired, s)
			}
		}
		r.mu.Unlock()
		for _, s := range expired {
			_ = r.pushFrame(ctx, s.tun.channel(), proto.OpClose, s.id, nil)
			r.dropUDPSession(ctx, s)
		}
	}
}

// handleHTTPConn 偷看请求头、按 Host 找隧道，然后把偷看到的字节
// 作为该连接的第一段数据注入数据面（不丢字节：读端会先吐 head 再吐 br 余量）。
func (r *Relay) handleHTTPConn(ctx context.Context, conn net.Conn, ts []*tunnel) {
	br := bufio.NewReaderSize(conn, 16<<10)
	head, err := peekHeader(br)
	if err != nil {
		sloth.Infow(ctx, "http peek failed", "remote", conn.RemoteAddr(), "err", err)
		_ = conn.Close()
		return
	}
	// TLS ClientHello 的首字节固定 0x16：v1 不做 SNI 透传，直接拒绝并提示。
	if head[0] == 0x16 {
		sloth.Infow(ctx, "tls traffic rejected", "remote", conn.RemoteAddr(),
			"hint", "https requires SNI passthrough (not in v1); terminate TLS in front or use tcp mode")
		_ = conn.Close()
		return
	}
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(head)))
	if err != nil {
		sloth.Infow(ctx, "bad http request", "remote", conn.RemoteAddr(), "err", err)
		_ = conn.Close()
		return
	}
	t := matchHost(ts, req.Host)
	if t == nil {
		sloth.Infow(ctx, "no http route", "host", req.Host, "remote", conn.RemoteAddr())
		rejectHTTP(conn, http.StatusMisdirectedRequest,
			"tunnel relay: no route for host "+req.Host)
		return
	}
	if !t.online() {
		sloth.Infow(ctx, "tunnel offline", "tunnel", t.cfg.ID, "host", req.Host)
		rejectHTTP(conn, http.StatusBadGateway,
			"tunnel relay: tunnel "+t.cfg.ID+" is offline")
		return
	}
	r.openVisitor(ctx, t, conn, io.MultiReader(bytes.NewReader(head), br), head)
}

// openVisitor 登记访客连接、推 OPEN 帧，然后进入 pump（同步调用，
// accept goroutine 即该连接的 pump goroutine）。
// reader 是该连接的读端；first 是 http 模式下已偷看的请求头（可为 nil）。
func (r *Relay) openVisitor(ctx context.Context, t *tunnel, raw net.Conn, reader io.Reader, first []byte) {
	ch := t.channel()
	if ch == nil {
		_ = raw.Close()
		return
	}
	id := r.nextID.Add(1)
	vc := &visitorConn{id: id, tun: t, raw: raw, r: reader}
	r.mu.Lock()
	r.conns[id] = vc
	r.mu.Unlock()

	// OPEN 帧先于任何 DATA 帧（同一 goroutine 顺序推送），agent 收到才会拨本地目标。
	if !r.pushFrame(ctx, ch, proto.OpOpen, id, []byte(t.cfg.ID)) {
		sloth.Infow(ctx, "visitor rejected, agent channel dead", "tunnel", t.cfg.ID, "conn", id)
		r.dropConn(id)
		return
	}
	if len(first) > 0 {
		if !r.pushFrame(ctx, ch, proto.OpData, id, first) {
			r.dropConn(id)
			return
		}
	}
	r.pumpVisitor(ctx, vc, ch)
}

// pumpVisitor 访客 → agent：读一块推一帧；EOF/错误时回 CLOSE 收口。
func (r *Relay) pumpVisitor(ctx context.Context, vc *visitorConn, ch bucket.IChannel) {
	defer r.dropConn(vc.id)
	buf := make([]byte, proto.ChunkSize)
	for {
		n, err := vc.r.Read(buf)
		if n > 0 {
			if !r.pushFrame(ctx, ch, proto.OpData, vc.id, buf[:n]) {
				return
			}
		}
		if err != nil {
			_ = r.pushFrame(ctx, ch, proto.OpClose, vc.id, nil)
			return
		}
	}
}

// dropConn 从登记表移除并关闭访客连接。
func (r *Relay) dropConn(id uint32) {
	r.mu.Lock()
	vc := r.conns[id]
	delete(r.conns, id)
	r.mu.Unlock()
	if vc != nil {
		_ = vc.raw.Close()
	}
}

func (r *Relay) getConn(id uint32) *visitorConn {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conns[id]
}

// pushFrame 向 agent 推送一帧。fire-and-forget：推送成功即认为交付，
// 失败（连接已死）由调用方关闭对应访客连接收口。
func (r *Relay) pushFrame(ctx context.Context, ch bucket.IChannel, op byte, id uint32, payload []byte) bool {
	if ch == nil {
		return false
	}
	if cc, ok := ch.(interface{ IsClosed() bool }); ok && cc.IsClosed() {
		return false
	}
	return ch.Push(ctx, message.NewTextMessage(proto.Encode(op, id, payload))) == nil
}

// onAgentGone agent 连接断开：把它接管的全部隧道置离线，关闭在途访客连接。
// 访客侧的表现就是连接被立即复位——与"后端挂了"语义一致。
func (r *Relay) onAgentGone(ctx context.Context, ch bucket.IChannel) {
	for _, t := range r.tunnels {
		if t.channel() != ch {
			continue
		}
		t.setChannel(nil)
		r.mu.Lock()
		var ids []uint32
		for id, vc := range r.conns {
			if vc.tun == t {
				ids = append(ids, id)
			}
		}
		r.mu.Unlock()
		for _, id := range ids {
			r.dropConn(id)
		}
		// UDP 会话一并清掉：agent 侧本地 socket 已随 teardown 关闭，旧 connID
		// 的映射已失效；访客的下一个数据报会建新会话并重新走 OPEN。
		r.mu.Lock()
		var udpIDs []*udpSession
		for _, s := range r.udpConns {
			if s.tun == t {
				udpIDs = append(udpIDs, s)
			}
		}
		r.mu.Unlock()
		for _, s := range udpIDs {
			r.dropUDPSession(ctx, s)
		}
		sloth.Infow(ctx, "agent offline", "tunnel", t.cfg.ID,
			"conns dropped", len(ids), "udp sessions dropped", len(udpIDs))
	}
}

// ---- agent 侧数据入站（relay 的 ws OnData）----

// handleAgentFrame 处理 agent 推回的帧：写访客（TCP 连接 / UDP 数据报）/ 收口。
func (r *Relay) handleAgentFrame(ctx context.Context, b []byte) {
	op, id, payload, err := proto.Decode(b)
	if err != nil {
		sloth.Errorw(ctx, "bad frame from agent", "err", err)
		return
	}
	switch op {
	case proto.OpData:
		if vc := r.getConn(id); vc != nil {
			if _, err := vc.raw.Write(payload); err != nil {
				sloth.Errorw(ctx, "write to visitor failed", "conn", id, "err", err)
				r.dropConn(id)
			}
			return
		}
		if s := r.getUDPSession(id); s != nil {
			s.touch() // 有回包说明会话活跃
			if pc := s.tun.udpPCConn(); pc != nil {
				if _, err := pc.WriteTo(payload, s.addr); err != nil {
					sloth.Errorw(ctx, "write to udp visitor failed", "conn", id, "err", err)
					r.dropUDPSession(ctx, s)
				}
			}
			return
		}
		// TCP 访客已断开 / UDP 会话已过期：数据无处可去，静默丢弃
	case proto.OpClose:
		r.dropConn(id)
		if s := r.getUDPSession(id); s != nil {
			r.dropUDPSession(ctx, s)
		}
	default:
		sloth.Errorw(ctx, "unexpected op from agent", "op", op, "conn", id)
	}
}

// ---- 工具函数 ----

// peekHeader 从 br 偷看完整的 HTTP 请求头（以 \r\n\r\n 结尾），不消费任何字节。
func peekHeader(br *bufio.Reader) ([]byte, error) {
	for n := 4; ; n++ {
		if n > br.Size() {
			return nil, fmt.Errorf("http header too large (> %d bytes)", br.Size())
		}
		b, err := br.Peek(n)
		if err != nil {
			return nil, err
		}
		if bytes.HasSuffix(b, []byte("\r\n\r\n")) {
			return b, nil
		}
	}
}

// matchHost 按请求 Host 找隧道：精确匹配优先，其次 *.example.com 通配
// （匹配任意一级以上子域，不裸匹配 example.com 本身）。
func matchHost(ts []*tunnel, hostport string) *tunnel {
	h := hostOnly(hostport)
	for _, t := range ts {
		if hostOnly(t.cfg.Host) == h {
			return t
		}
	}
	for _, t := range ts {
		hc := hostOnly(t.cfg.Host)
		if strings.HasPrefix(hc, "*.") && strings.HasSuffix(h, hc[1:]) {
			return t
		}
	}
	return nil
}

// hostOnly 从 host:port 里剥出主机名并转小写。
func hostOnly(hostport string) string {
	h := strings.ToLower(strings.TrimSpace(hostport))
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}

// rejectHTTP 回一个最小 HTTP 响应并断开（用于无路由 / 隧道离线）。
func rejectHTTP(conn net.Conn, code int, text string) {
	body := text + "\n"
	fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\nContent-Type: text/plain; charset=utf-8\r\n"+
		"Connection: close\r\nContent-Length: %d\r\n\r\n%s",
		code, http.StatusText(code), len(body), body)
	_ = conn.Close()
}
