package wsocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/w6xian/sloth/v3/actions"
	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/decoder/fn"
	"github.com/w6xian/sloth/v3/internal/logger"
	"github.com/w6xian/sloth/v3/internal/tools"
	"github.com/w6xian/sloth/v3/internal/utils"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/types/handler"
	"github.com/w6xian/sloth/v3/types/trpc"
	"github.com/w6xian/tlv"

	"log"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

func allowWebSocketOrigin(r *http.Request) bool {
	if r == nil {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || strings.TrimSpace(u.Host) == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

type WsServer struct {
	Buckets         []*bucket.Bucket
	bucketIdx       uint32
	serviceMapMu    sync.RWMutex
	Connect         trpc.ICallRpc
	uriPath         string
	handler         handler.IServerHandleMessage
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
	panic("SetAddress is not implemented")
	return nil
}

func (s *WsServer) SetServerHandleMessage(handler handler.IServerHandleMessage) error {
	s.handler = handler
	return nil
}
func (s *WsServer) SetClientHandleMessage(handler handler.IClientHandleMessage) error {
	// 空方法
	panic("SetClientHandleMessage is not implemented")
	return nil
}

func (s *WsServer) log(level logger.LogLevel, line string, args ...any) {
	log.Println("[WsServer]", line, args)
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
	for _, opt := range opts {
		opt(s)
	}
	return s
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
func (s *WsServer) Broadcast(ctx context.Context, msg *message.Msg) error {
	for _, b := range s.Buckets {
		if b == nil {
			continue
		}
		for _, room := range b.GetRooms() {
			room.Push(ctx, msg)
		}
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
	upGrader.CheckOrigin = allowWebSocketOrigin
	conn, err := upGrader.Upgrade(w, r, nil)
	if err != nil {
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
			s.log(logger.Error, "writePump 111 recover err : %v", err)
		}
	}()
	//PingPeriod default eq 54s
	ticker := time.NewTicker(9 * time.Second)
	defer func() {
		ticker.Stop()
		if ch.Conn != nil {
			ch.Conn.Close()
			ch.Conn = nil
		}
	}()

	for {
		select {
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
		case payload, ok := <-ch.rpcCaller:
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
		case payload, ok := <-ch.rpcBacker:
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

func (s *WsServer) readPump(ctx context.Context, ch *WsChannelServer, handler handler.IServerHandleMessage) {
	defer func() {
		if err := recover(); err != nil {
			s.log(logger.Error, "readPump recover err : %v", err)
		}
	}()
	defer func() {
		if ch.Room() == nil || ch.UserId() == 0 {
			GetBucket(ctx, s.Buckets, ch.UserId()).DeleteChannel(ch)
		}
		if ch.Conn != nil {
			ch.Conn.Close()
			ch.Conn = nil
		}
	}()

	ch.Conn.SetReadLimit(s.MaxMessageSize)
	ch.Conn.SetReadDeadline(time.Now().Add(s.PongWait))
	ch.Conn.SetPongHandler(func(string) error {
		ch.Conn.SetReadDeadline(time.Now().Add(s.PongWait))
		return nil
	})

	// OnOpen
	if handler != nil {
		err := handler.OnOpen(ctx, s, ch)
		if err != nil {
			handler.OnError(ctx, s, ch, err)
			return
		}
	}

	for {
		messageType, msg, err := ch.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				if handler != nil {
					handler.OnError(ctx, s, ch, err)
				}
				return
			} else {
				if handler != nil {
					handler.OnClose(ctx, s, ch)
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
			if handler != nil {
				handler.OnError(ctx, s, ch, err)
			}
			continue
		}
		tlvFrame, err := tlv.Deserialize(m)
		if err == nil {
			m = tlvFrame.Value()
		}
		if _, err := fn.Action(m); err == nil {
			if err := s.HandleFn(ctx, ch, m); err != nil {
				if handler != nil {
					handler.OnError(ctx, s, ch, err)
				}
			}
			continue
		}
		if handler != nil {
			handler.OnData(ctx, s, ch, messageType, m)
		}
	}
}

func (s *WsServer) HandleFn(ctx context.Context, ch *WsChannelServer, data []byte) error {
	action, err := fn.Action(data)
	if err != nil {
		return err
	}
	id := fn.Id(data)
	body := fn.Data(data)
	switch action {
	case actions.ACTION_CALL:
		fx := getCallObj()
		err := json.Unmarshal(body, fx)
		if err != nil {
			log.Println(logger.Error, "server readPump，json.Unmarshal err:%v", err)
			return err
		}
		if !s.Connect.IsRegisteredService(fx.Method) {
			resp, lerr := s.Connect.CallNetFunc(ctx, fx.Method, id, data)
			ch.Reply(id, resp, lerr)
			return nil
		}
		// 链接通道
		// fx.Channel = ch
		// 调用 connect.CallFunc 方法
		rst, err := s.Connect.CallFunc(ctx, s, &trpc.RpcCaller{
			Method:  fx.Method,
			Data:    body,
			Channel: ch,
			Header:  fx.Header,
			Args:    fx.Args,
		})
		ch.Reply(id, rst, err)
		return nil
	case actions.ACTION_REPLY_SUCCESS, actions.ACTION_REPLY_ERROR:
		select {
		case ch.rpcResult <- data:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	default:
		log.Fatalln(logger.Error, "server readPump，action:%d is not valid", action)
		return nil
	}
}

//
