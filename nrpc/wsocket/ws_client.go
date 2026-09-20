package wsocket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime/debug"

	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/internal/logger"
	"github.com/w6xian/sloth/v3/internal/metrics"
	"github.com/w6xian/sloth/v3/internal/utils"
	"github.com/w6xian/sloth/v3/internal/utils/id"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/nrpc"
	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/types/auth"
	"github.com/w6xian/sloth/v3/types/handler"
	"github.com/w6xian/sloth/v3/types/trpc"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type LocalClient struct {
	nrpc.RpcConn
	serviceMapMu sync.RWMutex
	// clientMu 保护 client 字段：ClientWs 在连接建立 goroutine 中写入，
	// 调用方可能在另一 goroutine 轮询/使用该字段
	clientMu sync.RWMutex
	uriPath  string
	address  string
	handler  handler.IClientHandleMessage
	client   trpc.ICall

	defaultHeader message.Header

	// appID 在 LocalClient 生命周期内保持稳定（原先每次拨号重新生成）：
	// 便于服务端在多次重连之间关联同一会话，且省掉每次拨号的 uuid 生成。
	appID string
	// dialer 独立拨号器：把 ReadBufferSize/WriteBufferSize 等配置真正应用到连接上
	// （原先沿用 websocket.DefaultDialer，连接层缓冲区配置在客户端侧完全失效）。
	dialer *websocket.Dialer

	// authMu/lastAuth 保存最近一次成功设置的认证信息。
	// 弱网重连后新连接是一张"白纸"（UserId=0/Sign 为空），若不恢复，
	// 服务端 bucket 里挂着的仍是已死的旧连接，新连接永远收不到服务端调用。
	authMu   sync.RWMutex
	lastAuth *auth.AuthInfo

	// relogin 每次(重)连建立后回调：业务可在此重新 Sign/Reg，
	// 让服务端把新连接重新注册进 bucket。这是"断线重连后服务端调不通"的根治手段。
	reloginMu sync.RWMutex
	relogin   func(ctx context.Context) error

	// queueSize 连接队列容量（见 option.WithChannelQueueSize）
	queueSize int
}

// 实现 options.ConnectOption
func (c *LocalClient) SetRouter(router *mux.Router) error {
	return nil
}
func (c *LocalClient) SetUriPath(path string) error {
	c.uriPath = path
	return nil
}
func (c *LocalClient) SetAddress(address string) error {
	c.address = address
	return nil
}
func (c *LocalClient) SetHeader(key string, value string) error {
	c.Header[key] = value
	return nil
}
func (c *LocalClient) SetOrigin(origins ...string) error {
	return nil
}
// SetChannelQueueSize 设置客户端连接的队列容量（见 option.WithChannelQueueSize）。
func (c *LocalClient) SetChannelQueueSize(n int) {
	if n > 0 {
		c.queueSize = n
	}
}

// SetServerHandleMessage 在客户端无意义（客户端没有"服务端消息处理器"这一角色）。
// 原实现直接 panic：库内 panic 会把调用方进程打挂，且无法被业务 recover 判断，
// 这里改为返回 error，由调用方决定如何处理。
func (s *LocalClient) SetServerHandleMessage(handler handler.IServerHandleMessage) error {
	return errors.New("SetServerHandleMessage is not implemented on client")
}
func (s *LocalClient) SetClientHandleMessage(handler handler.IClientHandleMessage) error {
	s.handler = handler
	return nil
}

func NewLocalClient(connect trpc.ICallRpc, options ...option.ConnectOption) *LocalClient {
	s := new(LocalClient)
	s.Connect = connect
	s.uriPath = "/ws"
	s.address = "127.0.0.1:8080"
	s.defaultHeader = message.Header{}
	s.appID = id.ShortStringID()

	opt := s.Connect.Options()
	s.WriteWait = opt.WriteWait
	s.ReadWait = opt.ReadWait
	s.PongWait = opt.PongWait
	s.PingPeriod = opt.PingPeriod
	s.MaxMessageSize = opt.MaxMessageSize
	s.ReadBufferSize = opt.ReadBufferSize
	s.WriteBufferSize = opt.WriteBufferSize
	s.BroadcastSize = opt.BroadcastSize
	s.SliceSize = opt.SliceSize
	s.KeepAlive = opt.KeepAlive
	s.Header = make(map[string]string)
	s.handler = nil

	for _, opt := range options {
		opt(s)
	}
	// 配置在 options 应用之后才生效，因此拨号器必须在此构建
	c := s
	c.dialer = &websocket.Dialer{
		Proxy:             http.ProxyFromEnvironment,
		HandshakeTimeout:  c.dialHandshakeTimeout(),
		ReadBufferSize:    c.ReadBufferSize,
		WriteBufferSize:   c.WriteBufferSize,
		EnableCompression: false,
	}
	return s
}

