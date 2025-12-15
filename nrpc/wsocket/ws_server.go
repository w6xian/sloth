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
	"github.com/w6xian/sloth/bucket"
	"github.com/w6xian/sloth/decoder/tlv"
	"github.com/w6xian/sloth/internal/logger"
	"github.com/w6xian/sloth/internal/tools"
	"github.com/w6xian/sloth/internal/utils"
	"github.com/w6xian/sloth/message"
	"github.com/w6xian/sloth/nrpc"

	"log"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type WsServer struct {
	Buckets         []*bucket.Bucket
	bucketIdx       uint32
	serviceMapMu    sync.RWMutex
	Connect         nrpc.ICallRpc
	uriPath         string
	handler         IServerHandleMessage
	router          *mux.Router
	WriteWait       time.Duration
	ReadWait        time.Duration
	PongWait        time.Duration
	PingPeriod      time.Duration
	MaxMessageSize  int64
	ReadBufferSize  int
	WriteBufferSize int
	BroadcastSize   int
	SliceSize       int64
}

func (s *WsServer) log(level logger.LogLevel, line string, args ...any) {
	s.Connect.Log(level, "[WsServer]"+line, args...)
}

func NewWsServer(server nrpc.ICallRpc, options ...ServerOption) *WsServer {
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
	s := &WsServer{
		Buckets:         bs,
		bucketIdx:       uint32(len(bs)),
		Connect:         server,
		uriPath:         "/ws",
		handler:         nil,
		router:          mux.NewRouter(),
		WriteWait:       opt.WriteWait,
		ReadWait:        opt.ReadWait,
		PongWait:        opt.PongWait,
		PingPeriod:      opt.PingPeriod,
		MaxMessageSize:  opt.MaxMessageSize,
		ReadBufferSize:  opt.ReadBufferSize,
		WriteBufferSize: opt.WriteBufferSize,
		BroadcastSize:   opt.BroadcastSize,
		SliceSize:       opt.SliceSize,
	}
	for _, opt := range options {
		opt(s)
	}
	return s
}
func (s *WsServer) Bucket(userId int64) *bucket.Bucket {
	userIdStr := fmt.Sprintf("%d", userId)
	idx := tools.CityHash32([]byte(userIdStr), uint32(len(userIdStr))) % s.bucketIdx
	return s.Buckets[idx]
}

func (s *WsServer) Channel(userId int64) bucket.IChannel {
	return s.Bucket(userId).Channel(userId)
}

func (s *WsServer) Room(roomId int64) *bucket.Room {
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
		s.log(logger.Info, "new client connect")
		s.serveWs(ctx, w, r)
	})
	return nil
}
func (s *WsServer) serveWs(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var upGrader = websocket.Upgrader{
		ReadBufferSize:  s.ReadBufferSize,
		WriteBufferSize: s.WriteBufferSize,
	}
	//cross origin domain support
	upGrader.CheckOrigin = func(r *http.Request) bool { return true }
	conn, err := upGrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("serveWs err:", err.Error())
		return
	}
	// 一个连接一个channel
	ch := NewWsChannelServer(s.Connect)
	//default broadcast size eq 512
	ch.Conn = conn
	// 需要确认客户端是否合法，一个是JWT,一个是ClientID
	go s.readPump(ctx, ch, s.handler)
	//send data to websocket conn
	go s.writePump(ctx, ch)
	//get data from websocket conn

}

