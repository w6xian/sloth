package main

// agent 核心逻辑。
//
// 角色：部署在家里能常开的机器上（NAS / 软路由 / 树莓派 / 任何电脑），
// 主动 ws 拨向云端 relay（出站连接，任何 NAT 都通），把内网服务挂到隧道上。
//
// 连接生命周期完全由 sloth 的 ws 客户端托管：内部自动重连，每次连接成功
// 触发 OnReady → agent 重新登记（tunnel.Register）；每次断开触发 OnClose →
// agent 拆掉全部本地连接。业务层只需在这两个时机做登记与清理。

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/tools/tunnel/proto"
	"github.com/w6xian/tlv"
)

// Agent agent 实例。
type Agent struct {
	cfg    Config
	client *sloth.ServerRpc
	runCtx context.Context

	mu      sync.Mutex
	conns   map[uint32]net.Conn // 本地（内网侧）连接，connID 与 relay 分配的一致
	targets map[string]string   // 隧道 ID → 本地目标
	modes   map[string]string   // 隧道 ID → 模式（tcp/udp），决定 OPEN 时怎么拨本地

	// 登记去重：重连风暴下 OnReady 可能连续触发，同一时刻只允许一个登记循环。
	regMu       sync.Mutex
	registering bool
	registered  atomic.Bool
}

// NewAgent 构造 agent。
func NewAgent(ctx context.Context, cfg Config, client *sloth.ServerRpc) (*Agent, error) {
	targets := make(map[string]string, len(cfg.Tunnels))
	modes := make(map[string]string, len(cfg.Tunnels))
	for _, tc := range cfg.Tunnels {
		if tc.ID == "" || tc.Token == "" || tc.Target == "" {
			return nil, fmt.Errorf("tunnel (target=%s): id, token and target are required", tc.Target)
		}
		if _, dup := targets[tc.ID]; dup {
			return nil, fmt.Errorf("tunnel id %q duplicated", tc.ID)
		}
		targets[tc.ID] = tc.Target
		modes[tc.ID] = tc.Mode
	}
	return &Agent{
		cfg:     cfg,
		client:  client,
		runCtx:  ctx,
		conns:   make(map[uint32]net.Conn),
		targets: targets,
		modes:   modes,
	}, nil
}

// registerPayload 生成登记报文（只带 id+token，target 不出内网）。
func (a *Agent) registerPayload() ([]byte, error) {
	type at struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	req := struct {
		Name    string `json:"name"`
		Tunnels []at   `json:"tunnels"`
	}{Name: a.cfg.Name}
	for _, tc := range a.cfg.Tunnels {
		req.Tunnels = append(req.Tunnels, at{ID: tc.ID, Token: tc.Token})
	}
	// 必须用 tlv.Json（TLV 帧）而不是裸 json.Marshal：
	// relay 侧 tlv.Json2Struct 只认 TLV 帧（带 CRC 校验）。
	return tlv.Json(req), nil
}

// tryRegister 登记循环：重试直到成功（或进程退出）。
// 每次连接成功（OnReady）调用；已在登记中或已登记则直接返回。
func (a *Agent) tryRegister() {
	a.regMu.Lock()
	if a.registering {
		a.regMu.Unlock()
		return
	}
	a.registering = true
	a.regMu.Unlock()
	defer func() {
		a.regMu.Lock()
		a.registering = false
		a.regMu.Unlock()
	}()

	ctx := a.runCtx
	payload, err := a.registerPayload()
	if err != nil {
		sloth.Errorw(ctx, "build register payload failed", "err", err)
		return
	}
	delay := time.Second
	for i := 0; ; i++ {
		if a.registered.Load() || ctx.Err() != nil {
			return
		}
		resp, err := a.client.Call(ctx, "tunnel.Register", payload)
		if err == nil {
			a.registered.Store(true)
			sloth.Infow(ctx, "tunnels registered", "agent", a.cfg.Name, "resp", string(resp))
			return
		}
		sloth.Errorw(ctx, "register failed, will retry",
			"agent", a.cfg.Name, "attempt", i+1, "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if delay < 30*time.Second {
			delay *= 2
		}
	}
}

// teardown 连接断开：清空登记状态、关闭全部本地连接。
// 在途数据的两端泵会因写失败/读失败自行退出。
func (a *Agent) teardown() {
	ctx := a.runCtx
	a.registered.Store(false)
	a.mu.Lock()
	conns := a.conns
	a.conns = make(map[uint32]net.Conn)
	a.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
	if len(conns) > 0 {
		sloth.Infow(ctx, "tunnels torn down", "agent", a.cfg.Name, "conns dropped", len(conns))
	}
}