// dialHandshakeTimeout 握手超时：PongWait 内有配置则用其一半，否则 15s。
// 避免弱网下拨号长时间悬挂在握手阶段而无人感知。
func (c *LocalClient) dialHandshakeTimeout() time.Duration {
	if c.PongWait > 2*time.Second {
		return c.PongWait / 2
	}
	return 15 * time.Second
}

// SetAutoRelogin 设置"每次连接建立后自动重新登录/注册"的钩子。
//
// 背景：断线重连后服务端拿到的是一条全新连接，若业务只在启动时登录一次，
// 该连接永远不会被注册进 bucket —— 表现为"客户端在线但服务端调不通"，
// 只有重启任一端才能恢复。在此回调里重新发起 Sign/Reg 即可根治
// （服务端 Bucket.Put 对同一 userId 幂等顶号，重复登录安全）。
func (c *LocalClient) SetAutoRelogin(fn func(ctx context.Context) error) {
	c.reloginMu.Lock()
	c.relogin = fn
	c.reloginMu.Unlock()
}

func (c *LocalClient) reloginFn() func(ctx context.Context) error {
	c.reloginMu.RLock()
	defer c.reloginMu.RUnlock()
	return c.relogin
}

func (c *LocalClient) savedAuth() *auth.AuthInfo {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	return c.lastAuth
}

// log 输出一行带级别的日志。
//
// 原实现先丢弃 level（_ = level）再用 log.Println 打印：
// 级别被完全忽略，且 format 与 args 被并列打印（"%s" 原样出现在日志里）。
func (c *LocalClient) log(level logger.LogLevel, line string, args ...any) {
	logger.Logf(level, nil, line, args...)
}

func signalClose(closeChan chan struct{}) {
	if closeChan == nil {
		return
	}
	select {
	case closeChan <- struct{}{}:
	default:
	}
}

// 重连退避区间：断线后按 500ms → 30s 指数退避（带抖动），
// 既能在网络恢复后快速重连，又能避免服务端重启时所有客户端同一时刻集体冲击（惊群）。
const (
	reconnectMinWait = 500 * time.Millisecond
	reconnectMaxWait = 30 * time.Second
)

// dialErrLog 拨号失败日志采样计数：弱网期间失败会连续发生，
// 全量打印会形成日志风暴，故仅首次与每满 20 次记录一条。
// 精确累计值见指标 sloth_client_dial_errors_total。
var (
	dialErrLog atomic.Uint64

	clientDialErrors = metrics.NewCounter("sloth_client_dial_errors_total", "客户端拨号失败次数")
	clientReconnects = metrics.NewCounter("sloth_client_reconnects_total", "客户端重连尝试次数")
)

// backoff 计算第 attempt 次（从 0 开始）失败后的等待时长，带 ±25% 抖动。
func backoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 32 {
		attempt = 32
	}
	d := reconnectMinWait << uint(attempt)
	if d <= 0 || d > reconnectMaxWait {
		d = reconnectMaxWait
	}
	// 抖动：d/4 范围内随机，避免多客户端同步重连
	jitter := time.Duration(utils.RandInt64(0, int64(d/4)))
	return d - d/8 + jitter
}

