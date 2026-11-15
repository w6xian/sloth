package sloth

// p2p 打洞的桥接层：把 p2p 包（纯引擎）与 sloth 的 ws 信令客户端接起来。
//
// 为什么放在根包：打洞需要拨 coordinator 的 ws 信令（sloth 的 RPC 客户端），
// 而 p2p 包不能 import 根包（根包要 import p2p）——所以信令实现以
// p2p.Signaler 接口注入，本文件是唯一实现。
//
// 信令链路复用 tools/tunnel 踩平的三个框架坑：
//  1. codec 设恒等（SetEncoder/SetDecoder），报文原样上线；
//  2. RPC 报文用 tlv.Json/Json2Struct 对称编解码（TLV+CRC）；
//  3. coordinator 的推送（Intro）在 wire 上是 message.Msg 的 JSON 信封
//     （{"type":1,"body":"<base64>"}），OnData 收到后先解封。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"

	"github.com/w6xian/sloth/v4/codec"
	"github.com/w6xian/sloth/v4/decoder/ag"
	"github.com/w6xian/sloth/v4/logger"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/p2p"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/sloth/v4/types/handler"
	"github.com/w6xian/tlv"
)

// ── 选项解析 ─────────────────────────────────────────────────────────

// optCollector 收集 ConnectOption 里的 p2p 相关配置。
// 实现 IConnectOption 全部方法（都是空实现），再按可选接口补 p2p 项。
type optCollector struct {
	coords []option.Coordinate
	workID string
	token  string
}

func (c *optCollector) SetUriPath(path string) error { return nil }
func (c *optCollector) SetRouter(router *mux.Router) error {
	return nil
}
func (c *optCollector) SetAddress(address string) error { return nil }
func (c *optCollector) SetServerHandleMessage(h handler.IServerHandleMessage) error {
	return nil
}
func (c *optCollector) SetClientHandleMessage(h handler.IClientHandleMessage) error {
	return nil
}
func (c *optCollector) SetHeader(key string, value string) error { return nil }
func (c *optCollector) SetOrigin(origins ...string) error        { return nil }
func (c *optCollector) SetCodec(cd codec.Codec)                  {}

// SetCoordinates p2p：coordinator 信令面（可选接口，经 WithCoordinate 注入）。
// 多次 WithCoordinate 是追加而不是覆盖：ws + udp 两条坐标都要留下。
func (c *optCollector) SetCoordinates(coords []option.Coordinate) {
	c.coords = append(c.coords, coords...)
}

// SetWorkID p2p：本端身份（可选接口，经 WithWorkID 注入）。
func (c *optCollector) SetWorkID(id, token string) { c.workID, c.token = id, token }

// resolveP2POpts 从选项里提取 p2p 配置。
func resolveP2POpts(opts []option.ConnectOption) (coords []option.Coordinate, workID, token string) {
	c := &optCollector{}
	for _, o := range opts {
		o(c)
	}
	return c.coords, c.workID, c.token
}

// coordAddress 从坐标列表里取指定网络（如 ws）的 coordinator 地址；
// 没有匹配就取第一条（调用方通常只传一条，或 ws+udp 两条）。
func coordAddress(coords []option.Coordinate, network string) string {
	if len(coords) == 0 {
		return ""
	}
	for _, c := range coords {
		if c.Network == network {
			return c.Address
		}
	}
	return coords[0].Address
}

// punchedConnOption 把已打洞的 socket 注入传输实例（内部选项，
// QUIC 客户端按可选接口接收，其他传输忽略）。
func punchedConnOption(pc net.PacketConn, remote net.Addr) option.ConnectOption {
	return func(s option.IConnectOption) {
		if v, ok := s.(interface{ SetPunchedConn(net.PacketConn, net.Addr) }); ok {
			v.SetPunchedConn(pc, remote)
		}
	}
}

// ── 信令实现：p2p.Signaler over sloth ws ─────────────────────────────

