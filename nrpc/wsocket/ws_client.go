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
	"github.com/w6xian/sloth/decoder"
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
	PongWait        time.Duration
	PingPeriod      time.Duration
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

	s.WriteWait = 10 * time.Second
	s.PongWait = 60 * time.Second
	s.PingPeriod = 54 * time.Second
	s.MaxMessageSize = 2048
	s.ReadBufferSize = 4096
	s.WriteBufferSize = 4096
	s.BroadcastSize = 1024

	s.handler = nil
	for _, opt := range options {
		opt(s)
	}
	return s
}

func (s *LocalClient) ListenAndServe(ctx context.Context) error {
	addr := fmt.Sprintf("ws://%s%s", s.serverUri, s.uriPath)
	_, err := url.ParseRequestURI(addr)
	if err == nil {
		conn, _, err := websocket.DefaultDialer.Dial(addr, http.Header{
			"app_id": []string{id.ShortStringID()},
		})
		if err != nil {
			fmt.Println("websocket.DefaultDialer.Dial err = ", err.Error())
			time.Sleep(5 * time.Second)
			s.ListenAndServe(ctx)
			return err
		}
		s.ClientWs(ctx, conn)
	}
	return nil
}

// ClientWs 客户端连接
func (c *LocalClient) ClientWs(ctx context.Context, conn *websocket.Conn) {
	// 链接session
	closeChan := make(chan bool, 1)
	// 全局client websocket连接
	wsConn := NewWsClient(2, 2)
	c.client = wsConn
	//default broadcast size eq 512
	wsConn.conn = conn
	wsConn.RoomId = 1
	//send data to websocket conn
	go c.writePump(ctx, wsConn)
	//get data from websocket conn
	go c.readPump(ctx, wsConn, closeChan, c.handler)
	// 等待关闭信号
	<-closeChan
	// 重连
	c.ListenAndServe(ctx)
}

func (s *LocalClient) Call(ctx context.Context, mtd string, data any) ([]byte, error) {
	if s.client == nil {
		return nil, errors.New("server not found")
	}

	resp, err := s.client.Call(ctx, mtd, data)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *LocalClient) Push(ctx context.Context, msg *message.Msg) (err error) {
	if s.client == nil {
		return errors.New("server not found")
	}
	return s.client.Push(ctx, msg)
}