// ListenAndServe 建立并维持到服务端的连接，直到 ctx 结束或 KeepAlive 关闭。
//
// 采用循环而非递归重连：原实现在一条连接结束后递归调用自身，
// 每次断线都多保留一层栈帧（持有 ctx/resp 等引用且永不释放），
// 长时间运行的客户端栈会单调增长。
func (c *LocalClient) ListenAndServe(ctx context.Context) error {
	defer func() {
		if err := recover(); err != nil {
			c.log(logger.Error, "ListenAndServe recover err : %v", err)
		}
	}()
	// 构建ws url
	addr := utils.GetWsUrl(c.address, c.uriPath)
	if _, err := url.ParseRequestURI(addr); err != nil {
		return fmt.Errorf("invalid websocket address %q: %w", addr, err)
	}
	c.log(logger.Info, "new client connect %s", addr)

	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		conn, resp, err := c.dialOnce(addr)
		if err != nil {
			if !c.KeepAlive {
				return err
			}
			clientDialErrors.Inc()
			clientReconnects.Inc()
			if n := dialErrLog.Add(1); n == 1 || n%20 == 0 {
				c.log(logger.Error, "connect server %s err: %v (failures:%d)", addr, err, n)
			}
			if !c.sleep(ctx, backoff(attempt)) {
				return ctx.Err()
			}
			continue
		}
		// 连接成功：重置退避计数
		attempt = -1
		// 调用OnConnect
		if c.handler != nil {
			if err := c.handler.OnConnect(ctx, resp); err != nil {
				logger.Errorw(ctx, "ws client OnConnect rejected", "addr", addr, "err", err)
				_ = conn.Close()
				return err
			}
		}
		// 阻塞服务这条连接，直到它结束（断开/关闭）
		c.ClientWs(ctx, conn, resp)
		if !c.KeepAlive {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// 连接是被对端/网络断掉的，立刻重连，不再退避等待
	}
}

// dialOnce 发起一次拨号。
func (c *LocalClient) dialOnce(addr string) (*websocket.Conn, *http.Response, error) {
	header := make(http.Header)
	header["app_id"] = []string{c.appID}
	for k, v := range c.Header {
		header[k] = []string{v}
	}
	dialer := c.dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	return dialer.Dial(addr, header)
}

// sleep 等待 d；ctx 结束返回 false。
func (c *LocalClient) sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Client 返回底层客户端通道（连接建立后非 nil，用于调用方轮询连接就绪状态）
// Ready 是否已建立 WebSocket 连接（传输无关的就绪判定，供上层/测试轮询）。
func (c *LocalClient) Ready() bool { return c.Client() != nil }

func (c *LocalClient) Client() trpc.ICall {
	c.clientMu.RLock()
	defer c.clientMu.RUnlock()
	return c.client
}

func (c *LocalClient) SetAuthInfo(auth *auth.AuthInfo) error {
	if auth == nil {
		return errors.New("auth is nil")
	}
	cli := c.Client()
	if cli == nil {
		return errors.New("client not found")
	}
	if err := cli.SetAuthInfo(auth); err != nil {
		return err
	}
	// 记住认证信息：重连后自动恢复到新连接
	c.authMu.Lock()
	c.lastAuth = auth
	c.authMu.Unlock()
	return nil
}

// GetAuthInfo 获取认证信息
func (c *LocalClient) GetAuthInfo() (*auth.AuthInfo, error) {
	cli := c.Client()
	if cli == nil {
		return nil, errors.New("client not found")
	}
	return cli.GetAuthInfo()
}

// ClientWs 服务一条已建立的连接，直到该连接结束才返回。
// 重连由 ListenAndServe 的循环负责，这里不再递归，避免栈无限增长。
func (c *LocalClient) ClientWs(ctx context.Context, conn *websocket.Conn, resp *http.Response) {
	defer func() {
		if err := recover(); err != nil {
			c.log(logger.Error, "ClientWs recover err : %v", err)
		}

	}()
	// 链接session
	closeChan := make(chan struct{}, 1)
	// 全局client websocket连接
	wsConn := NewWsChannelClient(c.Connect, WithClientQueueSize(c.queueSize))
	//default broadcast size eq 512
	wsConn.Conn = conn
	wsConn.RoomId = 0
	c.clientMu.Lock()
	c.client = wsConn
	c.clientMu.Unlock()
	// 会话恢复：新连接默认没有任何身份信息，把上次成功设置的认证信息回填，
	// 保证 GetAuthInfo/本地状态在重连后依旧可用（服务端 bucket 的重新注册由
	// relogin 钩子或业务 OnReady 里的登录 RPC 完成）。
	if saved := c.savedAuth(); saved != nil {
		_ = wsConn.SetAuthInfo(saved)
	}
	ctx, cancel := context.WithCancel(ctx)
	//get data from websocket conn
	go c.readPump(ctx, wsConn, closeChan, resp)
	//send data to websocket conn
	go c.writePump(ctx, wsConn, closeChan)
	// 自动重新登录/注册：必须在读写协程启动之后执行（登录本身要发包）。
	c.runRelogin(ctx)
	// 等待关闭信号
	<-closeChan
	cancel()
}

