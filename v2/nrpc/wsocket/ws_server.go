package wsocket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/w6xian/sloth/v2/actions"
	"github.com/w6xian/sloth/v2/bucket"
	"github.com/w6xian/sloth/v2/internal/logger"
	"github.com/w6xian/sloth/v2/internal/tools"
	"github.com/w6xian/sloth/v2/internal/utils"
	"github.com/w6xian/sloth/v2/message"
	"github.com/w6xian/sloth/v2/nrpc/middleware"
	"github.com/w6xian/sloth/v2/option"
	"github.com/w6xian/sloth/v2/pprof"
	"github.com/w6xian/sloth/v2/types/handler"
	"github.com/w6xian/sloth/v2/types/trpc"
	"github.com/w6xian/tlv"

	"log"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// sync.Pool for JsonValue reuse
var jsonValuePool = sync.Pool{
	New: func() any {
		return &utils.JsonValue{}
	},
}

func getJsonValue() *utils.JsonValue {
	return jsonValuePool.Get().(*utils.JsonValue)
}

func putJsonValue(v *utils.JsonValue) {
	if v != nil {
		*v = utils.JsonValue{}
		jsonValuePool.Put(v)
	}
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
	middlewares     []middleware.Middleware
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
	if s.Connect == nil {
		fmt.Println("WsServer Connect is nil")
		return
	}
	s.Connect.Log(level, "[WsServer]"+line, args...)
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
	pprof.New(nil).Buckets = int64(len(bs))
	return s
}

