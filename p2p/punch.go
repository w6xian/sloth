// Package p2p sloth 内置的 NAT 打洞引擎（信令 + UDP 直连）。
//
// 三端模型（coordinator 只做信令，流量不经它）：
//
//	peer ── ws 信令(punch.Register/Connect) ──▶ coordinator（固定公网 IP）
//	peer ── REG 心跳(UDP) ────────────────────▶ coordinator 观测公网地址
//	coordinator ── Intro 推送(ws) ──▶ 双方互发 PUNCH 探针 → 直连建立
//
// 打洞的核心前提：peer 用 **同一个 UDP socket** 既跟 coordinator 保持 REG
// 心跳、又对对方公网地址发 PUNCH 探针——cone 类 NAT 对同一 socket 的出站
// 映射是稳定的，coordinator 观测到的地址即对端可直达地址。Symmetric NAT
// 每个目的地址映射都不同，观测地址失效，打不通（应回落云端中继）。
//
// socket 归属（与 QUIC 传输衔接的关键约定）：
//   - socket 由调用方创建并传入；
//   - Establish/PunchOut 阶段独占读（收 ACK / 对端探针）；
//   - 两者返回后调用方即可把 socket 交给 QUIC/KCP 接管（Transport 接管后
//     读走全部数据报），此后本包只**写**不读——REG 保活与 PUNCH 探针都是
//     纯写，ACK/PUNCH 回包无人读也无碍（UDP 无连接语义）。
package p2p

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// ── 可调参数（默认值对公网部署已够用）────────────────────────────────

const (
	RegEvery      = 2 * time.Second        // REG 心跳间隔（刷新 NAT 映射 + 观测地址）
	ProbeEvery    = 150 * time.Millisecond // PUNCH 探针间隔
	PunchTimeout  = 20 * time.Second       // 打洞总超时
	ProbeBurstFor = 15 * time.Second       // 服务端收到 Intro 后的探针时长（覆盖对端建链窗口）
	RegAckTimeout = 10 * time.Second       // 首次 ACK 等待上限
)

// ErrPunchFailed 打洞失败（Symmetric NAT / UDP 被防火墙拦截）。
// 调用方应回落云端中继。
var ErrPunchFailed = errors.New("p2p: punch failed (symmetric NAT or UDP blocked?)")

// Intro coordinator 经 ws 信令推给双方的引见报文。
//
// JSON 字段名与 coordinator 服务端约定一致（session/peer_id/peer_addr）。
type Intro struct {
	Session  uint64 `json:"session"`   // 本次牵线的会话号
	PeerID   string `json:"peer_id"`   // 对端 ID
	PeerAddr string `json:"peer_addr"` // 对端公网 UDP 地址（coordinator 观测）
}

// Options 打洞会话参数。
type Options struct {
	ID     string // 本端 work-id（服务端必填；客户端可空 → coordinator 分配匿名 id）
	Token  string // id 的接管凭据（id 由 coordinator 管理时必填）
	Target string // 要连接的对端 id（仅客户端角色使用）
}

// Session 一次打洞会话的产物。
type Session struct {
	AssignedID string       // coordinator 分配的实际 id（匿名时与传入不同）
	CoordUDP   string       // coordinator 的 UDP 观测地址（保活目标）
	Observed   *net.UDPAddr // 本端公网地址（coordinator 观测）
}

// Signaler 信令面抽象：与 coordinator 的 ws 交互由宿主（sloth 根包桥接层）
// 实现，本包不依赖任何传输——保持纯逻辑、可独立测试。
type Signaler interface {
	// Register 登记本端；id 为空时 coordinator 分配一次性匿名 id。
	// 返回 (实际 id, coordinator 的 UDP 观测地址)。
	Register(ctx context.Context, id, token string) (assigned string, coordUDP string, err error)

	// Connect 请求与 to 牵线。返回会话号与 coordinator 观测到的对端公网地址。
	Connect(ctx context.Context, from, to string) (session uint64, peerAddr string, err error)

	// OnIntro 注册 Intro 回调（coordinator 经 ws 推送，长驻连接持续触发）。
	OnIntro(fn func(Intro))
}

// ── UDP 线上协议（P2P1 文本帧，与 coordinator 共用）──────────────────

const proto = "P2P1"

const (
	prefixReg   = proto + " REG "   // peer → coordinator：登记/保活本端 UDP 地址
	prefixAck   = proto + " ACK "   // coordinator → peer：回观测到的公网地址
	prefixPunch = proto + " PUNCH " // 双方互发的打洞探针
)

func punchPacket(session uint64) []byte {
	return []byte(fmt.Sprintf("%s%d 0", prefixPunch, session))
}

// parsePunch 解析探针，返回会话号。
func parsePunch(s string) (uint64, bool) {
	rest, ok := trimPrefix(s, prefixPunch)
	if !ok {
		return 0, false
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return 0, false
	}
	session, err := strconv.ParseUint(fields[0], 10, 64)
	return session, err == nil
}

func trimPrefix(s, prefix string) (string, bool) {
	if !strings.HasPrefix(s, prefix) {
		return "", false
	}
	return strings.TrimSpace(s[len(prefix):]), true
}

// ── 引擎 ─────────────────────────────────────────────────────────────