// runRelogin 执行自动重新登录钩子。
func (c *LocalClient) runRelogin(ctx context.Context) {
	fn := c.reloginFn()
	if fn == nil {
		return
	}
	if err := fn(ctx); err != nil {
		c.log(logger.Error, "auto relogin err: %v", err)
	}
}

func (c *LocalClient) DefaultHeader() message.Header {
	return c.defaultHeader
}

func (c *LocalClient) Call(ctx context.Context, header message.Header, mtd string, data ...[]byte) ([]byte, error) {
	defer func() {
		if err := recover(); err != nil {
			c.log(logger.Error, "Call recover err : %v", err)
		}
	}()
	cli := c.Client()
	if cli == nil {
		c.log(logger.Error, "client not found")
		return nil, errors.New("client not found")
	}

	usePoolHeader := false
	mergedHeader := header
	if len(c.defaultHeader) != 0 {
		usePoolHeader = true
		mergedHeader = message.GetHeader()
		for k, v := range c.defaultHeader {
			mergedHeader[k] = v
		}
		for k, v := range header {
			mergedHeader[k] = v
		}
	}
	if usePoolHeader {
		defer message.PutHeader(mergedHeader)
	}

	// 使用中间件链包装调用
	handler := func(ctx context.Context, hdr message.Header, method string, args ...[]byte) ([]byte, error) {
		return cli.Call(ctx, hdr, method, args...)
	}

	rst, err := handler(ctx, mergedHeader, mtd, data...)
	if err != nil {
		return nil, err
	}
	return rst, nil
}

func (c *LocalClient) Push(ctx context.Context, msg *message.Msg) (err error) {
	cli := c.Client()
	if cli == nil {
		c.log(logger.Error, "server not found")
		return errors.New("server not found")
	}
	return cli.Push(ctx, msg)
}

