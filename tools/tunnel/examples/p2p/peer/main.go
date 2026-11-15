package main

// peer 入口：三端打洞实例的 NAT 后节点端（一个二进制，跑几个实例就是几个端）。
//
// 全流程：
//
//	① ws 连 coordinator，punch.Register 登记（重连后自动重新登记）；
//	② 用打洞要用的 **同一个 UDP socket** 每 2s 向 coordinator 发 REG 保活，
//	   coordinator 回 ACK 告知观测到的公网地址（cone 类 NAT 下即是对端可达地址）；
//	③ -connect 指定对端 ID 时调 punch.Connect，coordinator 把双方地址互推
//	   （ws 推 Intro）；
//	④ 双方各自对对方公网地址狂发 PUNCH 探针（150ms 一次，同一条 socket），
//	   谁先收到谁的探针，谁就确认直连建立——NAT 两侧的洞同时被凿开；
//	⑤ 成功后直接在 UDP 上互发消息（stdout 聊天），coordinator 不再参与；
//	   20s 打不通则报告失败（Symmetric NAT / UDP 被封）。
//
// 运行（两端各一台机器，各一个终端）：
//
//	go build -o peer.exe ./peer
//	./peer.exe -server ws://coord.graybook.com.cn:9100 -coord coord.graybook.com.cn:9101 -id home
//	./peer.exe -server ws://coord.graybook.com.cn:9100 -coord coord.graybook.com.cn:9101 -id office -connect home
//
// 连上后直接敲回车聊天，Ctrl+C 退出。

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/tools/tunnel/examples/p2p/wire"
	"github.com/w6xian/sloth/v4/tools/tunnel/proto"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/tlv"
)

const (
	regInterval    = 2 * time.Second // REG 保活周期
	punchInterval  = 150 * time.Millisecond
	punchTimeout   = 20 * time.Second
	connectRetries = 10
)

// Peer 一个 NAT 后节点。
type Peer struct {
	id       string
	coordUDP string // coordinator 的 UDP 观测地址（host:port）
	conn     *net.UDPConn
	client   *sloth.ServerRpc
	runCtx   context.Context

	registered atomic.Bool

	// 登记去重：重连风暴下 OnReady 可能连续触发
	regMu       sync.Mutex
	registering bool

	mu        sync.Mutex
	public    string       // coordinator 观测到的本端公网 UDP 地址
	session   uint64       // 当前牵线会话号
	punching  bool         // 正在打洞
	connected bool         // 直连已建立
	remote    *net.UDPAddr // 对端实际来源地址（以收到的探针为准）
}

// ---- UDP 数据面 ----

func (p *Peer) udpLoop() {
	buf := make([]byte, 1500)
	for {
		n, from, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		line := string(buf[:n])
		switch {
		case p.handleAck(line):
		case p.handlePunch(from, line):
		case p.handleData(from, line):
		}
	}
}

func (p *Peer) handleAck(line string) bool {
	addr, ok := wire.ParseAck(line)
	if !ok {
		return false
	}
	p.mu.Lock()
	changed := p.public != addr
	p.public = addr
	p.mu.Unlock()
	if changed {
		fmt.Printf("[%s] coordinator 观测到本端公网 UDP 地址: %s\n", p.id, addr)
	}
	return true
}

// handlePunch 收到探针：如果正是当前会话且尚未确认直连，则确认建立
// （对端来源地址可能与 coordinator 观测的不同，以实际来源为准），
// 并立即回一根探针帮对端也尽快确认。
func (p *Peer) handlePunch(from *net.UDPAddr, line string) bool {
	session, ok := wire.ParsePunch(line)
	if !ok {
		return false
	}
	p.mu.Lock()
	active := p.punching && session == p.session && !p.connected
	if active {
		p.connected = true
		p.remote = from
	}
	connected := p.connected
	sessionActive := p.session == session
	p.mu.Unlock()

	if !sessionActive {
		return true // 过期会话的探针，吞掉
	}
	if active {
		fmt.Printf("[%s] ★ P2P 直连建立: 本端 %s <-> 对端 %s（流量不再经过 coordinator）\n",
			p.id, p.conn.LocalAddr(), from)
		go p.sendText(fmt.Sprintf("hello from %s (via direct UDP)", p.id))
	} else if connected {
		_, _ = p.conn.WriteToUDP(wire.Punch(session, 0), from)
	}
	return true
}

