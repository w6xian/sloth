package wsocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/w6xian/sloth/actions"
	"github.com/w6xian/sloth/group"
	"github.com/w6xian/sloth/internal/tools"
	"github.com/w6xian/sloth/internal/utils"
	"github.com/w6xian/sloth/message"
	"github.com/w6xian/sloth/nrpc"

	"log"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"modernc.org/mathutil"
)

type WsServer struct {
	Buckets         []*group.Bucket
	bucketIdx       uint32
	serviceMapMu    sync.RWMutex
	Connect         nrpc.ICallRpc
	uriPath         string
	handler         IServerHandleMessage
	router          *mux.Router
	WriteWait       time.Duration
	PongWait        time.Duration
	PingPeriod      time.Duration
	MaxMessageSize  int64
	ReadBufferSize  int
	WriteBufferSize int
	BroadcastSize   int
}

func NewWsServer(server nrpc.ICallRpc, options ...ServerOption) *WsServer {
	bsNum := 1
	bsNum = mathutil.Min(bsNum, runtime.NumCPU())
	//init Connect layer rpc server, logic client will call this
	bs := make([]*group.Bucket, bsNum)
	for i := 0; i < bsNum; i++ {
		bs[i] = group.NewBucket(
			group.WithChannelSize(1024),
			group.WithRoomSize(1024),
			group.WithRoutineAmount(32),
			group.WithRoutineSize(20),
		)
	}
	s := &WsServer{
		Buckets:         bs,
		bucketIdx:       uint32(len(bs)),
		Connect:         server,
		uriPath:         "/ws",
		handler:         nil,
		router:          mux.NewRouter(),
		WriteWait:       10 * time.Second,
		PongWait:        60 * time.Second,
		PingPeriod:      54 * time.Second,
		MaxMessageSize:  2048,
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		BroadcastSize:   1024,
	}
	for _, opt := range options {
		opt(s)
	}
	return s
}
func (s *WsServer) Bucket(userId int64) *group.Bucket {
	userIdStr := fmt.Sprintf("%d", userId)
	// fmt.Println("userIdStr:", userIdStr)
	idx := tools.CityHash32([]byte(userIdStr), uint32(len(userIdStr))) % s.bucketIdx
	// fmt.Println("userId:", userId, "idx:", idx)
	return s.Buckets[idx]
}

func (s *WsServer) Channel(userId int64) group.IChannel {
	return s.Bucket(userId).Channel(userId)
}

func (s *WsServer) Room(roomId int64) *group.Room {
	for _, b := range s.Buckets {
		if room := b.Room(roomId); room != nil {
			return room
		}
	}
	return nil
}
func (s *WsServer) Broadcast(ctx context.Context, msg *message.Msg) error {
	for _, b := range s.Buckets {
		for _, room := range b.GetRooms() {
			room.Push(ctx, msg)
		}
	}
	return nil
}

func (s *WsServer) ListenAndServe(ctx context.Context) error {
	s.router.HandleFunc(s.uriPath, func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("new client connect")
		s.serveWs(ctx, w, r)
	})
	return nil
}
func (s *WsServer) serveWs(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var upGrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
	//cross origin domain support
	upGrader.CheckOrigin = func(r *http.Request) bool { return true }
	conn, err := upGrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("serveWs err:", err.Error())
		return
	}
	// 一个连接一个channel
	ch := NewChannel(512)
	//default broadcast size eq 512
	ch.Conn = conn
	//send data to websocket conn
	go s.writePump(ctx, ch)
	//get data from websocket conn
	// 需要确认客户端是否合法，一个是JWT,一个是ClientID
	go s.readPump(ctx, ch, s.handler)
}

