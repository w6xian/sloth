package wsocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sloth/actions"
	"sloth/group"
	"sloth/internal/tools"
	"sloth/internal/utils"
	"sloth/message"
	"sloth/nrpc"
	"sync"
	"time"

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
		bs[i] = group.NewBucket(group.BucketOptions{
			ChannelSize:   1024,
			RoomSize:      1024,
			RoutineAmount: 32,
			RoutineSize:   20,
		})
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

func (s *WsServer) ListenAndServe() error {
	s.router.HandleFunc(s.uriPath, func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("new client connect")
		s.serveWs(w, r)
	})
	return nil
}
func (s *WsServer) serveWs(w http.ResponseWriter, r *http.Request) {
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
	ch := NewWsChannel(512)
	//default broadcast size eq 512
	ch.Conn = conn
	//send data to websocket conn
	go s.writePump(ch)
	//get data from websocket conn
	// 需要确认客户端是否合法，一个是JWT,一个是ClientID
	go s.readPump(ch, s.handler)
}

func (s *WsServer) writePump(ch *WsChannel) {
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
		case message, ok := <-ch.broadcast:
			//write data dead time , like http timeout , default 10s
			ch.Conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				ch.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := slicesSend(getSliceName(), ch.Conn, message.Body, 32); err != nil {

				return
			}
		case message, ok := <-ch.rpcCaller:
			//write data dead time , like http timeout , default 10s
			ch.Conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				ch.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := slicesSend(getSliceName(), ch.Conn, utils.Serialize(message), 32); err != nil {
				return
			}
		case message, ok := <-ch.rpcBacker:
			//write data dead time , like http timeout , default 10s
			ch.Conn.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if !ok {
				ch.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := slicesSend(getSliceName(), ch.Conn, utils.Serialize(message), 32); err != nil {
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

func (s *WsServer) readPump(ch *WsChannel, handler IServerHandleMessage) {
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
	fmt.Println("readPump start")
	for {
		msgType, msg, err := ch.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				return
			}
		}
		if msg == nil || msgType == -1 {
			fmt.Println("readPump msgType:", msgType)
			return
		}
		// 消息体可能太大，需要分片接收后再解析
		// 实现分片接收的函数
		m, err := receiveMessage(ch.Conn, msg)
		if err != nil {
			fmt.Println("receiveMessage err = ", err.Error())
			continue
		}
		// fmt.Println("readPump msgType:", msgType, "message:", string(m))
		var connReq *nrpc.RpcCaller
		if reqErr := json.Unmarshal(m, &connReq); reqErr == nil {
			if connReq.Action == actions.ACTION_CALL {
				// 调用方法
				s.HandleCall(ch, connReq)
				continue
			} else if connReq.Action == actions.ACTION_REPLY {
				backObj := message.NewWsJsonBackObject(connReq.Id, []byte(connReq.Data))
				ch.rpcBacker <- backObj
				continue
			}
		}

		if handler != nil {
			handler.HandleMessage(s, ch, msgType, m)
		}
	}
}

// HandleCall 处理来自服务器的消息
// 有两种情况：
// 1. 服务器主动推送消息，需要调用本地方法处理
// 2. 服务器调用本地方法，需要返回结果
func (s *WsServer) HandleCall(ch IWsReply, msgReq *nrpc.RpcCaller) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Println("HandleMessage recover err :", err)
		}
	}()

	if msgReq.Action == actions.ACTION_CALL {
		s.serviceMapMu.RLock()
		defer s.serviceMapMu.RUnlock()
		rst, err := s.Connect.CallFunc(msgReq)
		if err != nil {
			return
		}
		ch.Reply(msgReq.Id, rst)
		return
	}
}
