package wsocket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/w6xian/sloth/v2/actions"
	"github.com/w6xian/sloth/v2/bucket"
	"github.com/w6xian/sloth/v2/internal/logger"
	"github.com/w6xian/sloth/v2/internal/utils"
	"github.com/w6xian/sloth/v2/internal/utils/id"
	"github.com/w6xian/sloth/v2/message"
	"github.com/w6xian/sloth/v2/nrpc"
	"github.com/w6xian/sloth/v2/nrpc/middleware"
	"github.com/w6xian/sloth/v2/option"
	"github.com/w6xian/tlv"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type LocalClient struct {
	serviceMapMu sync.RWMutex
	uriPath      string
	address      string

	Connect nrpc.ICallRpc
	handler option.IClientHandleMessage
	client  nrpc.ICall

	WriteWait       time.Duration
	ReadWait        time.Duration
	PongWait        time.Duration
	PingPeriod      time.Duration
	SliceSize       int64
	MaxMessageSize  int64
	ReadBufferSize  int
	WriteBufferSize int
	BroadcastSize   int
	KeepAlive       bool

	middlewares []middleware.Middleware

	// RpcCaller 对象池，减少 GC 压力
	rpcCallerPool sync.Pool
}

// getRpcCaller 从池中获取 RpcCaller 对象
func (c *LocalClient) getRpcCaller() *nrpc.RpcCaller {
	req := c.rpcCallerPool.Get()
	if req == nil {
		return &nrpc.RpcCaller{}
	}
	return req.(*nrpc.RpcCaller)
}