func (c *LocalClient) writePump(ctx context.Context, ch *WsChannelClient, closeChan chan struct{}) {
	defer func() {
		if err := recover(); err != nil {
			c.log(logger.Error, "writePump recover 11 err : %v", err)
		}
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	//PingPeriod default eq 54s
	ticker := time.NewTicker(c.PingPeriod)
	defer func() {
		// 检测是否有效或已关闭
		signalClose(closeChan)
	}()
	defer func() {
		ticker.Stop()
		// closeConn 幂等：readPump 与 writePump 的 defer 会并发执行，必须只关闭一次
		ch.closeConn()
	}()
	sliceSize := int(c.SliceSize) // 默认512
	for {
		select {
		case msg, ok := <-ch.PSend:
			conn := ch.getConn()
			if conn == nil {
				return
			}
			//write data dead time , like http timeout , default 10s
			conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
			if !ok {
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := slicesTextSend(ch.nextSliceName(), conn, msg.Body, sliceSize); err != nil {
				return
			}
		case payload, ok := <-ch.PRpcCaller:
			/*
			 * @call  调用服务器方法
			 * @param payload 调用参数
			 */
			conn := ch.getConn()
			if conn == nil {
				return
			}
			// @call  调用服务器方法
			//write data dead time , like http timeout , default 10s
			conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
			if !ok {
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := slicesTextSend(ch.nextSliceName(), conn, payload, sliceSize); err != nil {
				c.log(logger.Error, "slicesBinarySend err = %v", err.Error())
				return
			}
		case payload, ok := <-ch.PRpcBacker:
			/*
			 * @reply  服务器返回调用结果
			 * @param payload 调用结果
			 */
			conn := ch.getConn()
			if conn == nil {
				return
			}
			// @reply  服务器返回调用结果
			//write data dead time , like http timeout , default 10s
			conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
			if !ok {
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := slicesTextSend(ch.nextSliceName(), conn, payload, sliceSize); err != nil {
				return
			}

		case <-ticker.C:
			conn := ch.getConn()
			if conn == nil {
				return
			}
			//heartbeat，if ping error will exit and close current websocket conn
			conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-ctx.Done():
			c.log(logger.Error, "[ws_client]writePump ctx.Done()")
			return
		}
	}
}

func (c *LocalClient) readPump(ctx context.Context, ch *WsChannelClient, closeChan chan struct{}, resp *http.Response) {
	// 读循环解析的是服务端发来的字节流，任何字节都可能是畸形的。
	// 原实现的 recover 被整段注释掉了：解码路径一旦 panic（如分片越界、
	// tlv 畸形帧），客户端进程会直接退出——而服务端同名函数是有 recover 的，
	// 两端不对称。这里恢复兜底：panic 只影响本条连接，转成日志后退出读循环。
	defer func() {
		if r := recover(); r != nil {
			logger.Errorw(ctx, "ws client readPump panic", "panic", r,
				"stack", string(debug.Stack()))
		}
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		signalClose(closeChan)
		ch.closeConn()
	}()

	conn := ch.getConn()
	if conn == nil {
		return
	}
	conn.SetReadLimit(c.MaxMessageSize)
	conn.SetReadDeadline(time.Now().Add(c.PongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(c.PongWait))
		return nil
	})
	// 要防止OnOpen阻塞，导致readPump阻塞
	if c.handler != nil {
		go c.handler.OnReady(ctx, resp, c, ch)
	}
	for {
		// 主动关闭
		select {
		case <-ctx.Done():
			c.log(logger.Error, "[ws_client]readPump ctx.Done()")
			return
		default:
		}
		// 来自服务器的消息
		messageType, msg, err := conn.ReadMessage()
		if err != nil {
			// 原实现 c.log(logger.Error, err.Error())：把动态错误串当 format，
			// 错误文本里的 % 会被当成占位符，输出成 %!x(MISSING)。
			logger.Errorw(ctx, "ws client readPump read failed", "err", err)
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				if c.handler != nil {
					c.handler.OnError(ctx, resp, c, ch, err)
				}
			} else {
				if c.handler != nil {
					c.handler.OnClose(ctx, resp, c, ch)
				}
			}
			c.log(logger.Error, "readPump，ch.Conn.ReadMessage return")
			return
		}
		if len(msg) == 0 || messageType == -1 {
			c.log(logger.Info, "readPump，message is nil or messageType is -1")
			continue
		}
		// 消息体可能太大，需要分片接收后再解析
		// 实现分片接收的函数
		m, err := receiveMessage(conn, byte(messageType), msg)
		if err != nil {
			if c.handler != nil {
				c.handler.OnError(ctx, resp, c, ch, err)
			}
			continue
		}
		// tlv 是外部库，畸形帧会 panic：走 tlvValue 兜底，失败即按裸数据处理
		if v, err := tlvValue(m); err == nil {
			m = v
		}
		if err := nrpc.DispatchMessage(nrpc.RouteArgs{
			Context: ctx,
			Request: resp.Request,
			Data:    m,
			// 用户通过 option.WithCodec 注入的 codec（nil = 自动识别）
			Codec: c.Codec,
			OnFn: func(ctx context.Context, raw []byte) error {
				return nrpc.HandleFn(ctx, nil, resp, nil, c.Connect, ch, raw)
			},
			OnData: func(ctx context.Context, raw []byte) error {
				if c.handler == nil {
					return nil
				}
				return c.handler.OnData(ctx, resp, c, ch, messageType, raw)
			},
		}); err != nil && c.handler != nil {
			c.handler.OnError(ctx, resp, c, ch, err)
		}
	}
}

// 实现IBucket接口 (为了统一，无其他)
func (c *LocalClient) Bucket(userId int64) *bucket.Bucket {
	return nil
}

func (c *LocalClient) Channel(userId int64) bucket.IChannel {
	return nil
}

func (c *LocalClient) Room(roomId int64) *bucket.Room {
	return nil
}

func (c *LocalClient) Broadcast(ctx context.Context, msg *message.Msg) (err error) {
	return nil
}
