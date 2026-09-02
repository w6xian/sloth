package wsocket

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"

	"sync"
	"time"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/internal/logger"
	"github.com/w6xian/sloth/v3/internal/utils"
	"github.com/w6xian/sloth/v3/internal/utils/id"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/nrpc"
	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/types/auth"
	"github.com/w6xian/sloth/v3/types/handler"
	"github.com/w6xian/sloth/v3/types/trpc"
	"github.com/w6xian/tlv"

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
func (s *LocalClient) SetServerHandleMessage(handler handler.IServerHandleMessage) error {
	// 空方法
	panic("SetClientHandleMessage is not implemented")
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

	s.serviceMapMu = sync.RWMutex{}

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
	return s
}

func (c *LocalClient) log(level logger.LogLevel, line string, args ...any) {
	_ = level
	log.Println("[LocalClient]", line, args)
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

func (c *LocalClient) ListenAndServe(ctx context.Context) error {
	defer func() {
		if err := recover(); err != nil {
			c.log(logger.Error, "ListenAndServe recover err : %v", err)
		}
	}()
	// 构建ws url
	addr := utils.GetWsUrl(c.address, c.uriPath)
	c.log(logger.Info, "new client connect %s", addr)
	_, err := url.ParseRequestURI(addr)
	if err == nil {
		// 构建header
		header := make(http.Header)
		header["app_id"] = []string{id.ShortStringID()}
		for k, v := range c.Header {
			header[k] = []string{v}
		}

		conn, resp, err := websocket.DefaultDialer.Dial(addr, header)
		if err != nil && c.KeepAlive {
			// 1-30 秒重试
			retry := utils.RandInt64(1, 30)
			log.Printf("connect server %s err : %v, retry after %d seconds", addr, err, retry)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(retry) * time.Second):
			}
			if ctx.Err() == nil {
				c.ListenAndServe(ctx)
			}
			return err
		}
		// 调用OnConnect
		if c.handler != nil {
			if err := c.handler.OnConnect(ctx, resp); err != nil {
				log.Printf("OnConnect err %v", err)
				return err
			}
		}
		c.ClientWs(ctx, conn, resp)
	}
	return nil
}

// Client 返回底层客户端通道（连接建立后非 nil，用于调用方轮询连接就绪状态）
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
	return cli.SetAuthInfo(auth)
}

// GetAuthInfo 获取认证信息
func (c *LocalClient) GetAuthInfo() (*auth.AuthInfo, error) {
	cli := c.Client()
	if cli == nil {
		return nil, errors.New("client not found")
	}
	return cli.GetAuthInfo()
}

// ClientWs 客户端连接
func (c *LocalClient) ClientWs(ctx context.Context, conn *websocket.Conn, resp *http.Response) {
	defer func() {
		if err := recover(); err != nil {
			c.log(logger.Error, "ClientWs recover err : %v", err)
		}

	}()
	// 链接session
	closeChan := make(chan struct{}, 1)
	// 全局client websocket连接
	wsConn := NewWsChannelClient(c.Connect)
	c.clientMu.Lock()
	c.client = wsConn
	c.clientMu.Unlock()
	//default broadcast size eq 512
	wsConn.Conn = conn
	wsConn.RoomId = 0
	parentCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	//get data from websocket conn
	go c.readPump(ctx, wsConn, closeChan, resp)
	//send data to websocket conn
	go c.writePump(ctx, wsConn, closeChan)
	// 等待关闭信号
	<-closeChan
	cancel()
	// 重连
	if c.KeepAlive && parentCtx.Err() == nil {
		c.ListenAndServe(parentCtx)
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
			if ch.Conn == nil {
				return
			}
			//write data dead time , like http timeout , default 10s
			ch.Conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
			if !ok {
				ch.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := slicesTextSend(getSliceName(), ch.Conn, msg.Body, sliceSize); err != nil {
				return
			}
		case payload, ok := <-ch.PRpcCaller:
			/*
			 * @call  调用服务器方法
			 * @param payload 调用参数
			 */
			if ch.Conn == nil {
				return
			}
			// @call  调用服务器方法
			//write data dead time , like http timeout , default 10s
			ch.Conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
			if !ok {
				ch.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := slicesTextSend(getSliceName(), ch.Conn, payload, sliceSize); err != nil {
				c.log(logger.Error, "slicesBinarySend err = %v", err.Error())
				return
			}
		case payload, ok := <-ch.PRpcBacker:
			/*
			 * @reply  服务器返回调用结果
			 * @param payload 调用结果
			 */
			if ch.Conn == nil {
				return
			}
			// @reply  服务器返回调用结果
			//write data dead time , like http timeout , default 10s
			ch.Conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
			if !ok {
				ch.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := slicesTextSend(getSliceName(), ch.Conn, payload, sliceSize); err != nil {
				return
			}

		case <-ticker.C:
			if ch.Conn == nil {
				return
			}
			//heartbeat，if ping error will exit and close current websocket conn
			ch.Conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
			if err := ch.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-ctx.Done():
			c.log(logger.Error, "[ws_client]writePump ctx.Done()")
			return
		}
	}
}

func (c *LocalClient) readPump(ctx context.Context, ch *WsChannelClient, closeChan chan struct{}, resp *http.Response) {
	// defer func() {
	// 	if err := recover(); err != nil {
	// 		c.log(logger.Error, "readPump recover err : %v", err)
	// 	}
	// }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		signalClose(closeChan)
		ch.closeConn()
	}()

	ch.Conn.SetReadLimit(c.MaxMessageSize)
	ch.Conn.SetReadDeadline(time.Now().Add(c.PongWait))
	ch.Conn.SetPongHandler(func(string) error {
		ch.Conn.SetReadDeadline(time.Now().Add(c.PongWait))
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
		messageType, msg, err := ch.Conn.ReadMessage()
		if err != nil {
			c.log(logger.Error, err.Error())
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
		m, err := receiveMessage(ch.Conn, byte(messageType), msg)
		if err != nil {
			if c.handler != nil {
				c.handler.OnError(ctx, resp, c, ch, err)
			}
			continue
		}
		tlvFrame, err := tlv.Deserialize(m)
		if err == nil {
			m = tlvFrame.Value()
		}
		if err := nrpc.DispatchMessage(nrpc.RouteArgs{
			Context: ctx,
			Request: resp.Request,
			Data:    m,
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