func (s *WsServer) writePump(ctx context.Context, ch *Channel) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Println("writePump recover err :", err)
		}
	}()
	//PingPeriod default eq 54s
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		ch.Conn.Close()
	}()

	for {
		select {
		case msg, ok := <-ch.broadcast:
			//write data dead time , like http timeout , default 10s
			ch.Conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				ch.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := slicesTextSend(getSliceName(), ch.Conn, utils.Serialize(msg), 512); err != nil {

				return
			}
		case msg, ok := <-ch.rpcCaller:
			//write data dead time , like http timeout , default 10s
			ch.Conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				ch.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := slicesTextSend(getSliceName(), ch.Conn, utils.Serialize(msg), 512); err != nil {
				return
			}
		case msg, ok := <-ch.rpcBacker:
			//write data dead time , like http timeout , default 10s
			ch.Conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				ch.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if msg.Type == websocket.BinaryMessage {
				slicesBinarySend(msg.Id, ch.Conn, msg.Data.([]byte), 512)
				continue
			}
			if err := slicesTextSend(getSliceName(), ch.Conn, utils.Serialize(msg), 512); err != nil {
				return
			}
		case <-ticker.C:
			//heartbeat，if ping error will exit and close current websocket conn
			ch.Conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if err := ch.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (s *WsServer) readPump(ctx context.Context, ch *Channel, handler IServerHandleMessage) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Println("readPump recover err :", err)
		}
	}()
	defer func() {
		if ch.Room() == nil || ch.UserId() == 0 {
			ch.Conn.Close()
			return
		}
		GetBucket(context.Background(), s.Buckets, ch.UserId()).DeleteChannel(ch)
		ch.Conn.Close()
	}()

	ch.Conn.SetReadLimit(s.MaxMessageSize)
	ch.Conn.SetReadDeadline(time.Now().Add(s.PongWait))
	ch.Conn.SetPongHandler(func(string) error {
		ch.Conn.SetReadDeadline(time.Now().Add(s.PongWait))
		return nil
	})
	log.Println("readPump start")
	for {
		messageType, msg, err := ch.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				return
			}
		}
		if msg == nil || messageType == -1 {
			log.Println("readPump messageType:", messageType)
			return
		}
		if messageType == websocket.BinaryMessage {
			if hdc, hdcErr := receiveHdCFrame(ch.Conn, msg); hdcErr == nil {
				// 处理HdC消息
				handler.HandleMessage(ctx, s, ch, messageType, hdc)
				continue
			}
		}
		// 消息体可能太大，需要分片接收后再解析
		// 实现分片接收的函数
		m, err := receiveMessage(ch.Conn, messageType, msg)
		if err != nil {
			log.Println("server receiveMessage err = ", err.Error())
			continue
		}
		// fmt.Println("readPump msgType:", msgType, "message:", string(m))
		var connReq *nrpc.RpcCaller
		if reqErr := json.Unmarshal(m, &connReq); reqErr == nil {
			if connReq.Action == actions.ACTION_CALL {
				// 调用方法
				s.HandleCall(ctx, ch, connReq)
				continue
			} else if connReq.Action == actions.ACTION_REPLY {
				if connReq.Error != "" {
					// 处理服务器返回的错误
					backObj := message.NewWsJsonBackError(connReq.Id, []byte(connReq.Error))
					ch.rpcBacker <- backObj
					continue
				}
				backObj := message.NewWsJsonBackSuccess(connReq.Id, []byte(connReq.Data))
				ch.rpcBacker <- backObj
				continue
			}
		}

		if handler != nil {
			handler.HandleMessage(ctx, s, ch, messageType, m)
		}
	}
}

// HandleCall 处理来自服务器的消息
// 有两种情况：
// 1. 服务器主动推送消息，需要调用本地方法处理
// 2. 服务器调用本地方法，需要返回结果
func (s *WsServer) HandleCall(ctx context.Context, ch IWsReply, msgReq *nrpc.RpcCaller) {
	s.serviceMapMu.RLock()
	defer s.serviceMapMu.RUnlock()

	defer func() {
		if err := recover(); err != nil {
			log.Println("HandleMessage recover err :", err)
		}
	}()

	if msgReq.Action == actions.ACTION_CALL {
		rst, err := s.Connect.CallFunc(ctx, msgReq)
		if err != nil {
			ch.ReplyError(msgReq.Id, []byte(err.Error()))
			return
		}
		ch.ReplySuccess(msgReq.Id, rst)
		return
	}
}
