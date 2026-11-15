package main

// coordinator 入口：三端打洞实例的"云端固定 IP"端。
//
// 职责全部在信令面（流量几乎为 0）：
//  1. ws（sloth，:9100）RPC 服务 punch.*：
//     - CreateID  生成 work-id + token 并持久化（ID 管理：客户端凭 id 连接，
//       服务端凭 id+token 接管）；
//     - Register  peer 上线登记（token 校验，常数时间比较；空 id 分配匿名 id）；
//     - Connect   牵线（把两端的公网 UDP 地址互推给对方）；
//     - List      全部 ID 与在线状态（管理面）。
//  2. UDP（:9101）：观测每个 peer 的公网 UDP 地址——STUN 的最小子集。
//     peer 用将来要打洞的同一个 socket 定期发 REG，coordinator 记下
//     观测地址并回 ACK 告知。
//
// 状态调整：ws 断开（OnClose）→ 立即离线；REG 心跳超时（peer 崩溃没来得及
// 断开）→ janitor 清离线。ID 台账（含 token）持久化在 -store 指定的文件，
// 重启不丢。
//
// 生成 ID：
//
//	./coordinator -create 生产环境A     # 打印 id+token 并写入台账
//
// 运行：
//
//	./coordinator -rpc :9100 -udp :9101 -store ids.json
//
// 本地联调（三端同一台机器，打洞必然成功，验证代码路径）见 README.md。

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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
	"time"

	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/tools/tunnel/examples/p2p/wire"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/tlv"
)

// ── ID 台账（生成 / 管理 / 持久化）───────────────────────────────────

// idRecord 台账里的一条 ID：谁申请的、接管凭据的哈希、何时创建。
// token 只存哈希——台账文件泄露也不会冒用别人的 id。
type idRecord struct {
	Name        string `json:"name"`         // 申请方备注（如"生产环境A"）
	TokenSHA256 string `json:"token_sha256"` // 接管凭据的 sha256
	CreatedUnix int64  `json:"created_unix"`
}

// idStore ID 台账：CreateID 生成、Register 校验、List 展示。
// 文件持久化，重启不丢。
type idStore struct {
	mu   sync.Mutex
	path string
	ids  map[string]*idRecord
}

func newIDStore(path string) (*idStore, error) {
	s := &idStore{path: path, ids: make(map[string]*idRecord)}
	if path == "" {
		return s, nil // 不持久化（联调场景）
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &s.ids); err != nil {
		return nil, fmt.Errorf("parse id store %s: %w", path, err)
	}
	return s, nil
}

func (s *idStore) save() {
	if s.path == "" {
		return
	}
	b, err := json.MarshalIndent(s.ids, "", "  ")
	if err != nil {
		return
	}
	// 原子写：先写临时文件再改名，崩溃不至于写坏台账
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		sloth.Errorw(context.Background(), "save id store", "err", err)
		return
	}
	_ = os.Rename(tmp, s.path)
}

// create 生成新 ID。返回 (id, token 明文一次)。
func (s *idStore) create(name string) (string, string, error) {
	id := "work-" + randHex(4)
	token := randHex(16)
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if _, dup := s.ids[id]; !dup {
			break
		}
		id = "work-" + randHex(4)
	}
	sum := sha256.Sum256([]byte(token))
	s.ids[id] = &idRecord{
		Name:        name,
		TokenSHA256: hex.EncodeToString(sum[:]),
		CreatedUnix: time.Now().Unix(),
	}
	s.save()
	return id, token, nil
}

// verify 校验 id 的接管凭据（常数时间比较；未登记的 id 由调用方决定放行）。
func (s *idStore) verify(id, token string) (ok bool, exists bool) {
	s.mu.Lock()
	rec, exists := s.ids[id]
	s.mu.Unlock()
	if !exists {
		return false, false
	}
	sum := sha256.Sum256([]byte(token))
	want, _ := hex.DecodeString(rec.TokenSHA256)
	return subtle.ConstantTimeCompare(sum[:], want) == 1, true
}

// list 台账快照（在线状态在展示时并入）。
func (s *idStore) list() map[string]*idRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]*idRecord, len(s.ids))
	for k, v := range s.ids {
		cp := *v
		out[k] = &cp
	}
	return out
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ── peer 登记表（运行时状态）─────────────────────────────────────────

// regStaleAfter REG 心跳多久没来就算离线（peer 崩溃没机会走 ws OnClose 时兜底）。
const regStaleAfter = 30 * time.Second

