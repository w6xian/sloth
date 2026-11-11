package main

// coordinator 入口：三端打洞实例的"云端固定 IP"端。
//
// 职责只有两件，全部是信令面（流量几乎为 0）：
//  1. ws（sloth，:9100）：peer 拨入登记（punch.Register）、
//     牵线（punch.Connect，把两端的公网 UDP 地址互推给对方）；
//  2. UDP（:9101）：观测每个 peer 的公网 UDP 地址——STUN 的最小子集。
//     peer 用将来要打洞的同一个 socket 定期发 REG，coordinator 记下
//     观测地址并回 ACK 告知。
//
// 牵线成功后 coordinator 不再参与：数据在两端直连的 UDP 通道上流动。
// 打不通（Symmetric NAT / UDP 被封）由 peer 侧超时报告；生产实现应在
// 超时后回落云端中继（tools/tunnel v1 的 relay/agent 就是现成的回落通道）。
//
// 运行：
//
//	go build -o coordinator.exe ./coordinator
//	./coordinator.exe -rpc :9100 -udp :9101
//
// 本地联调（三端同一台机器，打洞必然成功，验证代码路径）见 README.md。

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/tools/tunnel/examples/p2p/wire"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/tlv"
)

// peerEntry 一个 peer 的运行时状态。
type peerEntry struct {
	mu   sync.Mutex
	ch   bucket.IChannel // ws 信令通道（重连后会被 Register 替换）
	addr *net.UDPAddr    // coordinator 观测到的公网 UDP 地址
}

func (e *peerEntry) setChannel(ch bucket.IChannel) {
	e.mu.Lock()
	e.ch = ch
	e.mu.Unlock()
}

func (e *peerEntry) channel() bucket.IChannel {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ch
}

func (e *peerEntry) setAddr(addr *net.UDPAddr) {
	e.mu.Lock()
	e.addr = addr
	e.mu.Unlock()
}

func (e *peerEntry) udpAddr() *net.UDPAddr {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.addr
}

// Hub 全部在线 peer 的登记表。
type Hub struct {
	mu    sync.Mutex
	peers map[string]*peerEntry
	next  atomic.Uint64 // 牵线会话号
}

func (h *Hub) get(id string) *peerEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.peers[id]
}

func (h *Hub) setChannel(id string, ch bucket.IChannel) {
	h.mu.Lock()
	e := h.peers[id]
	if e == nil {
		e = &peerEntry{}
		h.peers[id] = e
	}
	h.mu.Unlock()
	e.setChannel(ch)
}

// dropChannel 连接断开：仅当该连接仍是登记的那条时才移除
// （重连后新连接已顶替，旧连接的 OnClose 不能误删新状态）。
func (h *Hub) dropChannel(ch bucket.IChannel) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, e := range h.peers {
		if e.channel() == ch {
			delete(h.peers, id)
			return id, true
		}
	}
	return "", false
}

// Coord coordinator 实例。
type Coord struct {
	hub Hub
}

// NewCoord 构造并初始化内部状态。
func NewCoord() *Coord {
	return &Coord{hub: Hub{peers: make(map[string]*peerEntry)}}
}

// ---- 信令面：RPC 服务（sloth，方法名前缀 punch.）----

type regReq struct {
	ID string `json:"id"`
}

type regResp struct {
	OK bool `json:"ok"`
}

type connectReq struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type connectResp struct {
	Session  uint64 `json:"session"`
	PeerAddr string `json:"peer_addr"`
	Err      string `json:"err,omitempty"`
}

// PunchService peer 拨入后调用的 RPC 服务。
type PunchService struct {
	c *Coord
}

// Register peer 上线登记。断线重连后 OnReady 会再次触发，幂等替换通道。
func (s *PunchService) Register(ctx context.Context, req []byte) ([]byte, error) {
	ch, err := sloth.GetChannel(ctx)
	if err != nil {
		return nil, err
	}
	var rr regReq
	if err := tlv.Json2Struct(req, &rr); err != nil {
		return nil, fmt.Errorf("bad register payload: %w", err)
	}
	if rr.ID == "" {
		return nil, fmt.Errorf("peer id is required")
	}
	s.c.hub.setChannel(rr.ID, ch)
	sloth.Infow(ctx, "peer online", "peer", rr.ID)
	return tlv.Json(regResp{OK: true}), nil
}

