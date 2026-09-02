package wsocket

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/internal/logger"
	"github.com/w6xian/sloth/v3/internal/tools"
	"github.com/w6xian/sloth/v3/internal/utils"
	"github.com/w6xian/sloth/v3/internal/utils/array"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/nrpc"
	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/types/handler"
	"github.com/w6xian/sloth/v3/types/trpc"
	"github.com/w6xian/tlv"

	"log"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type WsServer struct {
	nrpc.RpcConn

	Buckets      []*bucket.Bucket
	bucketIdx    uint32
	serviceMapMu sync.RWMutex

	uriPath string
	handler handler.IServerHandleMessage
	router  *mux.Router

	originDomain []string

	// 连接限额（来自 Options.MaxConnsGlobal/MaxConnsPerIP/MaxConnsWS）
	maxGlobal  int64
	maxWS      int64
	maxPerIP   int64
	trustProxy bool

	// connsMu 同时保护 conns（活跃连接）与 perIP（每 IP 连接计数）
	connsMu   sync.Mutex
	conns     map[*WsChannelServer]struct{}
	perIP     map[string]int64
	globalCnt atomic.Int64
	closed    bool
}

// 实现 options.ConnectOption
func (s *WsServer) SetRouter(router *mux.Router) error {
	s.router = router
	return nil
}

func (s *WsServer) SetUriPath(path string) error {
	s.uriPath = path
	return nil
}
func (s *WsServer) SetAddress(address string) error {
	return nil
}
func (s *WsServer) SetHeader(key string, value string) error {
	s.Header[key] = value
	return nil
}
func (s *WsServer) SetOrigin(args ...string) error {
	s.originDomain = append(s.originDomain, args...)
	return nil
}

func (s *WsServer) SetServerHandleMessage(handler handler.IServerHandleMessage) error {
	s.handler = handler
	return nil
}
func (s *WsServer) SetClientHandleMessage(handler handler.IClientHandleMessage) error {
	return nil
}

func (s *WsServer) log(level logger.LogLevel, line string, args ...any) {
	log.Println("[WsServer]", level, line, args)
}