// Use 注册服务端中间件，可多次调用，按注册顺序执行。
func (s *WsServer) Use(middlewares ...middleware.Middleware) {
	s.middlewares = append(s.middlewares, middlewares...)
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

// 资源信息
func (s *WsServer) PProf(ctx context.Context) (*pprof.BucketInfo, error) {
	info := &pprof.BucketInfo{
		Buckets: int64(len(s.Buckets)),
		Rooms:   map[int64]pprof.Room{},
	}
	for _, b := range s.Buckets {
		info.Buckets++
		for _, room := range b.GetRooms() {
			info.Rooms[room.Id] = pprof.Room{
				Id:       room.Id,
				Connects: int64(room.OnlineCount),
			}
			info.Connects += int64(room.OnlineCount)
		}
	}
	return info, nil
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
	var release func()
	if sp, ok := s.Connect.(interface{ ConnGuard() *tools.ConnGuard }); ok {
		guard := sp.ConnGuard()
		if guard != nil {
			ip := tools.RemoteIPFromRequest(r, s.Connect.Options().TrustProxyHeaders)
			rel, err := guard.Acquire("ws", ip)
			if err != nil {
				status := http.StatusTooManyRequests
				if gr, ok := err.(*tools.GuardReject); ok && errors.Is(gr.Err, tools.ErrConnBanned) {
					status = http.StatusForbidden
				}
				w.WriteHeader(status)
				return
			}
			release = rel
		}
	}
	var upGrader = websocket.Upgrader{
		ReadBufferSize:  s.ReadBufferSize,
		WriteBufferSize: s.WriteBufferSize,
	}
	//cross origin domain support
	upGrader.CheckOrigin = func(r *http.Request) bool { return true }
	conn, err := upGrader.Upgrade(w, r, nil)
	if err != nil {
		if release != nil {
			release()
		}
		fmt.Println("serveWs err:", err.Error())
		return
	}
	// 一个连接一个channel
	ch := NewWsChannelServer(s.Connect)
	//default broadcast size eq 512
	ch.Conn = conn
	ch.releaseConn = release
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
	// 记录连接数
	pprof.New(nil).NewConeect()
	defer pprof.New(nil).CloseConeect()
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
			GetBucket(context.Background(), s.Buckets, ch.UserId()).DeleteChannel(ch)
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

	// 使用 sync.Pool 复用 JsonValue，减少 GC 压力
	connReq := getJsonValue()
	defer putJsonValue(connReq)

	// OnOpen
	if handler != nil {
		go handler.OnOpen(ctx, s, ch)
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
		// fmt.Println("2ws_server readPump messageType:", messageType, "msg:", string(m), err)
		if err != nil {
			if handler != nil {
				handler.OnError(ctx, s, ch, err)
			}
			continue
		}
		// fmt.Println("4ws_server readPump messageType:", messageType, "msg:", m)
		tlvFrame, err := tlv.Deserialize(m)
		if err == nil {
			m = tlvFrame.Value()
		}
		// var connReq *nrpc.RpcCaller
		// fmt.Println(string(m))
		if reqErr := json.Unmarshal(m, connReq); reqErr == nil {
			// fmt.Println("----------", connReq.Int64("action"))
			action := int(connReq.Int64("action"))
			protocol := int(connReq.Int64("protocol"))
			idstr := connReq.String("id")
			// fmt.Println("3ws_server readPump messageType:", "action:", action, "protocol:", protocol, "id:", idstr)
			if action == actions.ACTION_CALL {
				if atomic.LoadInt64(&ch.rpc_io) < 0 {
					atomic.StoreInt64(&ch.rpc_io, 0)
				}
				// 调用方法
				hdr := message.GetHeader()
				connReq.MapStringInto("header", hdr)
				args := &trpc.RpcCaller{
					Id:       idstr,
					Protocol: protocol,
					Action:   action,
					Header:   hdr,
					Method:   connReq.String("method"),
					Args:     connReq.BytesArray("args"),
				}
				// 调试Args
				// fmt.Println("--------args:", args.Args)
				b := connReq.Bytes("data")
				if protocol == 1 {
					args.Data = []byte(connReq.String("data"))
				}
				args.Data = b
				// 链接通道
				args.Channel = ch
				// 调用 connect.CallFunc 方法
				hctx := context.Background()
				s.HandleCall(hctx, args)
				message.PutHeader(message.Header(args.Header))
				args.Header = nil
				continue
			} else if action == actions.ACTION_REPLY {
				// 防止被恶意阻塞，这里也有个问题，同一个方法，不能一直返回
				if atomic.AddInt64(&ch.rpc_io, -1) < -100 {
					atomic.StoreInt64(&ch.rpc_io, 0)
					continue
				}
				errStr := connReq.String("error")
				if errStr != "" {
					backObj := ch.getBackObj()
					backObj.Id = connReq.String("id")
					backObj.Action = actions.ACTION_REPLY
					backObj.Type = message.TextMessage
					backObj.Data = nil
					backObj.Error = errStr
					payload := utils.Serialize(backObj)
					ch.putBackObj(backObj)
					select {
					case ch.rpcResult <- payload:
					case <-ctx.Done():
						return
					}
					continue
				}
				b := connReq.Bytes("data")
				if protocol == 1 {
					b = []byte(connReq.String("data"))
				}
				backObj := ch.getBackObj()
				backObj.Id = connReq.String("id")
				backObj.Action = actions.ACTION_REPLY
				backObj.Type = message.TextMessage
				backObj.Data = b
				backObj.Error = ""
				payload := utils.Serialize(backObj)
				ch.putBackObj(backObj)
				select {
				case ch.rpcResult <- payload:
				case <-ctx.Done():
					return
				}
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

// HandleCall 处理来自客户端的 RPC 调用
func (s *WsServer) HandleCall(ctx context.Context, msgReq *trpc.RpcCaller) {
	s.serviceMapMu.RLock()
	defer s.serviceMapMu.RUnlock()

	defer func() {
		if err := recover(); err != nil {
			log.Println("ws_server.HandleCall recover err :", err)
		}
	}()
	// 使用中间件链包装业务调用
	final := func(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {
		msg := &trpc.RpcCaller{
			Id:      msgReq.Id,
			Channel: msgReq.Channel,
			Header:  header,
			Method:  mtd,
			Args:    args,
			Data:    msgReq.Data,
		}
		// fmt.Println("ws_server.HandleCall msg:", msg)
		// fmt.Println("ws_server.HandleCall msg:", msg.Args)
		rst, err := s.Connect.CallFunc(ctx, s, msg)
		if err != nil {
			return nil, err
		}
		msg.Channel.ReplySuccess(msg.Id, rst)
		return rst, nil
	}
	handler := middleware.Chain(s.middlewares, final)

	// 执行中间件链
	if _, err := handler(ctx, msgReq.Header, msgReq.Method, msgReq.Args...); err != nil {
		msgReq.Channel.ReplyError(msgReq.Id, []byte(err.Error()))
	}
}

//