// punch 信令的 TLV 载荷（与 coordinator 服务端对称）。
type punchRegReq struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

type punchRegResp struct {
	OK       bool   `json:"ok"`
	ID       string `json:"id"`        // 匿名登记时是 coordinator 分配的 id
	CoordUDP string `json:"coord_udp"` // coordinator 的 UDP 观测地址
}

type punchConnectReq struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type punchConnectResp struct {
	Session  uint64 `json:"session"`
	PeerAddr string `json:"peer_addr"`
}

// punchSignaler p2p.Signaler 的 sloth ws 实现。
type punchSignaler struct {
	client  *ServerRpc
	coordWS string // coordinator 的 ws 地址：coordUDP 缺主机时用它补（兼容旧版）
	intro   atomic.Pointer[func(p2p.Intro)]

	mu       sync.Mutex
	regID    string // 最近一次 Register 的 id/token：ws 重连后 OnReady 自动重登
	regToken string
}

func (s *punchSignaler) OnIntro(fn func(p2p.Intro)) { s.intro.Store(&fn) }

// normalizeCoordUDP 旧版 coordinator 的 regResp.coord_udp 是 `-udp` 参数
// 原样回传（如 ":9101"，只有端口没有主机），resolve 出来 IP 为空，
// sendto 直接报 "address is not valid in its context"。这里用 ws 地址的
// 主机补齐；新版 coordinator 回传完整地址，原样通过。
func normalizeCoordUDP(coordUDP, coordWS string) string {
	if coordUDP == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(coordUDP)
	if err != nil || host != "" {
		return coordUDP // 已带主机（或无法解析，交由引擎报错）
	}
	wsHost, _, err := net.SplitHostPort(strings.TrimPrefix(coordWS, "ws://"))
	if err != nil || wsHost == "" {
		return coordUDP
	}
	return net.JoinHostPort(wsHost, port)
}

func (s *punchSignaler) fireIntro(in p2p.Intro) {
	if fn := s.intro.Load(); fn != nil && *fn != nil {
		(*fn)(in)
	}
}