// handleFrame 处理 relay 推来的帧：拨本地 / 写数据 / 收口。
func (a *Agent) handleFrame(ctx context.Context, b []byte) {
	op, id, payload, err := proto.Decode(b)
	if err != nil {
		sloth.Errorw(ctx, "bad frame from relay", "err", err)
		return
	}
	switch op {
	case proto.OpOpen:
		tunnelID := string(payload)
		target, ok := a.targets[tunnelID]
		if !ok || len(tunnelID) > proto.MaxTunnelIDLen {
			// relay 上有、本地没有的隧道：回 CLOSE 让访客立即失败
			sloth.Errorw(ctx, "unknown tunnel in OPEN", "tunnel", tunnelID, "conn", id)
			_ = a.client.Send(ctx, proto.Encode(proto.OpClose, id, nil))
			return
		}
		if a.modes[tunnelID] == "udp" {
			a.dialLocalUDP(ctx, id, target)
			return
		}
		a.dialLocal(ctx, id, target)
	case proto.OpData:
		a.mu.Lock()
		c := a.conns[id]
		a.mu.Unlock()
		if c == nil {
			// OPEN 丢失的兜底：没有隧道信息无法拨号，只能关掉
			_ = a.client.Send(ctx, proto.Encode(proto.OpClose, id, nil))
			return
		}
		if _, err := c.Write(payload); err != nil {
			a.closeLocal(ctx, id)
		}
	case proto.OpClose:
		a.closeLocal(ctx, id)
	default:
		sloth.Errorw(ctx, "unexpected op from relay", "op", op, "conn", id)
	}
}

// dialLocal 拨本地目标并起上行泵。登记表里的 connID 占位放在拨号前，
// 防止并发帧对同一 connID 重复拨号。
func (a *Agent) dialLocal(ctx context.Context, id uint32, target string) {
	a.mu.Lock()
	if _, ok := a.conns[id]; ok {
		a.mu.Unlock()
		return
	}
	conn, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		a.mu.Unlock()
		sloth.Errorw(ctx, "dial local failed", "target", target, "conn", id, "err", err)
		_ = a.client.Send(ctx, proto.Encode(proto.OpClose, id, nil))
		return
	}
	a.conns[id] = conn
	a.mu.Unlock()
	go a.pumpLocal(ctx, id, conn)
}

// dialLocalUDP 为 UDP 隧道拨一条"已连接"的本地 UDP socket（每个访客会话一条，
// 端口独立，报文语义与本地服务一对一保留）。读/写泵与 TCP 共用 pumpLocal：
// 已连接 UDP socket 的 Read/Write 就是按数据报语义进行的。
func (a *Agent) dialLocalUDP(ctx context.Context, id uint32, target string) {
	a.mu.Lock()
	if _, ok := a.conns[id]; ok {
		a.mu.Unlock()
		return
	}
	raddr, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		a.mu.Unlock()
		sloth.Errorw(ctx, "resolve local udp target failed", "target", target, "conn", id, "err", err)
		_ = a.client.Send(ctx, proto.Encode(proto.OpClose, id, nil))
		return
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		a.mu.Unlock()
		sloth.Errorw(ctx, "dial local udp failed", "target", target, "conn", id, "err", err)
		_ = a.client.Send(ctx, proto.Encode(proto.OpClose, id, nil))
		return
	}
	a.conns[id] = conn
	a.mu.Unlock()
	go a.pumpLocal(ctx, id, conn)
}

// pumpLocal 本地 → relay：读一块推一帧；EOF/错误时回 CLOSE 收口。
func (a *Agent) pumpLocal(ctx context.Context, id uint32, conn net.Conn) {
	defer a.closeLocal(ctx, id)
	buf := make([]byte, proto.ChunkSize)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			if err := a.client.Send(ctx, proto.Encode(proto.OpData, id, buf[:n])); err != nil {
				// 推送失败说明到 relay 的链路已断，等待重连后由访客重试
				sloth.Errorw(ctx, "send to relay failed", "conn", id, "err", err)
				return
			}
		}
		if err != nil {
			_ = a.client.Send(ctx, proto.Encode(proto.OpClose, id, nil))
			return
		}
	}
}

// closeLocal 关闭并移除一条本地连接。
func (a *Agent) closeLocal(ctx context.Context, id uint32) {
	a.mu.Lock()
	c := a.conns[id]
	delete(a.conns, id)
	a.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}