// peerEntry 一个 peer 的运行时状态。
type peerEntry struct {
	mu       sync.Mutex
	ch       bucket.IChannel // ws 信令通道（重连后会被 Register 替换；nil=离线）
	addr     *net.UDPAddr    // coordinator 观测到的公网 UDP 地址
	lastSeen atomic.Int64    // 最近一次 REG 的 unix 秒
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

func (e *peerEntry) online() bool {
	if e.channel() == nil {
		return false
	}
	last := e.lastSeen.Load()
	return last == 0 || time.Since(time.Unix(last, 0)) < regStaleAfter
}

// Hub 全部 peer 的登记表（含离线的，供 List 展示状态）。
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

// ensure 取（或建）一条 peer 状态。
func (h *Hub) ensure(id string) *peerEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.peers[id]
	if e == nil {
		e = &peerEntry{}
		h.peers[id] = e
	}
	return e
}

// dropChannel 连接断开：仅当该连接仍是登记的那条时才移除
// （重连后新连接已顶替，旧连接的 OnClose 不能误删新状态）。
// 只清通道不删台账：ID 的在线状态可由 List 观察。
func (h *Hub) dropChannel(ch bucket.IChannel) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, e := range h.peers {
		if e.channel() == ch {
			e.setChannel(nil)
			return id, true
		}
	}
	return "", false
}

// Coord coordinator 实例。
type Coord struct {
	hub          Hub
	store        *idStore
	coordUDPAddr *net.UDPAddr // 本端 UDP 观测地址（Register 响应里告知 peer）
}

func NewCoord(store *idStore) *Coord {
	return &Coord{store: store, hub: Hub{peers: make(map[string]*peerEntry)}}
}

// ── 信令面：RPC 服务（sloth，方法名前缀 punch.）─────────────────────

type regReq struct {
	ID    string `json:"id"`
	Token string `json:"token"` // 台账里的 id 必须携带；未登记 id 无需
}

type regResp struct {
	OK       bool   `json:"ok"`
	ID       string `json:"id"`        // 实际生效的 id（匿名登记时是分配值）
	CoordUDP string `json:"coord_udp"` // 本端 UDP 观测地址（框架从这里拿保活目标）
}

type createReq struct {
	Name string `json:"name"`
}

type createResp struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

type connectReq struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type connectResp struct {
	Session  uint64 `json:"session"`
	PeerAddr string `json:"peer_addr"`
}

type listReq struct{}

type listEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Online   bool   `json:"online"`
	LastSeen int64  `json:"last_seen"`
}

type listResp struct {
	Items []listEntry `json:"items"`
}

// PunchService peer 拨入后调用的 RPC 服务。
type PunchService struct {
	c *Coord
}

// CreateID 生成并登记一个新 ID（管理面）。token 明文只在创建时返回一次。
func (s *PunchService) CreateID(ctx context.Context, req []byte) ([]byte, error) {
	var cr createReq
	if err := tlv.Json2Struct(req, &cr); err != nil {
		return nil, fmt.Errorf("bad create payload: %w", err)
	}
	id, token, err := s.c.store.create(cr.Name)
	if err != nil {
		return nil, err
	}
	sloth.Infow(ctx, "work id created", "id", id, "name", cr.Name)
	return tlv.Json(createResp{ID: id, Token: token}), nil
}