func (p *Peer) handleData(from *net.UDPAddr, line string) bool {
	session, text, ok := wire.ParseData(line)
	if !ok {
		return false
	}
	p.mu.Lock()
	mine := p.connected && p.session == session
	p.mu.Unlock()
	if mine {
		fmt.Printf("[%s] << %s\n", p.id, text)
	}
	return true
}

// sendText 直连通道上发一条消息。
func (p *Peer) sendText(text string) {
	p.mu.Lock()
	session, remote, connected := p.session, p.remote, p.connected
	p.mu.Unlock()
	if !connected {
		fmt.Printf("[%s] (尚未直连，消息未发送: %s)\n", p.id, text)
		return
	}
	_, _ = p.conn.WriteToUDP(wire.Data(session, text), remote)
}

// regLoop REG 保活：让 coordinator 持续观测本端地址，同时保持 NAT 映射活跃。
func (p *Peer) regLoop() {
	coord, err := net.ResolveUDPAddr("udp", p.coordUDP)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid coordinator udp addr:", err)
		os.Exit(1)
	}
	ticker := time.NewTicker(regInterval)
	defer ticker.Stop()
	send := func() { _, _ = p.conn.WriteToUDP(wire.Reg(p.id), coord) }
	send()
	for {
		select {
		case <-p.runCtx.Done():
			return
		case <-ticker.C:
			send()
		}
	}
}

// punchLoop 对对端公网地址狂发探针。会话被新牵线顶替时自动退出。
func (p *Peer) punchLoop(session uint64, announced *net.UDPAddr) {
	fmt.Printf("[%s] 开始打洞: session=%d 对端=%s（每 %v 一发，超时 %v）\n",
		p.id, session, announced, punchInterval, punchTimeout)
	ticker := time.NewTicker(punchInterval)
	defer ticker.Stop()
	deadline := time.After(punchTimeout)
	for seq := uint64(1); ; seq++ {
		select {
		case <-p.runCtx.Done():
			return
		case <-deadline:
			p.mu.Lock()
			p.punching = false
			p.mu.Unlock()
			fmt.Printf("[%s] ✗ 打洞失败（%v 内未收到对端探针）。\n"+
				"    常见原因：对端/本端是 Symmetric NAT（映射按目的地址变化）或 UDP 被封。\n"+
				"    生产实现应回落云端中继（见 tools/tunnel 的 relay/agent）。\n", p.id, punchTimeout)
			return
		case <-ticker.C:
			p.mu.Lock()
			done := p.connected || p.session != session
			p.mu.Unlock()
			if done {
				return
			}
			_, _ = p.conn.WriteToUDP(wire.Punch(session, seq), announced)
		}
	}
}

// onIntro 收到 coordinator 的引见推送：记录会话并开打。
func (p *Peer) onIntro(in wire.Intro) {
	addr, err := net.ResolveUDPAddr("udp", in.PeerAddr)
	if err != nil {
		fmt.Printf("[%s] 引见报文地址非法: %v\n", p.id, err)
		return
	}
	p.mu.Lock()
	p.session = in.Session
	p.punching = true
	p.connected = false
	p.remote = nil
	p.mu.Unlock()
	fmt.Printf("[%s] coordinator 牵线成功: 对端 %s @ %s\n", p.id, in.PeerID, in.PeerAddr)
	go p.punchLoop(in.Session, addr)
}

// ---- 信令面（sloth ws）----

