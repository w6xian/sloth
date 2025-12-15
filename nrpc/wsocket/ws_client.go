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

	"github.com/w6xian/sloth/actions"
	"github.com/w6xian/sloth/decoder/tlv"
	"github.com/w6xian/sloth/internal/logger"
	"github.com/w6xian/sloth/internal/utils"
	"github.com/w6xian/sloth/internal/utils/id"
	"github.com/w6xian/sloth/message"
	"github.com/w6xian/sloth/nrpc"

	"github.com/gorilla/websocket"
)

type LocalClient struct {
	serviceMapMu sync.RWMutex
	uriPath      string
	serverUri    string

	Connect nrpc.ICallRpc
	handler IClientHandleMessage
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
}

func NewLocalClient(connect nrpc.ICallRpc, options ...ClientOption) *LocalClient {
	s := new(LocalClient)
	s.Connect = connect
	s.uriPath = "/ws"
	s.serverUri = "127.0.0.1:8080"

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

	s.handler = nil
	for _, opt := range options {
		opt(s)
	}
	return s
}
func (s *LocalClient) log(level logger.LogLevel, line string, args ...any) {
	s.Connect.Log(level, line, args...)
}

func (s *LocalClient) ListenAndServe(ctx context.Context) error {
	addr := fmt.Sprintf("ws://%s%s", s.serverUri, s.uriPath)
	s.log(logger.Info, "new client connect %s", addr)
	_, err := url.ParseRequestURI(addr)
	if err == nil {
		conn, _, err := websocket.DefaultDialer.Dial(addr, http.Header{
			"app_id": []string{id.ShortStringID()},
		})
		if err != nil {
			// 1-30 秒重试
			retry := utils.RandInt64(1, 30)
			s.log(logger.Error, "connect server %s err : %v, retry after %d seconds", addr, err, retry)
			time.Sleep(time.Duration(retry) * time.Second)
			s.ListenAndServe(ctx)
			return err
		}
		s.ClientWs(ctx, conn)
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
func (c *LocalClient) GetAuthInfo() *nrpc.AuthInfo {
	if c.client == nil {
		return nil
	}
	return c.client.GetAuthInfo()
}

// ClientWs 客户端连接
func (c *LocalClient) ClientWs(ctx context.Context, conn *websocket.Conn) {
	// 链接session
	closeChan := make(chan bool, 1)
	// 全局client websocket连接
	wsConn := NewWsChannelClient()
	c.client = wsConn
	//default broadcast size eq 512
	wsConn.conn = conn
	wsConn.RoomId = 0
	//send data to websocket conn
	go c.writePump(ctx, wsConn)
	//get data from websocket conn
	go c.readPump(ctx, wsConn, closeChan, c.handler)
	// 等待关闭信号
	<-closeChan
	// 重连
	c.ListenAndServe(ctx)
}

func (s *LocalClient) Call(ctx context.Context, mtd string, data ...[]byte) ([]byte, error) {
	if s.client == nil {
		s.log(logger.Error, "client not found")
		return nil, errors.New("client not found")
	}
	// fmt.Println("Call LocalClient-11-:", mtd, data)
	resp, err := s.client.Call(ctx, mtd, data...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *LocalClient) Push(ctx context.Context, msg *message.Msg) (err error) {
	if s.client == nil {
		s.log(logger.Error, "server not found")
		return errors.New("server not found")
	}
	return s.client.Push(ctx, msg)
}

func (s *LocalClient) writePump(ctx context.Context, ch *WsChannelClient) {
	defer func() {
		if err := recover(); err != nil {
			s.log(logger.Error, "writePump recover err : %v", err)
		}
	}()
	//PingPeriod default eq 54s
	ticker := time.NewTicker(s.PingPeriod)
	defer func() {
		ticker.Stop()
		ch.conn.Close()
		ch.conn = nil
	}()
	sliceSize := int(s.SliceSize) // 默认512
	for {
		select {
		case msg, ok := <-ch.send:
			//write data dead time , like http timeout , default 10s
			ch.conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				ch.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := slicesTextSend(getSliceName(), ch.conn, msg.Body, sliceSize); err != nil {
				return
			}
		case msg, ok := <-ch.rpcCaller:
			// @call  调用服务器方法
			//write data dead time , like http timeout , default 10s
			ch.conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				ch.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			// fmt.Println("Call LocalClient-22-:", msg)
			if err := slicesTextSend(getSliceName(), ch.conn, utils.Serialize(msg), sliceSize); err != nil {
				s.log(logger.Error, "slicesTextSend err = %v", err.Error())
				return
			}
			// fmt.Println("rpcCaller message:", "message, ok := <-ch.rpcCaller")
		case msg, ok := <-ch.rpcBacker:
			// @reply  服务器返回调用结果
			//write data dead time , like http timeout , default 10s
			ch.conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
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
			ch.conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if err := ch.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *LocalClient) readPump(ctx context.Context, ch *WsChannelClient, closeChan chan bool, handler IClientHandleMessage) {
	defer func() {
		if ch.UserId == 0 {
			ch.conn.Close()
			ch.conn = nil
			return
		}
		ch.conn.Close()
		ch.conn = nil
		closeChan <- true
	}()

	ch.conn.SetReadLimit(c.MaxMessageSize)
	ch.conn.SetReadDeadline(time.Now().Add(c.PongWait))
	ch.conn.SetPongHandler(func(string) error {
		// fmt.Println("pooooooooong")
		ch.conn.SetReadDeadline(time.Now().Add(c.PongWait))
		return nil
	})
	// onOpen
	handler.OnOpen(ctx, c, ch)
	for {
		// 来自服务器的消息
		messageType, msg, err := ch.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				handler.OnError(ctx, c, ch, err)
				return
			}
		}
		// fmt.Println("Call LocalClient-44-:", messageType, msg)
		if msg == nil || messageType == -1 {
			handler.OnClose(ctx, c, ch)
			return
		}
		// 消息体可能太大，需要分片接收后再解析
		// 实现分片接收的函数
		m, err := receiveMessage(ch.conn, byte(messageType), msg)
		// fmt.Println("Call LocalClient-44-:", messageType, msg)
		if err != nil {
			handler.OnError(ctx, c, ch, err)
			continue
		}
		tlvFrame, err := tlv.Deserialize(m)
		if err == nil {
			m = tlvFrame.Value()
		}
		// fmt.Println("Call LocalClient-44-:", m)
		// var connReq *nrpc.RpcCaller
		var connReq utils.JsonValue
		if reqErr := json.Unmarshal(m, &connReq); reqErr == nil {
			action := int(connReq.Int64("action"))
			protocol := int(connReq.Int64("protocol"))
			idstr := connReq.String("id")
			if action == actions.ACTION_CALL {
				args := &nrpc.RpcCaller{
					Id:       idstr,
					Protocol: protocol,
					Action:   action,
					Method:   connReq.String("method"),
					Args:     connReq.BytesArray("args"),
				}
				b := connReq.Bytes("data")
				if protocol == 1 {
					args.Data = []byte(connReq.String("data"))
				}
				args.Data = b
				args.Channel = ch
				c.HandleCall(ctx, args)
				continue
			} else if action == actions.ACTION_REPLY {
				if connReq.String("error") != "" {
					// 处理服务器返回的错误
					backObj := message.NewWsJsonBackError(idstr, []byte(connReq.String("error")))
					ch.rpcBacker <- backObj
					continue
				}
				b := connReq.Bytes("data")
				if protocol == 1 {
					b = []byte(connReq.String("data"))
				}
				backObj := message.NewWsJsonBackSuccess(idstr, b)
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
			c.log(logger.Error, "HandleMessage recover err : %v", err)
		}
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

// // 实现IBucket接口 (为了统一，无其他)
// func (s *LocalClient) Bucket(userId int64) *group.Bucket {
// 	return nil
// }

// func (s *LocalClient) Channel(userId int64) group.IChannel {
// 	return nil
// }

// func (s *LocalClient) Room(roomId int64) *group.Room {
// 	return nil
// }

// func (s *LocalClient) Broadcast(ctx context.Context, msg *message.Msg) (err error) {
// 	return nil
// }