// Register 登记本端（重试到成功或 ctx 结束：ws 可能还在拨号/重连中）。
func (s *punchSignaler) Register(ctx context.Context, id, token string) (string, string, error) {
	s.mu.Lock()
	s.regID, s.regToken = id, token
	s.mu.Unlock()

	var lastErr error
	for i := 0; i < 30; i++ {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		resp, err := s.client.Call(ctx, "punch.Register", tlv.Json(punchRegReq{ID: id, Token: token}))
		if err == nil {
			var out punchRegResp
			if err := tlv.Json2Struct(resp, &out); err != nil {
				return "", "", fmt.Errorf("p2p: bad register response: %w", err)
			}
			if !out.OK {
				return "", "", fmt.Errorf("p2p: register rejected (id=%q token mismatch?)", id)
			}
			return out.ID, normalizeCoordUDP(out.CoordUDP, s.coordWS), nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return "", "", fmt.Errorf("p2p: register did not settle: %w", lastErr)
}

// reRegister ws 重连后自动重登（handler.OnReady 触发）。
func (s *punchSignaler) reRegister() {
	s.mu.Lock()
	id, token := s.regID, s.regToken
	s.mu.Unlock()
	if id == "" {
		return // 还没登记过（匿名客户端重连由下次打洞流程处理）
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp, err := s.client.Call(ctx, "punch.Register", tlv.Json(punchRegReq{ID: id, Token: token}))
	if err != nil {
		logger.Warnw(ctx, "p2p re-register failed (will retry on next reconnect)", "err", err)
		return
	}
	var out punchRegResp
	if err := tlv.Json2Struct(resp, &out); err != nil || !out.OK {
		logger.Warnw(ctx, "p2p re-register rejected", "err", err)
	}
}

// Connect 请求牵线。
func (s *punchSignaler) Connect(ctx context.Context, from, to string) (uint64, string, error) {
	resp, err := s.client.Call(ctx, "punch.Connect", tlv.Json(punchConnectReq{From: from, To: to}))
	if err != nil {
		return 0, "", err
	}
	var out punchConnectResp
	if err := tlv.Json2Struct(resp, &out); err != nil {
		return 0, "", fmt.Errorf("p2p: bad connect response: %w", err)
	}
	if out.Session == 0 || out.PeerAddr == "" {
		return 0, "", fmt.Errorf("p2p: coordinator returned empty session/addr")
	}
	return out.Session, out.PeerAddr, nil
}

// punchBridgeHandler ws 客户端钩子：收 coordinator 推的 Intro + 重连重登。
type punchBridgeHandler struct {
	sig *punchSignaler
}

func (h *punchBridgeHandler) OnConnect(ctx context.Context, resp *http.Response) error {
	return nil
}

func (h *punchBridgeHandler) OnReady(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo) error {
	go h.sig.reRegister()
	return nil
}

func (h *punchBridgeHandler) OnData(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo, msgType int, msg []byte) error {
	raw, err := unwrapPunchPush(msg)
	if err != nil {
		return nil
	}
	var in p2p.Intro
	if json.Unmarshal(raw, &in) == nil && in.Session > 0 && in.PeerAddr != "" {
		h.sig.fireIntro(in)
	}
	return nil
}

func (h *punchBridgeHandler) OnClose(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo) error {
	return nil // 重连由框架托管，重登由 OnReady 触发
}

func (h *punchBridgeHandler) OnError(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo, err error) error {
	logger.Warnw(ctx, "p2p signaling connection error", "err", err)
	return nil
}

// unwrapPunchPush 从 ws 推送的 JSON 信封里还原 Intro 报文。
// 兼容两种形态：直接是 JSON、JSON 信封 body=base64。
func unwrapPunchPush(b []byte) ([]byte, error) {
	var env struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(b, &env); err != nil || env.Body == "" {
		return b, nil // 已经是裸 JSON
	}
	raw, err := base64.StdEncoding.DecodeString(env.Body)
	if err != nil {
		return nil, err
	}
	// 兜底：body 外若再套一层 ag 帧（框架版本差异），剥掉
	if len(raw) >= 2 && raw[0] == ag.ArgumentMagic1 && raw[1] == ag.ArgumentMagic2 {
		if v, err := ag.Decoder(raw); err == nil && len(v) > 0 {
			return v, nil
		}
	}
	return raw, nil
}

// dialCoordinator 拨 coordinator 的 ws 信令（后台长驻，断线自动重连），
// 返回实现 p2p.Signaler 的信令端。
// codec 设恒等：报文原样上线，避免 ag/TLV 双层包装搅乱 TLV 帧
// （与 tools/tunnel/agent 同一套处理，见那里更完整的注释）。
func dialCoordinator(ctx context.Context, coordAddr string) (*punchSignaler, error) {
	if !containsScheme(coordAddr) {
		coordAddr = "ws://" + coordAddr
	}
	client := DefaultClient()
	client.SetEncoder(func(v any) ([]byte, error) {
		b, ok := v.([]byte)
		if !ok {
			return nil, fmt.Errorf("p2p signaling: identity encoder only accepts []byte, got %T", v)
		}
		return b, nil
	})
	client.SetDecoder(func(b []byte) ([]byte, error) { return b, nil })

	sig := &punchSignaler{client: client, coordWS: coordAddr}
	conn := ClientConn(client)
	go func() {
		err := conn.Dial(ctx, WS, coordAddr,
			option.WithClientHandleMessage(&punchBridgeHandler{sig: sig}))
		if err != nil && ctx.Err() == nil {
			logger.Errorw(ctx, "p2p signaling connection exited", "coord", coordAddr, "err", err)
		}
	}()
	return sig, nil
}

func containsScheme(addr string) bool {
	for i := 0; i+2 < len(addr); i++ {
		if addr[i] == ':' && addr[i+1] == '/' && addr[i+2] == '/' {
			return true
		}
	}
	return false
}

// ── 编排入口（Listen / Dial 各走一个）────────────────────────────────

// punchServeSocket 服务端 p2p 编排：bind UDP socket → 拨 coordinator →
// 登记 + 观测地址 → REG 保活 + Intro 监听（后台长驻）。
// 返回的 socket 已让出读权，调用方立刻交给 QUIC Listen。
func punchServeSocket(ctx context.Context, listenAddr, coordAddr, workID, token string) (*net.UDPConn, error) {
	if workID == "" {
		return nil, fmt.Errorf("p2p listen requires WithWorkID(id, token): clients locate this peer by its work-id")
	}
	sock, err := net.ListenUDP("udp", resolveUDPAddrOrAny(listenAddr))
	if err != nil {
		return nil, fmt.Errorf("p2p: bind %s: %w", listenAddr, err)
	}
	sig, err := dialCoordinator(ctx, coordAddr)
	if err != nil {
		_ = sock.Close()
		return nil, err
	}
	sess, err := p2p.Establish(ctx, sock, sig, p2p.Options{ID: workID, Token: token})
	if err != nil {
		_ = sock.Close()
		return nil, err
	}
	if sess.Observed != nil {
		keepCtx := context.WithoutCancel(ctx)
		if uaddr, err := net.ResolveUDPAddr("udp", sess.CoordUDP); err == nil {
			go p2p.Keepalive(keepCtx, sock, uaddr, sess.AssignedID)
		}
	}
	go p2p.WatchIntros(context.WithoutCancel(ctx), sock, sig)
	Infow(ctx, "p2p ready",
		"work_id", sess.AssignedID, "local", sock.LocalAddr().String(),
		"observed", observedStr(sess.Observed))
	return sock, nil
}

// punchDialSocket 客户端 p2p 编排：bind 随机端口 → 拨 coordinator →
// 匿名登记 → 请求与 target 牵线 → 探针打洞。
// 返回打了洞的 socket 与对端直连地址。
func punchDialSocket(ctx context.Context, coordAddr, target, workID, token string) (*net.UDPConn, *net.UDPAddr, error) {
	sock, err := net.ListenUDP("udp", resolveUDPAddrOrAny(""))
	if err != nil {
		return nil, nil, fmt.Errorf("p2p: bind local udp: %w", err)
	}
	sig, err := dialCoordinator(ctx, coordAddr)
	if err != nil {
		_ = sock.Close()
		return nil, nil, err
	}
	remote, sess, err := p2p.DialPunch(ctx, sock, sig, p2p.Options{ID: workID, Token: token, Target: target})
	if err != nil {
		_ = sock.Close()
		return nil, nil, err
	}
	Infow(ctx, "p2p punched through",
		"target", target, "remote", remote.String(), "id", sess.AssignedID)
	return sock, remote, nil
}

// splitP2PAddr 解析 p2p 拨号地址："对端ID@coordinator地址"。
// 不含 @ 返回 false（普通直连地址，走原路径）。
func splitP2PAddr(address string) (target, coord string, ok bool) {
	for i := 0; i < len(address); i++ {
		if address[i] == '@' && i > 0 && i < len(address)-1 {
			return address[:i], address[i+1:], true
		}
	}
	return "", "", false
}

func resolveUDPAddrOrAny(addr string) *net.UDPAddr {
	if addr == "" {
		return nil
	}
	if uaddr, err := net.ResolveUDPAddr("udp", addr); err == nil {
		return uaddr
	}
	return nil
}

func observedStr(a *net.UDPAddr) string {
	if a == nil {
		return "(unobserved)"
	}
	return a.String()
}