func (s *WsServer) writePump(ctx context.Context, ch *WsChannelServer) {
	defer func() {
		if err := recover(); err != nil {
			s.log(logger.Error, "writePump recover err : %v", err)
		}
	}()
	//PingPeriod default eq 54s
	ticker := time.NewTicker(9 * time.Second)
	defer func() {
		ticker.Stop()
		ch.Conn.Close()
	}()
	// go func() {
	// 	msg := message.NewMessage(websocket.TextMessage, []byte("hello"))
	// 	ch.broadcast <- msg
	// }()
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

func (s *WsServer) readPump(ctx context.Context, ch *WsChannelServer, handler IServerHandleMessage) {
	defer func() {
		if err := recover(); err != nil {
			s.log(logger.Error, "readPump recover err : %v", err)
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
	// OnOpen
	go handler.OnOpen(ctx, s, ch)

	for {
		messageType, msg, err := ch.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				handler.OnError(ctx, s, ch, err)
				return
			}
		}
		if msg == nil || messageType == -1 {
			handler.OnClose(ctx, s, ch)
			return
		}
		//@call HandleCall 处理调用方法
		// 消息体可能太大，需要分片接收后再解析
		// 实现分片接收的函数
		// fmt.Println("1ws_server readPump messageType:", messageType, "msg:", string(msg))
		m, err := receiveMessage(ch.Conn, byte(messageType), msg)
		// fmt.Println("2ws_server readPump messageType:", messageType, "msg:", string(m), err)
		if err != nil {
			handler.OnError(ctx, s, ch, err)
			continue
		}
		// fmt.Println("4ws_server readPump messageType:", messageType, "msg:", m)
		tlvFrame, err := tlv.Deserialize(m)
		if err == nil {
			m = tlvFrame.Value()
		}
		// var connReq *nrpc.RpcCaller
		var connReq utils.JsonValue
		if reqErr := json.Unmarshal(m, &connReq); reqErr == nil {
			// fmt.Println("----------", connReq.Int64("action"))
			action := int(connReq.Int64("action"))
			protocol := int(connReq.Int64("protocol"))
			idstr := connReq.String("id")
			// fmt.Println("3ws_server readPump messageType:", "action:", action, "protocol:", protocol, "id:", idstr)
			if action == actions.ACTION_CALL {
				if ch.rpc_io < 0 {
					ch.rpc_io = 0
				}
				// 调用方法
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
				// 链接通道
				args.Channel = ch
				s.HandleCall(ctx, args)
				continue
			} else if action == actions.ACTION_REPLY {
				ch.rpc_io--
				// 防止被恶意阻塞，这里也有个问题，同一个方法，不能一直返回
				if ch.rpc_io < -100 {
					ch.rpc_io = 0
					continue
				}
				if connReq.String("error") != "" {
					// 处理服务器返回的错误
					backObj := message.NewWsJsonBackError(connReq.String("id"), []byte(connReq.String("error")))
					ch.rpcBacker <- backObj
					continue
				}
				b := connReq.Bytes("data")
				if protocol == 1 {
					// 没有用协议，直接返回字符串
					b = []byte(connReq.String("data"))
				}
				// fmt.Println("5ws_server readPump messageType:", messageType, "msg:", string(b))
				backObj := message.NewWsJsonBackSuccess(connReq.String("id"), b)
				ch.rpcBacker <- backObj
				continue
			}
			// fmt.Println("ws_server readPump err action messageType:", connReq.Action)
		} else {
			log.Println("ws_server readPump err action messageType:", messageType, "msg:", string(m), reqErr)
		}

		if handler != nil {
			handler.OnData(ctx, s, ch, messageType, m)
		}
	}
}

// HandleCall 处理来自服务器的消息
// 有两种情况：
// 1. 服务器主动推送消息，需要调用本地方法处理
// 2. 服务器调用本地方法，需要返回结果
func (s *WsServer) HandleCall(ctx context.Context, msgReq *nrpc.RpcCaller) {
	s.serviceMapMu.RLock()
	defer s.serviceMapMu.RUnlock()

	defer func() {
		if err := recover(); err != nil {
			// fmt.Println("-----------2-")
			log.Println("HandleMessage recover err :", err)
		}
	}()
	// @call HandleCall 处理调用方法
	if msgReq.Action == actions.ACTION_CALL {
		// fmt.Println("ws_server HandleCall messageType:", msgReq.Action, "msg:", string(msgReq.Data))
		rst, err := s.Connect.CallFunc(ctx, s, msgReq)
		// fmt.Println("ws_server HandleCall messageType:", msgReq.Action, "msg:", string(msgReq.Data), "rst:", string(rst), "err:", err)
		if err != nil {
			msgReq.Channel.ReplyError(msgReq.Id, []byte(err.Error()))
			return
		}
		// fmt.Println("ws_server HandleCall ReplySuccess")
		msgReq.Channel.ReplySuccess(msgReq.Id, rst)
		return
	}
}

//