func NewWsServer(server trpc.ICallRpc, opts ...option.ConnectOption) *WsServer {
	bsNum := 1
	bsNum = max(bsNum, runtime.NumCPU())
	//init Connect layer rpc server, logic client will call this
	bs := make([]*bucket.Bucket, bsNum)
	opt := server.Options()
	for i := 0; i < bsNum; i++ {
		bs[i] = bucket.NewBucket(
			bucket.WithChannelSize(opt.ChannelSize),
			bucket.WithRoomSize(opt.RoomSize),
			bucket.WithRoutineAmount(opt.RoutineAmount),
			bucket.WithRoutineSize(opt.RoutineSize),
		)
	}
	s := new(WsServer)
	s.Buckets = bs
	s.bucketIdx = uint32(len(bs))
	s.Connect = server
	s.uriPath = "/ws"
	s.handler = nil
	s.router = mux.NewRouter()
	s.WriteWait = opt.WriteWait
	s.ReadWait = opt.ReadWait
	s.PongWait = opt.PongWait
	s.PingPeriod = opt.PingPeriod
	s.MaxMessageSize = opt.MaxMessageSize
	s.ReadBufferSize = opt.ReadBufferSize
	s.WriteBufferSize = opt.WriteBufferSize
	s.BroadcastSize = opt.BroadcastSize
	s.SliceSize = opt.SliceSize
	s.Header = make(map[string]string)
	s.maxGlobal = opt.MaxConnsGlobal
	s.maxWS = opt.MaxConnsWS
	s.maxPerIP = opt.MaxConnsPerIP
	s.trustProxy = opt.TrustProxyHeaders
	s.conns = make(map[*WsChannelServer]struct{})
	s.perIP = make(map[string]int64)

	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Handler 返回 HTTP handler（mux router），供 Serve() 挂载到 http.Server。
// 此前 Serve() 直接 http.Serve(listener, nil) 使用 DefaultServeMux，
// 导致 /ws 路由与 OnConnect 校验从未生效，WebSocket 实际无法建连。
func (s *WsServer) Handler() http.Handler {
	return s.router
}

// clientIP 解析客户端真实 IP：开启 TrustProxyHeaders 时优先取 X-Forwarded-For / X-Real-IP。
// 注意：仅在可信反向代理之后部署时开启，否则该头可被伪造绕过 per-IP 限额。
func (s *WsServer) clientIP(r *http.Request) string {
	if s.trustProxy {
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			if ip := strings.TrimSpace(strings.Split(xff, ",")[0]); ip != "" {
				return ip
			}
		}
		if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
			return xri
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// acquireConn 在 WebSocket Upgrade 成功后占用一个连接名额（全局 + per-IP）。
// 超限时回滚计数并返回 ok=false，调用方应关闭该连接。
func (s *WsServer) acquireConn(r *http.Request) (ip string, ok bool) {
	ip = s.clientIP(r)
	cur := s.globalCnt.Add(1)
	if s.maxGlobal > 0 && cur > s.maxGlobal {
		s.globalCnt.Add(-1)
		return ip, false
	}
	if s.maxWS > 0 && cur > s.maxWS {
		s.globalCnt.Add(-1)
		return ip, false
	}
	if s.maxPerIP > 0 {
		s.connsMu.Lock()
		n := s.perIP[ip] + 1
		if n > s.maxPerIP {
			s.connsMu.Unlock()
			s.globalCnt.Add(-1)
			return ip, false
		}
		s.perIP[ip] = n
		s.connsMu.Unlock()
	}
	return ip, true
}

// releaseConn 在连接断开时释放连接名额。
func (s *WsServer) releaseConn(ip string) {
	s.globalCnt.Add(-1)
	if s.maxPerIP > 0 && ip != "" {
		s.connsMu.Lock()
		if n := s.perIP[ip]; n > 1 {
			s.perIP[ip] = n - 1
		} else {
			delete(s.perIP, ip)
		}
		s.connsMu.Unlock()
	}
}

// addConn 登记活跃连接；若服务器已进入关闭流程则立即关闭该连接。
func (s *WsServer) addConn(ch *WsChannelServer) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	if s.closed {
		ch.Close()
		return
	}
	s.conns[ch] = struct{}{}
}

func (s *WsServer) removeConn(ch *WsChannelServer) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	delete(s.conns, ch)
}

// Close 优雅关闭：停止接收新连接，关闭所有活跃连接，并停止 bucket worker 池。
func (s *WsServer) Close() error {
	s.connsMu.Lock()
	chs := make([]*WsChannelServer, 0, len(s.conns))
	for ch := range s.conns {
		chs = append(chs, ch)
	}
	s.conns = make(map[*WsChannelServer]struct{})
	s.closed = true
	s.connsMu.Unlock()

	for _, ch := range chs {
		ch.Close()
	}
	for _, b := range s.Buckets {
		if b != nil {
			b.Close()
		}
	}
	return nil
}

func (s *WsServer) Bucket(userId int64) *bucket.Bucket {
	if s.bucketIdx == 0 {
		return nil
	}
	userIdStr := fmt.Sprintf("%d", userId)
	idx := tools.CityHash32([]byte(userIdStr), uint32(len(userIdStr))) % s.bucketIdx
	return s.Buckets[idx]
}

func (s *WsServer) Channel(userId int64) bucket.IChannel {
	if b := s.Bucket(userId); b != nil {
		return b.Channel(userId)
	}
	return nil
}