func (s *LocalClient) writePump(ctx context.Context, ch *WsClient) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Println("writePump recover err :", err)
		}
	}()
	//PingPeriod default eq 54s
	ticker := time.NewTicker(s.PingPeriod)
	defer func() {
		ticker.Stop()
		ch.conn.Close()
		ch.conn = nil
	}()

	for {
		select {
		case msg, ok := <-ch.send:
			//write data dead time , like http timeout , default 10s
			ch.conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				fmt.Println("SetWriteDeadline not ok")
				ch.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			sl, err := getSlice(msg.Body)
			if err != nil {
				continue
			}
			if err := slicesTextSend(sl.N, ch.conn, msg.Body, 32); err != nil {
				return
			}
		case msg, ok := <-ch.rpcCaller:
			//write data dead time , like http timeout , default 10s
			ch.conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				ch.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := slicesTextSend(getSliceName(), ch.conn, utils.Serialize(msg), 512); err != nil {
				fmt.Println("slicesTextSend err = ", err.Error())
				return
			}
			fmt.Println("rpcCaller message:", "message, ok := <-ch.rpcCaller")
		case msg, ok := <-ch.rpcBacker:
			//write data dead time , like http timeout , default 10s
			ch.conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				ch.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if msg.Type == websocket.BinaryMessage {
				slicesBinarySend(msg.Id, ch.conn, msg.Data.([]byte), 512)
				continue
			}

			if err := slicesTextSend(getSliceName(), ch.conn, utils.Serialize(msg), 512); err != nil {
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

func (c *LocalClient) readPump(ctx context.Context, ch *WsClient, closeChan chan bool, handler IClientHandleMessage) {
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
		fmt.Println("pooooooooong")
		ch.conn.SetReadDeadline(time.Now().Add(c.PongWait))
		return nil
	})

	for {
		// 来自服务器的消息
		messageType, msg, err := ch.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				fmt.Println("readPump ReadMessage err = ", err.Error())
				return
			}
		}
		if msg == nil || messageType == -1 {
			fmt.Println("readPump messageType:", messageType)
			return
		}
		fmt.Println("------------", len(msg))
		fmt.Println("readPump messageType:", messageType, "message:", string(msg))
		if messageType == websocket.BinaryMessage {
			fmt.Println("010101010101010101010101010101010101010101010101")
			// fmt.Println("readPump messageType:", messageType, "message:", string(msg))
			if frame, hdcErr := receiveHdCFrame(ch.conn, msg); hdcErr == nil {
				hdc, err := decoder.DecodeHdC(frame)
				if err != nil {
					fmt.Println("DecodeHdC err = ", err.Error())
					continue
				}
				// call
				if hdc.FunctionCode() == 0xFF {
					// c.HandleCall(ctx, ch, hdc)
					continue
				}
				fmt.Println("readPump messageType:", messageType, "message:", hdc.Id(), hdc.FunctionCode(), hdc.Data())
				// reply error
				if hdc.FunctionCode() == 0x00 {
					// 处理服务器返回的错误
					backObj := message.NewWsJsonBackError(hdc.Id(), []byte(hdc.Data()))
					ch.rpcBacker <- backObj
					continue
				}
				// reply success
				if hdc.FunctionCode() == 0x03 {
					backObj := message.NewWsJsonBackSuccess(hdc.Id(), []byte(hdc.Data()), messageType)
					fmt.Println("========================")
					ch.rpcBacker <- backObj
					fmt.Println("========================!", cap(ch.rpcBacker), len(ch.rpcBacker))
					continue
				}
				// 处理HdC消息
				handler.HandleMessage(ctx, c, ch, messageType, frame)
				continue
			}
		}
		// 消息体可能太大，需要分片接收后再解析
		// 实现分片接收的函数
		m, err := receiveMessage(ch.conn, messageType, msg)
		if err != nil {
			fmt.Println("client receiveMessage err = ", err.Error())
			continue
		}
		// fmt.Println("readPump messageType:", messageType, "message:", string(m))
		var connReq *nrpc.RpcCaller
		if reqErr := json.Unmarshal(m, &connReq); reqErr == nil {
			if connReq.Action == actions.ACTION_REPLY {
				// 处理服务器返回的结果
				if connReq.Error != "" {
					// 处理服务器返回的错误
					backObj := message.NewWsJsonBackError(connReq.Id, []byte(connReq.Error))
					ch.rpcBacker <- backObj
					continue
				}
				backObj := message.NewWsJsonBackSuccess(connReq.Id, []byte(connReq.Data))
				ch.rpcBacker <- backObj
				continue
			} else if connReq.Action == actions.ACTION_CALL {
				c.HandleCall(ctx, ch, connReq)
				continue
			}
		}

		if handler != nil {
			handler.HandleMessage(ctx, c, ch, messageType, m)
		}
	}
}

// HandleMessage 处理来自服务器的消息
// 有两种情况：
// 1. 服务器主动推送消息，需要调用本地方法处理
// 2. 服务器调用本地方法，需要返回结果
func (c *LocalClient) HandleCall(ctx context.Context, ch IWsReply, msgReq *nrpc.RpcCaller) {
	c.serviceMapMu.RLock()
	defer c.serviceMapMu.RUnlock()
	defer func() {
		if err := recover(); err != nil {
			fmt.Println("HandleMessage recover err :", err)
		}
	}()
	if msgReq.Action == actions.ACTION_CALL {
		rst, err := c.Connect.CallFunc(ctx, msgReq)
		if err != nil {
			ch.ReplyError(msgReq.Id, []byte(err.Error()))
			return
		}
		ch.ReplySuccess(msgReq.Id, rst)
		return
	}
}