// Connect 发起牵线：把两端的公网 UDP 地址互推给对方（JSON 信封经 ws 推送）。
// 双方收到 Intro 后各自开始对对方地址发 PUNCH 探针，coordinator 至此退场。
func (s *PunchService) Connect(ctx context.Context, req []byte) ([]byte, error) {
	chFrom, err := sloth.GetChannel(ctx)
	if err != nil {
		return nil, err
	}
	var cr connectReq
	if err := tlv.Json2Struct(req, &cr); err != nil {
		return nil, fmt.Errorf("bad connect payload: %w", err)
	}
	if cr.From == "" || cr.To == "" {
		return nil, fmt.Errorf("from and to are required")
	}
	if cr.From == cr.To {
		return nil, fmt.Errorf("cannot connect to self")
	}
	from, to := s.c.hub.get(cr.From), s.c.hub.get(cr.To)
	// 防冒充：调用连接必须是 From 登记的那条
	if from == nil || from.channel() != chFrom {
		return nil, fmt.Errorf("peer %q is not registered on this connection", cr.From)
	}
	addrFrom, addrTo := from.udpAddr(), to.udpAddr()
	if addrFrom == nil {
		return nil, fmt.Errorf("peer %q udp address not observed yet (wait for REG)", cr.From)
	}
	if to == nil || to.channel() == nil {
		return nil, fmt.Errorf("peer %q is offline", cr.To)
	}
	if addrTo == nil {
		return nil, fmt.Errorf("peer %q udp address not observed yet", cr.To)
	}

	session := s.c.hub.next.Add(1)
	okA := pushJSON(ctx, from.channel(), wire.Intro{
		Session: session, PeerID: cr.To, PeerAddr: addrTo.String(),
	})
	okB := pushJSON(ctx, to.channel(), wire.Intro{
		Session: session, PeerID: cr.From, PeerAddr: addrFrom.String(),
	})
	if !okA || !okB {
		return nil, fmt.Errorf("push intro failed (from=%v to=%v)", okA, okB)
	}
	sloth.Infow(ctx, "peers introduced", "session", session,
		"from", cr.From, "from_addr", addrFrom.String(),
		"to", cr.To, "to_addr", addrTo.String())
	return tlv.Json(connectResp{Session: session, PeerAddr: addrTo.String()}), nil
}

// pushJSON 向一条 ws 信令通道推送 JSON 报文（fire-and-forget）。
func pushJSON(ctx context.Context, ch bucket.IChannel, v any) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	if cc, ok := ch.(interface{ IsClosed() bool }); ok && cc.IsClosed() {
		return false
	}
	return ch.Push(ctx, message.NewTextMessage(b)) == nil
}

// ---- 信令面：ws 连接生命周期钩子 ----

type coordHandler struct {
	c *Coord
}

func (h *coordHandler) OnConnect(ctx context.Context, req *http.Request) error {
	return nil
}

func (h *coordHandler) OnReady(ctx context.Context, req *http.Request, s types.IBucket, ch bucket.IChannel) error {
	return nil
}

func (h *coordHandler) OnData(ctx context.Context, req *http.Request, s types.IBucket, ch bucket.IChannel, msgType int, msg []byte) error {
	return nil // peer 不在 ws 上推数据（数据面在 UDP）
}

func (h *coordHandler) OnClose(ctx context.Context, req *http.Request, s types.IBucket, ch bucket.IChannel) error {
	if id, ok := h.c.hub.dropChannel(ch); ok {
		sloth.Infow(ctx, "peer offline", "peer", id)
	}
	return nil
}

func (h *coordHandler) OnError(ctx context.Context, req *http.Request, s types.IBucket, ch bucket.IChannel, err error) error {
	if ch != nil {
		if id, ok := h.c.hub.dropChannel(ch); ok {
			sloth.Infow(ctx, "peer offline", "peer", id)
		}
	}
	return nil
}

// ---- 数据面观测：UDP ----

// udpLoop 观测各 peer 的公网 UDP 地址：REG 登记并回 ACK。
// 只认 REG 报文；其他数据不经过 coordinator（打洞成功后两端直连）。
func (c *Coord) udpLoop(conn *net.UDPConn) {
	buf := make([]byte, 1500)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		line := string(buf[:n])
		if id, ok := wire.ParseReg(line); ok {
			e := c.hub.get(id)
			if e == nil {
				continue // ws 未登记的 peer 不分配地址（信令先于数据面）
			}
			if old := e.udpAddr(); old == nil || old.String() != from.String() {
				sloth.Infow(context.Background(), "udp addr observed",
					"peer", id, "addr", from.String())
			}
			e.setAddr(from)
			_, _ = conn.WriteToUDP(wire.Ack(from.String()), from)
		}
	}
}

func main() {
	rpcAddr := flag.String("rpc", ":9100", "ws 信令监听地址")
	udpAddr := flag.String("udp", ":9101", "UDP 地址观测监听地址")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sloth.SetLogLevel("info")

	coord := NewCoord()

	uaddr, err := net.ResolveUDPAddr("udp", *udpAddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid udp addr:", err)
		os.Exit(1)
	}
	uconn, err := net.ListenUDP("udp", uaddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen udp:", err)
		os.Exit(1)
	}
	defer uconn.Close()
	go coord.udpLoop(uconn)

	server := sloth.DefaultServer()
	drpc := sloth.ServerConn(server)
	if err := drpc.Register("punch", &PunchService{c: coord}, "p2p punch coordinator"); err != nil {
		fmt.Fprintln(os.Stderr, "register service:", err)
		os.Exit(1)
	}
	if err := drpc.Listen(ctx, sloth.WS, *rpcAddr,
		option.WithServerHandleMessage(&coordHandler{c: coord})); err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- drpc.Serve() }()
	sloth.Infow(ctx, "coordinator started", "rpc", *rpcAddr, "udp", *udpAddr)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-serveErr:
		if err != nil {
			sloth.Errorw(ctx, "serve exited with error", "err", err)
		}
	case s := <-sig:
		sloth.Infow(ctx, "shutdown signal received", "signal", s.String())
	}
	cancel()
	_ = drpc.Close()
	_ = uconn.Close()
}