// Register peer 上线登记。断线重连后 OnReady 会再次触发，幂等替换通道。
//
// 鉴权策略：
//   - 台账里的 id：token 必须匹配（常数时间比较），否则拒绝；
//   - 空 id：分配一次性匿名 id（客户端临时身份，无需申请）；
//   - 台账外的非空 id：放行并按临时 id 处理（兼容旧 demo，生产建议收紧）。
func (s *PunchService) Register(ctx context.Context, req []byte) ([]byte, error) {
	ch, err := sloth.GetChannel(ctx)
	if err != nil {
		return nil, err
	}
	var rr regReq
	if err := tlv.Json2Struct(req, &rr); err != nil {
		return nil, fmt.Errorf("bad register payload: %w", err)
	}

	id := rr.ID
	switch {
	case id == "":
		id = "anon-" + randHex(4) // 客户端临时身份
	default:
		if ok, exists := s.c.store.verify(rr.ID, rr.Token); exists && !ok {
			sloth.Warnw(ctx, "register rejected: bad token", "peer", rr.ID)
			return nil, fmt.Errorf("bad token for work id %q", rr.ID)
		}
	}

	e := s.c.hub.ensure(id)
	e.setChannel(ch)
	sloth.Infow(ctx, "peer online", "peer", id)
	return tlv.Json(regResp{OK: true, ID: id, CoordUDP: s.c.coordUDPStr()}), nil
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
	if to == nil || to.channel() == nil {
		return nil, fmt.Errorf("peer %q is offline", cr.To)
	}
	addrFrom, addrTo := from.udpAddr(), to.udpAddr()
	if addrFrom == nil {
		return nil, fmt.Errorf("peer %q udp address not observed yet (wait for REG)", cr.From)
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

// List 全部 ID 与在线状态（管理面：台账里的 + 运行时出现过的）。
func (s *PunchService) List(ctx context.Context, req []byte) ([]byte, error) {
	if err := tlv.Json2Struct(req, &listReq{}); err != nil {
		return nil, fmt.Errorf("bad list payload: %w", err)
	}
	records := s.c.store.list()
	resp := listResp{Items: make([]listEntry, 0, len(records)+8)}
	for id, rec := range records {
		e := s.c.hub.get(id)
		le := listEntry{ID: id, Name: rec.Name, LastSeen: rec.CreatedUnix}
		if e != nil {
			le.Online = e.online()
			if last := e.lastSeen.Load(); last > 0 {
				le.LastSeen = last
			}
		}
		resp.Items = append(resp.Items, le)
	}
	// 运行时出现过但不在台账的（匿名/临时），也一并列出
	s.c.hub.mu.Lock()
	for id, e := range s.c.hub.peers {
		if _, managed := records[id]; managed {
			continue
		}
		le := listEntry{ID: id, Name: "(临时)", Online: e.online()}
		if last := e.lastSeen.Load(); last > 0 {
			le.LastSeen = last
		}
		resp.Items = append(resp.Items, le)
	}
	s.c.hub.mu.Unlock()
	return tlv.Json(resp), nil
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

// ── 信令面：ws 连接生命周期钩子 ──────────────────────────────────────

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

// offline 置离线（仅清通道；台账与运行时条目保留，供 List 观察状态）。
func (c *Coord) offline(ctx context.Context, ch bucket.IChannel, why string) {
	if id, ok := c.hub.dropChannel(ch); ok {
		sloth.Infow(ctx, "peer offline", "peer", id, "why", why)
	}
}

func (h *coordHandler) OnClose(ctx context.Context, req *http.Request, s types.IBucket, ch bucket.IChannel) error {
	h.c.offline(ctx, ch, "ws closed")
	return nil
}

func (h *coordHandler) OnError(ctx context.Context, req *http.Request, s types.IBucket, ch bucket.IChannel, err error) error {
	if ch != nil {
		h.c.offline(ctx, ch, "error: "+err.Error())
	}
	return nil
}

// ── 数据面观测：UDP ──────────────────────────────────────────────────

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
			e.lastSeen.Store(time.Now().Unix())
			_, _ = conn.WriteToUDP(wire.Ack(from.String()), from)
		}
	}
}

// regJanitor REG 心跳超时兜底：peer 崩溃没走 ws OnClose 时也能转离线。
func (c *Coord) regJanitor(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		c.hub.mu.Lock()
		var stale []*peerEntry
		for id, e := range c.hub.peers {
			if e.channel() == nil {
				continue
			}
			if last := e.lastSeen.Load(); last > 0 && time.Since(time.Unix(last, 0)) > regStaleAfter {
				stale = append(stale, e)
				sloth.Warnw(ctx, "peer offline (reg heartbeat timeout)", "peer", id)
			}
		}
		c.hub.mu.Unlock()
		for _, e := range stale {
			e.setChannel(nil)
		}
	}
}

func (c *Coord) coordUDPStr() string {
	if c.coordUDPAddr != nil {
		return c.coordUDPAddr.String()
	}
	return ""
}

func main() {
	rpcAddr := flag.String("rpc", ":9100", "ws 信令监听地址")
	udpAddr := flag.String("udp", ":9101", "UDP 地址观测监听地址")
	storePath := flag.String("store", "ids.json", "ID 台账文件路径（空串不持久化）")
	create := flag.String("create", "", "生成一个新 ID（name 为备注），打印 id+token 后退出")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sloth.SetLogLevel("info")

	store, err := newIDStore(*storePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load id store:", err)
		os.Exit(1)
	}

	// -create：本地生成 ID 并退出（不需要起服务）
	if *create != "" {
		id, token, err := store.create(*create)
		if err != nil {
			fmt.Fprintln(os.Stderr, "create:", err)
			os.Exit(1)
		}
		fmt.Printf("id:    %s\ntoken: %s\n（token 只显示这一次，请妥善保管）\n", id, token)
		return
	}

	coord := NewCoord(store)

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
	coord.coordUDPAddr = uaddr
	go coord.udpLoop(uconn)
	go coord.regJanitor(ctx)

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
	sloth.Infow(ctx, "coordinator started", "rpc", *rpcAddr, "udp", *udpAddr,
		"store", *storePath)

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
