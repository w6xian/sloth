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

func (s *LocalClient) ListenAndServe() error {
	addr := fmt.Sprintf("ws://%s%s", s.serverUri, s.uriPath)
	_, err := url.ParseRequestURI(addr)
	if err == nil {
		conn, _, err := websocket.DefaultDialer.Dial(addr, http.Header{
			"app_id": []string{id.ShortID()},
		})
		if err != nil {
			fmt.Println("websocket.DefaultDialer.Dial err = ", err.Error())
			time.Sleep(5 * time.Second)
			s.ListenAndServe()
			return err
		}
		s.ClientWs(conn)
	}
	return nil
}

// ClientWs 客户端连接
func (c *LocalClient) ClientWs(conn *websocket.Conn) {
	// 链接session
	closeChan := make(chan bool, 1)
	// 全局client websocket连接
	wsConn := NewWsClient(2, 10)
	c.client = wsConn
	//default broadcast size eq 512
	wsConn.conn = conn
	wsConn.RoomId = 1
	//send data to websocket conn
	go c.writePump(wsConn)
	//get data from websocket conn
	go c.readPump(wsConn, closeChan, c.handler)
	// 等待关闭信号
	<-closeChan
	// 重连
	c.ListenAndServe()
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

func (s *LocalClient) Push(msg *message.Msg) (err error) {
	if s.client == nil {
		return errors.New("server not found")
	}
	return s.client.Push(msg)
}

func (s *LocalClient) writePump(ch *WsClient) {
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
		case message, ok := <-ch.send:
			//write data dead time , like http timeout , default 10s
			ch.conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				fmt.Println("SetWriteDeadline not ok")
				ch.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			sl, err := getSlice(message.Body)
			if err != nil {
				continue
			}
			if err := slicesSend(sl.N, ch.conn, message.Body, 32); err != nil {
				return
			}
		case message, ok := <-ch.rpcCaller:
			//write data dead time , like http timeout , default 10s
			ch.conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				ch.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := slicesSend(getSliceName(), ch.conn, utils.Serialize(message), 32); err != nil {
				fmt.Println("slicesSend err = ", err.Error())
				return
			}
			// fmt.Println("rpcCaller message:", "message, ok := <-ch.rpcCaller")
		case message, ok := <-ch.rpcBacker:
			//write data dead time , like http timeout , default 10s
			ch.conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				ch.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := slicesSend(getSliceName(), ch.conn, utils.Serialize(message), 32); err != nil {
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

func (c *LocalClient) readPump(ch *WsClient, closeChan chan bool, handler IClientHandleMessage) {
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
		ch.conn.SetReadDeadline(time.Now().Add(c.PongWait))
		return nil
	})

	for {
		// 来自服务器的消息
		msgType, msg, err := ch.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				fmt.Println("readPump ReadMessage err = ", err.Error())
				return
			}
		}
		if msg == nil || msgType == -1 {
			fmt.Println("readPump msgType:", msgType)
			return
		}
		// 消息体可能太大，需要分片接收后再解析
		// 实现分片接收的函数
		m, err := receiveMessage(ch.conn, msg)
		if err != nil {
			fmt.Println("receiveMessage err = ", err.Error())
			continue
		}
		// fmt.Println("readPump msgType:", msgType, "message:", string(m))
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
				c.HandleCall(ch, connReq)
				continue
			}
		}

		if handler != nil {
			handler.HandleMessage(c, ch, msgType, m)
		}
	}
}

// HandleMessage 处理来自服务器的消息
// 有两种情况：
// 1. 服务器主动推送消息，需要调用本地方法处理
// 2. 服务器调用本地方法，需要返回结果
func (c *LocalClient) HandleCall(ch IWsReply, msgReq *nrpc.RpcCaller) {
	c.serviceMapMu.RLock()
	defer c.serviceMapMu.RUnlock()
	defer func() {
		if err := recover(); err != nil {
			fmt.Println("HandleMessage recover err :", err)
		}
	}()
	if msgReq.Action == actions.ACTION_CALL {
		rst, err := c.Connect.CallFunc(msgReq)
		if err != nil {
			ch.ReplyError(msgReq.Id, []byte(err.Error()))
			return
		}
		ch.ReplySuccess(msgReq.Id, rst)
		return
	}
}