// tryRegister 登记（带重试），OnReady 每次连接建立都会触发。
func (p *Peer) tryRegister() {
	if p.registered.Load() || p.runCtx.Err() != nil {
		return
	}
	p.regMu.Lock()
	if p.registering {
		p.regMu.Unlock()
		return
	}
	p.registering = true
	p.regMu.Unlock()
	defer func() {
		p.regMu.Lock()
		p.registering = false
		p.regMu.Unlock()
	}()

	ctx := p.runCtx
	payload := tlv.Json(regReq{ID: p.id})
	delay := time.Second
	for i := 0; ; i++ {
		if p.registered.Load() || ctx.Err() != nil {
			return
		}
		if _, err := p.client.Call(ctx, "punch.Register", payload); err == nil {
			p.registered.Store(true)
			fmt.Printf("[%s] 已向 coordinator 登记\n", p.id)
			return
		} else { //nolint:revive // 对称结构更直观
			sloth.Errorw(ctx, "register failed, will retry",
				"peer", p.id, "attempt", i+1, "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// connectLoop 发起牵线（-connect 指定了对端时）。要求本端地址已被观测。
func (p *Peer) connectLoop(target string) {
	ctx := p.runCtx
	for i := 0; i < connectRetries; i++ {
		if ctx.Err() != nil {
			return
		}
		p.mu.Lock()
		public, reg := p.public, p.registered.Load()
		p.mu.Unlock()
		if reg && public != "" {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
	payload := tlv.Json(connectReq{From: p.id, To: target})
	for i := 0; i < connectRetries; i++ {
		if ctx.Err() != nil {
			return
		}
		resp, err := p.client.Call(ctx, "punch.Connect", payload)
		if err == nil {
			var cr connectResp
			if e := tlv.Json2Struct(resp, &cr); e == nil && cr.Err == "" {
				fmt.Printf("[%s] 牵线请求已受理: session=%d 对端=%s\n",
					p.id, cr.Session, cr.PeerAddr)
				return
			}
		}
		fmt.Printf("[%s] 牵线未成功（第 %d 次）: %v，重试...\n", p.id, i+1, err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
	fmt.Printf("[%s] 放弃牵线: 对端 %s\n", p.id, target)
}

type regReq struct {
	ID string `json:"id"`
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

// ---- sloth 连接生命周期钩子 ----

type peerHandler struct {
	p *Peer
}

func (h *peerHandler) OnConnect(ctx context.Context, resp *http.Response) error {
	return nil
}

func (h *peerHandler) OnReady(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo) error {
	go h.p.tryRegister()
	return nil
}

func (h *peerHandler) OnData(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo, msgType int, msg []byte) error {
	// coordinator 的推送在 wire 上是 message.Msg 的 JSON 信封，先解封。
	raw, err := proto.UnwrapEnvelope(msg)
	if err != nil {
		return nil
	}
	var in wire.Intro
	if json.Unmarshal(raw, &in) == nil && in.Session > 0 && in.PeerAddr != "" {
		h.p.onIntro(in)
	}
	return nil
}

func (h *peerHandler) OnClose(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo) error {
	// 断线重连后 OnReady 会重新登记；直连是独立 UDP，不受 ws 断连影响。
	h.p.registered.Store(false)
	return nil
}

func (h *peerHandler) OnError(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo, err error) error {
	h.p.registered.Store(false)
	return nil
}

// ---- stdin 聊天 ----

func (p *Peer) stdinLoop() {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		p.sendText(line)
	}
}

func main() {
	server := flag.String("server", "ws://127.0.0.1:9100", "coordinator 的 ws 信令地址")
	coordUDP := flag.String("coord", "127.0.0.1:9101", "coordinator 的 UDP 观测地址")
	id := flag.String("id", "", "本端 ID（必填）")
	connect := flag.String("connect", "", "主动连的对端 ID（留空则只等待被连）")
	flag.Parse()
	if *id == "" {
		fmt.Fprintln(os.Stderr, "-id is required")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sloth.SetLogLevel("info")

	uconn, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen udp:", err)
		os.Exit(1)
	}
	defer uconn.Close()

	p := &Peer{
		id:       *id,
		coordUDP: *coordUDP,
		conn:     uconn,
		runCtx:   ctx,
	}

	client := sloth.DefaultClient()
	// 与 tunnel agent 同理：恒等 codec，避免 ag/TLV 双层包装搅乱报文
	// （详见 tools/tunnel/agent/main.go 的注释）。
	client.SetEncoder(func(v any) ([]byte, error) {
		b, ok := v.([]byte)
		if !ok {
			return nil, fmt.Errorf("p2p peer: identity encoder only accepts []byte, got %T", v)
		}
		return b, nil
	})
	client.SetDecoder(func(b []byte) ([]byte, error) { return b, nil })
	p.client = client

	go p.udpLoop()
	go p.regLoop()
	go p.stdinLoop()

	conn := sloth.ClientConn(client)
	go func() {
		if err := conn.Dial(ctx, sloth.WS, *server,
			option.WithClientHandleMessage(&peerHandler{p: p})); err != nil && ctx.Err() == nil {
			sloth.Errorw(ctx, "dial coordinator exited with error", "err", err)
			cancel()
		}
	}()

	fmt.Printf("[%s] peer started: 本地 UDP %s，coordinator ws=%s udp=%s\n",
		p.id, uconn.LocalAddr(), *server, *coordUDP)
	if *connect != "" {
		go p.connectLoop(*connect)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	fmt.Printf("[%s] bye\n", p.id)
	cancel()
	_ = conn.Close()
	_ = uconn.Close()
}