// Establish 登记本端并完成首次地址观测（独占读 socket）。
//
// 流程：ws Register（重试到成功）→ UDP REG 循环（等 coordinator 回 ACK，
// 拿到本端公网地址）。
func Establish(ctx context.Context, sock *net.UDPConn, sig Signaler, opt Options) (*Session, error) {
	assigned, coordUDP, err := sig.Register(ctx, opt.ID, opt.Token)
	if err != nil {
		return nil, fmt.Errorf("p2p: register: %w", err)
	}
	s := &Session{AssignedID: assigned, CoordUDP: coordUDP}
	if coordUDP == "" {
		// coordinator 没给观测面：跳过 UDP 环节（直连无从谈起，交上层处理）
		return s, nil
	}
	uaddr, err := net.ResolveUDPAddr("udp", coordUDP)
	if err != nil {
		return nil, fmt.Errorf("p2p: bad coordinator udp addr %q: %w", coordUDP, err)
	}
	observed, err := waitObserved(ctx, sock, uaddr, assigned)
	if err != nil {
		return nil, err
	}
	s.Observed = observed
	return s, nil
}

// waitObserved 发 REG 直到 coordinator 回 ACK（独占读窗口）。
func waitObserved(ctx context.Context, sock *net.UDPConn, coord *net.UDPAddr, id string) (*net.UDPAddr, error) {
	deadline := time.Now().Add(RegAckTimeout)
	buf := make([]byte, 1500)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if _, err := sock.WriteToUDP([]byte(prefixReg+id), coord); err != nil {
			return nil, fmt.Errorf("p2p: send reg: %w", err)
		}
		// ACK 可能在路上：1s 短窗口内等回包，没有就再发一轮 REG
		wait := time.Now().Add(time.Second)
		_ = sock.SetReadDeadline(wait)
		for time.Now().Before(wait) {
			n, _, err := sock.ReadFromUDP(buf)
			if err != nil {
				break // 窗口到，重新 REG
			}
			if addrStr, ok := trimPrefix(string(buf[:n]), prefixAck); ok {
				_ = sock.SetReadDeadline(time.Time{})
				return net.ResolveUDPAddr("udp", addrStr)
			}
			// 非 ACK 报文（可能是并发的 PUNCH 探针）忽略，继续读
		}
		_ = sock.SetReadDeadline(time.Time{})
		time.Sleep(time.Second)
	}
	return nil, fmt.Errorf("p2p: coordinator did not ack registration for %q", id)
}

// Keepalive 周期重发 REG（只写不读——socket 可能已被 QUIC 接管）。
// 刷新 NAT 映射与 coordinator 侧的在线状态。
func Keepalive(ctx context.Context, sock *net.UDPConn, coord *net.UDPAddr, id string) {
	ticker := time.NewTicker(RegEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = sock.WriteToUDP([]byte(prefixReg+id), coord)
		}
	}
}

// PunchOut 对 peerAddr 发探针直到收到对方的 PUNCH（确认洞已凿开）。
// 独占读窗口；返回后调用方即可把 socket 交给 QUIC Dial。
// 以实际来源地址为准（而非宣告地址）：先收到对方探针即确认直连。
func PunchOut(ctx context.Context, sock *net.UDPConn, session uint64, peerAddr string) (*net.UDPAddr, error) {
	remote, err := net.ResolveUDPAddr("udp", peerAddr)
	if err != nil {
		return nil, fmt.Errorf("p2p: bad peer addr %q: %w", peerAddr, err)
	}
	deadline := time.Now().Add(PunchTimeout)
	buf := make([]byte, 1500)
	packet := punchPacket(session)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if _, err := sock.WriteToUDP(packet, remote); err != nil {
			return nil, fmt.Errorf("p2p: send punch: %w", err)
		}
		wait := time.Now().Add(ProbeEvery)
		_ = sock.SetReadDeadline(wait)
		for time.Now().Before(wait) {
			n, from, err := sock.ReadFromUDP(buf)
			if err != nil {
				break
			}
			if s2, ok := parsePunch(string(buf[:n])); ok && s2 == session {
				_ = sock.SetReadDeadline(time.Time{})
				return from, nil
			}
		}
		_ = sock.SetReadDeadline(time.Time{})
	}
	return nil, ErrPunchFailed
}

// WatchIntros 服务端长驻循环：coordinator 牵线时推送 Intro 过来，
// 对每个新对端发起一段探针突发（为对端的 QUIC 握手包凿开本端 NAT 的
// 入站许可）。探针纯写，不与 QUIC 抢 socket。
func WatchIntros(ctx context.Context, sock *net.UDPConn, sig Signaler) {
	introCh := make(chan Intro, 8)
	sig.OnIntro(func(in Intro) { introCh <- in })
	for {
		select {
		case <-ctx.Done():
			return
		case in := <-introCh:
			remote, err := net.ResolveUDPAddr("udp", in.PeerAddr)
			if err != nil {
				continue
			}
			go probeBurst(ctx, sock, in.Session, remote)
		}
	}
}

// probeBurst 对单个对端的探针突发：持续 ProbeBurstFor 后停手。
func probeBurst(ctx context.Context, sock *net.UDPConn, session uint64, remote *net.UDPAddr) {
	packet := punchPacket(session)
	deadline := time.Now().Add(ProbeBurstFor)
	ticker := time.NewTicker(ProbeEvery)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = sock.WriteToUDP(packet, remote)
		}
	}
}

// DialPunch 客户端角色一步到位：Establish + Connect + PunchOut。
// 返回凿开的直连对端地址；socket 交回调用方给 QUIC Dial。
func DialPunch(ctx context.Context, sock *net.UDPConn, sig Signaler, opt Options) (*net.UDPAddr, *Session, error) {
	s, err := Establish(ctx, sock, sig, opt)
	if err != nil {
		return nil, s, err
	}
	session, peerAddr, err := sig.Connect(ctx, s.AssignedID, opt.Target)
	if err != nil {
		return nil, s, fmt.Errorf("p2p: connect to %q: %w", opt.Target, err)
	}
	remote, err := PunchOut(ctx, sock, session, peerAddr)
	return remote, s, err
}