// putRpcCaller 归还 RpcCaller 对象到池中
func (c *LocalClient) putRpcCaller(req *nrpc.RpcCaller) {
	// 重置字段
	req.Id = ""
	req.Protocol = 0
	req.Action = 0
	req.Header = nil
	req.Method = ""
	req.Data = nil
	req.Args = nil
	req.Channel = nil
	c.rpcCallerPool.Put(req)
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

func (s *LocalClient) SetServerHandleMessage(handler option.IServerHandleMessage) error {
	// 空方法
	panic("SetClientHandleMessage is not implemented")
}
func (s *LocalClient) SetClientHandleMessage(handler option.IClientHandleMessage) error {
	s.handler = handler
	return nil
}

func NewLocalClient(connect nrpc.ICallRpc, options ...option.ConnectOption) *LocalClient {
	s := new(LocalClient)
	s.Connect = connect
	s.uriPath = "/ws"
	s.address = "127.0.0.1:8080"

	s.serviceMapMu = sync.RWMutex{}

	// 初始化 RpcCaller 对象池
	s.rpcCallerPool = sync.Pool{
		New: func() any {
			return &nrpc.RpcCaller{}
		},
	}

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

	s.handler = nil
	s.middlewares = nil
	for _, opt := range options {
		opt(s)
	}
	return s
}

// Use 注册客户端中间件，可多次调用，按注册顺序执行。
func (c *LocalClient) Use(middlewares ...middleware.Middleware) {
	c.middlewares = append(c.middlewares, middlewares...)
}
func (c *LocalClient) log(level logger.LogLevel, line string, args ...any) {
	if c.Connect == nil {
		fmt.Println("LocalClient Connect is nil")
		return
	}
	c.Connect.Log(level, "[LocalClient]"+line, args...)
}

func (c *LocalClient) ListenAndServe(ctx context.Context) error {
	defer func() {
		if err := recover(); err != nil {
			c.log(logger.Error, "ListenAndServe recover err : %v", err)
		}
	}()
	addr := fmt.Sprintf("ws://%s%s", c.address, c.uriPath)
	c.log(logger.Info, "new client connect %s", addr)
	_, err := url.ParseRequestURI(addr)
	if err == nil {
		conn, _, err := websocket.DefaultDialer.Dial(addr, http.Header{
			"app_id": []string{id.ShortStringID()},
		})
		if err != nil && c.KeepAlive {
			// 1-30 秒重试
			retry := utils.RandInt64(1, 30)
			c.log(logger.Error, "connect server %s err : %v, retry after %d seconds", addr, err, retry)
			time.Sleep(time.Duration(retry) * time.Second)
			c.ListenAndServe(context.Background())
			return err
		}
		c.ClientWs(ctx, conn)
	}
	return nil
}

func (c *LocalClient) SetAuthInfo(auth *nrpc.AuthInfo) error {
	if auth == nil {
		return errors.New("auth is nil")
	}
	if c.client == nil {
		return errors.New("client not found")
	}
	return c.client.SetAuthInfo(auth)
}

// GetAuthInfo 获取认证信息
func (c *LocalClient) GetAuthInfo() (*nrpc.AuthInfo, error) {
	if c.client == nil {
		return nil, errors.New("client not found")
	}
	return c.client.GetAuthInfo()
}

// ClientWs 客户端连接
func (c *LocalClient) ClientWs(ctx context.Context, conn *websocket.Conn) {
	defer func() {
		if err := recover(); err != nil {
			c.log(logger.Error, "ClientWs recover err : %v", err)
		}

	}()

	// 链接session
	closeChan := make(chan struct{}, 2)
	defer close(closeChan)
	// 全局client websocket连接
	wsConn := NewWsChannelClient(c.Connect)
	c.client = wsConn
	//default broadcast size eq 512
	wsConn.conn = conn
	wsConn.RoomId = 0
	ctx, cancel := context.WithCancel(ctx)
	//get data from websocket conn
	go c.readPump(ctx, wsConn, closeChan, c.handler)
	//send data to websocket conn
	go c.writePump(ctx, wsConn, closeChan)
	// 等待关闭信号
	<-closeChan
	cancel()
	// 重连
	if c.KeepAlive {
		c.ListenAndServe(context.Background())
	}
}

func (c *LocalClient) Call(ctx context.Context, header message.Header, mtd string, data ...[]byte) ([]byte, error) {
	defer func() {
		if err := recover(); err != nil {
			c.log(logger.Error, "Call recover err : %v", err)
		}
	}()
	if c.client == nil {
		c.log(logger.Error, "client not found")
		return nil, errors.New("client not found")
	}

	// 使用中间件链包装调用
	final := func(ctx context.Context, hdr message.Header, method string, args ...[]byte) ([]byte, error) {
		return c.client.Call(ctx, hdr, method, args...)
	}

	handler := middleware.Chain(c.middlewares, final)
	rst, err := handler(ctx, header, mtd, data...)
	if err != nil {
		return nil, err
	}
	return rst, nil
}

func (c *LocalClient) Push(ctx context.Context, msg *message.Msg) (err error) {
	if c.client == nil {
		c.log(logger.Error, "server not found")
		return errors.New("server not found")
	}
	return c.client.Push(ctx, msg)
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
		if closeChan != nil {
			closeChan <- struct{}{}
		}
	}()
	defer func() {
		ticker.Stop()
		if ch.conn != nil {
			ch.conn.Close()
			ch.conn = nil
		}

	}()
	sliceSize := int(c.SliceSize) // 默认512
	for {
		select {
		case msg, ok := <-ch.send:
			if ch.conn == nil {
				return
			}
			//write data dead time , like http timeout , default 10s
			ch.conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
			if !ok {
				ch.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := slicesTextSend(getSliceName(), ch.conn, msg.Body, sliceSize); err != nil {
				return
			}
		case msg, ok := <-ch.rpcCaller:
			if ch.conn == nil {
				return
			}
			// @call  调用服务器方法
			//write data dead time , like http timeout , default 10s
			ch.conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
			if !ok {
				ch.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			// fmt.Println("Call LocalClient-22-:", msg)
			if err := slicesTextSend(getSliceName(), ch.conn, utils.Serialize(msg), sliceSize); err != nil {
				c.log(logger.Error, "slicesTextSend err = %v", err.Error())
				return
			}
			// fmt.Println("rpcCaller message:", "message, ok := <-ch.rpcCaller")
		case msg, ok := <-ch.rpcBacker:
			if ch.conn == nil {
				return
			}
			// @reply  服务器返回调用结果
			//write data dead time , like http timeout , default 10s
			ch.conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
			if !ok {
				ch.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := slicesTextSend(getSliceName(), ch.conn, utils.Serialize(msg), sliceSize); err != nil {
				return
			}
			// fmt.Println("rpcBacker message:", message)

		case <-ticker.C:
			if ch.conn == nil {
				return
			}
			//heartbeat，if ping error will exit and close current websocket conn
			ch.conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
			if err := ch.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-ctx.Done():
			c.log(logger.Error, "[ws_client]writePump ctx.Done()")
			return
		}
	}
}

func (c *LocalClient) readPump(ctx context.Context, ch *WsChannelClient, closeChan chan struct{}, handler option.IClientHandleMessage) {
	defer func() {
		if err := recover(); err != nil {
			c.log(logger.Error, "readPump recover err : %v", err)
		}
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		if closeChan != nil {
			closeChan <- struct{}{}
		}
	}()
	defer func() {

		if ch.conn != nil {
			ch.conn.Close()
			ch.conn = nil
		}

	}()

	ch.conn.SetReadLimit(c.MaxMessageSize)
	ch.conn.SetReadDeadline(time.Now().Add(c.PongWait))
	ch.conn.SetPongHandler(func(string) error {
		// fmt.Println("pooooooooong")
		ch.conn.SetReadDeadline(time.Now().Add(c.PongWait))
		return nil
	})
	// 要防止OnOpen阻塞，导致readPump阻塞
	if handler != nil {
		go handler.OnOpen(ctx, c, ch)
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
		messageType, msg, err := ch.conn.ReadMessage()
		if err != nil {
			c.log(logger.Error, err.Error())
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				if handler != nil {
					handler.OnError(ctx, c, ch, err)
				}
			} else {
				if handler != nil {
					handler.OnClose(ctx, c, ch)
				}
			}
			c.log(logger.Error, "readPump，ch.conn.ReadMessage return")
			return
		}
		if len(msg) == 0 || messageType == -1 {
			c.log(logger.Info, "readPump，message is nil or messageType is -1")
			continue
		}
		// 消息体可能太大，需要分片接收后再解析
		// 实现分片接收的函数
		m, err := receiveMessage(ch.conn, byte(messageType), msg)
		// fmt.Println("Call LocalClient-44-:", messageType, msg)
		if err != nil {
			if handler != nil {
				handler.OnError(ctx, c, ch, err)
			}
			continue
		}
		tlvFrame, err := tlv.Deserialize(m)
		if err == nil {
			m = tlvFrame.Value()
		}
		// fmt.Println("Call LocalClient-44-:", m)
		var connReq utils.JsonValue
		if reqErr := json.Unmarshal(m, &connReq); reqErr == nil {
			action := int(connReq.Int64("action"))
			protocol := int(connReq.Int64("protocol"))
			idstr := connReq.String("id")

			// 提取 data 字段（避免重复访问）
			var data []byte
			if protocol == 1 {
				// TextMessage 协议：data 是字符串
				data = []byte(connReq.String("data"))
			} else {
				// BinaryMessage 协议：data 是字节数组
				data = connReq.Bytes("data")
			}

			if action == actions.ACTION_CALL {
				if ch.rpc_io < 0 {
					ch.rpc_io = 0
				}
				// 使用对象池获取 RpcCaller，减少 GC 压力
				args := c.getRpcCaller()
				args.Id = idstr
				args.Protocol = protocol
				args.Action = action
				args.Header = connReq.MapString("header")
				args.Method = connReq.String("method")
				args.Data = data
				args.Args = connReq.BytesArray("args")
				args.Channel = ch
				c.HandleCall(ctx, args)
				// 注意：HandleCall 不负责归还对象，对象由调用方在处理完成后归还
				continue
			} else if action == actions.ACTION_REPLY {
				ch.rpc_io--
				if ch.rpc_io < -50 {
					ch.rpc_io = 0
					continue
				}
				errStr := connReq.String("error")
				if errStr != "" {
					// 处理服务器返回的错误
					backObj := message.NewWsJsonBackError(idstr, []byte(errStr))
					ch.rpcBacker <- backObj
					continue
				}
				backObj := message.NewWsJsonBackSuccess(idstr, data)
				ch.rpcBacker <- backObj
				continue
			}
		} else {
			// 处理其他消息类型
			fmt.Println("Call LocalClient-44-:", err)
		}

		if handler != nil {
			handler.OnData(ctx, c, ch, messageType, m)
		}
	}
}

// HandleMessage 处理来自服务器的消息
// 有两种情况：
// 1. 服务器主动推送消息，需要调用本地方法处理
// 2. 服务器调用本地方法，需要返回结果
func (c *LocalClient) HandleCall(ctx context.Context, msgReq *nrpc.RpcCaller) {
	c.serviceMapMu.RLock()
	defer c.serviceMapMu.RUnlock()
	defer func() {
		if err := recover(); err != nil {
			// fmt.Println("------------")
			c.log(logger.Error, "ws_client.HandleMessage recover err : %v", err)
		}
		// 处理完成后归还对象池
		c.putRpcCaller(msgReq)
	}()
	if msgReq.Action == actions.ACTION_CALL {
		rst, err := c.Connect.CallFunc(ctx, nil, msgReq)
		if err != nil {
			msgReq.Channel.ReplyError(msgReq.Id, []byte(err.Error()))
			return
		}
		// fmt.Println("HandleCall ws_client rst:", rst)
		msgReq.Channel.ReplySuccess(msgReq.Id, rst)
		return
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