func (s *WsServer) Room(roomId int64) *bucket.Room {
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
func (s *WsServer) AllBuckets() []*bucket.Bucket {
	return s.Buckets
}

// Broadcast 向所有 bucket 的所有房间异步广播。
// 仅把请求投递到各 bucket 的 worker 队列后立即返回，实际下发由 worker 池异步完成；
// 因此 ctx 不参与实际推送（worker 使用 bucket 自身的 background ctx）。
// 若某个房间因队列满投递失败（尽力而为），返回携带丢弃数量的 error，
// 调用方可感知队列压力并决定是否降级。
func (s *WsServer) Broadcast(ctx context.Context, msg *message.Msg) error {
	var dropped int
	for _, b := range s.Buckets {
		if b == nil {
			continue
		}
		dropped += b.BroadcastAll(ctx, msg)
	}
	if dropped > 0 {
		return fmt.Errorf("broadcast dropped %d rooms: worker queue full", dropped)
	}
	return nil
}

func (s *WsServer) ListenAndServe(ctx context.Context) error {
	defer func() {
		if err := recover(); err != nil {
			s.log(logger.Error, "ListenAndServe recover err : %v", err)
		}
	}()
	s.router.HandleFunc(s.uriPath, func(w http.ResponseWriter, r *http.Request) {
		if s.handler != nil {
			if err := s.handler.OnConnect(ctx, r); err != nil {
				log.Printf("OnConnect err %v", err)
				w.WriteHeader(http.StatusUnauthorized)
				// 关闭连接，返回401错误
				w.Write([]byte(err.Error()))
				return
			}
		}
		s.serveWs(ctx, w, r)
	})
	return nil
}
func (s *WsServer) serveWs(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var upGrader = websocket.Upgrader{
		ReadBufferSize:  s.ReadBufferSize,
		WriteBufferSize: s.WriteBufferSize,
	}
	// 构建header
	header := make(http.Header)
	for k, v := range s.Header {
		header[k] = []string{v}
	}

	upGrader.CheckOrigin = func(r *http.Request) bool {
		if r == nil {
			return false
		}
		// 未配置 originDomain 时默认放行：浏览器之外的客户端（原生、游戏客户端等）
		// 通常不带 Origin 头，若默认拒绝则所有未显式配置来源的连接都无法建立。
		if len(s.originDomain) == 0 {
			return true
		}
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		if err != nil || strings.TrimSpace(u.Host) == "" {
			return false
		}
		// originDomain 中* 表示允许所有域名
		if array.InArray("*", s.originDomain) {
			return true
		}
		return array.InArray(u.Host, s.originDomain)
	}
	conn, err := upGrader.Upgrade(w, r, header)
	if err != nil {
		return
	}
	// 连接数限额（全局/WS/per-IP），超限直接关闭
	ip, ok := s.acquireConn(r)
	if !ok {
		s.log(logger.Info, "ws connection limit exceeded, ip:%s", ip)
		conn.Close()
		return
	}
	// 一个连接一个channel
	ch := NewWsChannelServer(s.Connect)
	//default broadcast size eq 512
	ch.Conn = conn
	ch.PAddr = ip
	s.addConn(ch)
	// 需要确认客户端是否合法，一个是JWT,一个是ClientID
	go s.readPump(ctx, r, ch)
	//send data to websocket conn
	go s.writePump(ctx, r, ch)
	//get data from websocket conn

}

func (s *WsServer) writePump(ctx context.Context, r *http.Request, ch *WsChannelServer) {
	defer func() {
		if err := recover(); err != nil {
			s.log(logger.Error, "writePump 111 recover err : %v", err)
		}
	}()
	// 心跳间隔必须与 PongWait 配置保持一致（此前硬编码 9s 使 WithPingPeriod 失效）
	ticker := time.NewTicker(s.PingPeriod)
	defer func() {
		ticker.Stop()
		ch.Close()
	}()

	for {
		select {
		case <-ch.done:
			return
		case msg, ok := <-ch.broadcast:
			if ch.Conn == nil {
				return
			}
			//write data dead time , like http timeout , default 10s
			ch.Conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				ch.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := slicesTextSend(getSliceName(), ch.Conn, utils.Serialize(msg), 512); err != nil {
				return
			}
		case payload, ok := <-ch.PRpcCaller:
			if ch.Conn == nil {
				return
			}
			//write data dead time , like http timeout , default 10s
			ch.Conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				ch.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := slicesTextSend(getSliceName(), ch.Conn, payload, 512); err != nil {
				return
			}
		case payload, ok := <-ch.PRpcBacker:
			if ch.Conn == nil {
				return
			}
			//write data dead time , like http timeout , default 10s
			ch.Conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				ch.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := slicesTextSend(getSliceName(), ch.Conn, payload, 512); err != nil {
				return
			}
		case <-ticker.C:
			if ch.Conn == nil {
				return
			}
			//heartbeat，if ping error will exit and close current websocket conn
			ch.Conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if err := ch.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *WsServer) readPump(ctx context.Context, r *http.Request, ch *WsChannelServer) {
	defer func() {
		if err := recover(); err != nil {
			s.log(logger.Error, "readPump recover err : %v", err)
		}
	}()
	defer func() {
		// 无论登录与否，连接断开都必须从注册表移除。
		// 原条件（ch.Room()==nil || ch.UserId()==0）会放过已登录连接，导致：
		// Web 端 F5 强刷新后旧连接残留 b.chs/房间成员，新连接同 userId 重连又被
		// Bucket.Put 的幂等分支吞掉，后续 CallRoom/Broadcast 全部打在已断开的
		// 旧连接上 → 稳定超时，重启服务端才恢复。
		GetBucket(ctx, s.Buckets, ch.UserId()).DeleteChannel(ch)
		s.removeConn(ch)
		s.releaseConn(ch.PAddr)
		// 统一走 Close：关闭 done（通知 writePump 立即退出）+ 关闭底层连接。
		// ch.Close 带锁且 doneOnce 幂等，readPump/writePump 并发退出时无数据竞争。
		ch.Close()
	}()

	ch.Conn.SetReadLimit(s.MaxMessageSize)
	ch.Conn.SetReadDeadline(time.Now().Add(s.PongWait))
	ch.Conn.SetPongHandler(func(string) error {
		ch.Conn.SetReadDeadline(time.Now().Add(s.PongWait))
		return nil
	})

	// OnOpen  可以发送消息了
	if s.handler != nil {
		go s.handler.OnReady(ctx, r, s, ch)
	}

	for {
		messageType, msg, err := ch.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				if s.handler != nil {
					s.handler.OnError(ctx, r, s, ch, err)
				}
				return
			} else {
				if s.handler != nil {
					s.handler.OnClose(ctx, r, s, ch)
				}
			}
			s.log(logger.Error, "server readPump，ch.conn.ReadMessage return")
			return
		}
		if len(msg) == 0 || messageType == -1 {
			s.log(logger.Info, "server readPump，message is nil or messageType is -1")
			continue
		}
		//@call HandleCall 处理调用方法
		// 消息体可能太大，需要分片接收后再解析
		// 实现分片接收的函数

		m, err := receiveMessage(ch.Conn, byte(messageType), msg)
		if err != nil {
			if s.handler != nil {
				s.handler.OnError(ctx, r, s, ch, err)
			}
			continue
		}
		tlvFrame, err := tlv.Deserialize(m)
		if err == nil {
			m = tlvFrame.Value()
		}
		if err := nrpc.DispatchMessage(nrpc.RouteArgs{
			Context: ctx,
			Request: r,
			Data:    m,
			OnFn: func(ctx context.Context, raw []byte) error {
				return nrpc.HandleFn(ctx, r, nil, s, s.Connect, ch, raw)
			},
			OnData: func(ctx context.Context, raw []byte) error {
				if s.handler == nil {
					return nil
				}
				return s.handler.OnData(ctx, r, s, ch, messageType, raw)
			},
		}); err != nil && s.handler != nil {
			s.handler.OnError(ctx, r, s, ch, err)
		}
	}
}

//
