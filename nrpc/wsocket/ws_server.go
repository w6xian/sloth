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
	"github.com/w6xian/sloth/v3/internal/utils/array"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/types/handler"
	"github.com/w6xian/sloth/v3/types/trpc"
	"github.com/w6xian/tlv"

	"log"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

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
	header          map[string]string
	originDomain    []string
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
	return nil
}
func (s *WsServer) SetHeader(key string, value string) error {
	s.header[key] = value
	return nil
}
func (s *WsServer) SetOrigin(args ...string) error {
	s.originDomain = append(s.originDomain, args...)
	return nil
}

func (s *WsServer) SetServerHandleMessage(handler handler.IServerHandleMessage) error {
	s.handler = handler
	return nil
}
func (s *WsServer) SetClientHandleMessage(handler handler.IClientHandleMessage) error {
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
		header:          make(map[string]string),
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
func (s *WsServer) AllBuckets() []*bucket.Bucket {
	return s.Buckets
}

// Broadcast 向所有 bucket 的所有房间异步广播。
// 仅把请求投递到各 bucket 的 worker 队列后立即返回，实际下发由 worker 池异步完成；
// 因此 ctx 不参与实际推送（worker 使用 bucket 自身的 background ctx）。
// 若某个房间因队列满投递失败（尽力而为），返回携带丢弃数量的 error，
// 调用方可感知队列压力并决定是否降级。
func (s *WsServer) Broadcast(ctx context.Context, msg *message.Msg) error {
	var dropped int
	for _, b := range s.Buckets {
		if b == nil {
			continue
		}
		dropped += b.BroadcastAll(ctx, msg)
	}
	if dropped > 0 {
		return fmt.Errorf("broadcast dropped %d rooms: worker queue full", dropped)
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
		if s.handler != nil {
			if err := s.handler.OnConnect(ctx, r); err != nil {
				log.Printf("OnConnect err %v", err)
				w.WriteHeader(http.StatusUnauthorized)
				// 关闭连接，返回401错误
				w.Write([]byte(err.Error()))
				return
			}
		}
		s.serveWs(ctx, w, r)
	})
	return nil
}
func (s *WsServer) serveWs(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var upGrader = websocket.Upgrader{
		ReadBufferSize:  s.ReadBufferSize,
		WriteBufferSize: s.WriteBufferSize,
	}
	// 构建header
	header := make(http.Header)
	for k, v := range s.header {
		header[k] = []string{v}
	}

	upGrader.CheckOrigin = func(r *http.Request) bool {
		if r == nil {
			return false
		}
		if len(s.originDomain) == 0 {
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
		// originDomain 中* 表示允许所有域名
		if array.InArray("*", s.originDomain) {
			return true
		}
		return array.InArray(u.Host, s.originDomain)
	}
	conn, err := upGrader.Upgrade(w, r, header)
	if err != nil {
		return
	}
	// 一个连接一个channel
	ch := NewWsChannelServer(s.Connect)
	//default broadcast size eq 512
	ch.Conn = conn
	// 需要确认客户端是否合法，一个是JWT,一个是ClientID
	go s.readPump(ctx, r, ch)
	//send data to websocket conn
	go s.writePump(ctx, r, ch)
	//get data from websocket conn

}

func (s *WsServer) writePump(ctx context.Context, r *http.Request, ch *WsChannelServer) {
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

func (s *WsServer) readPump(ctx context.Context, r *http.Request, ch *WsChannelServer) {
	defer func() {
		if err := recover(); err != nil {
			s.log(logger.Error, "readPump recover err : %v", err)
		}
	}()
	defer func() {
		// 无论登录与否，连接断开都必须从注册表移除。
		// 原条件（ch.Room()==nil || ch.UserId()==0）会放过已登录连接，导致：
		// Web 端 F5 强刷新后旧连接残留 b.chs/房间成员，新连接同 userId 重连又被
		// Bucket.Put 的幂等分支吞掉，后续 CallRoom/Broadcast 全部打在已断开的
		// 旧连接上 → 稳定超时，重启服务端才恢复。
		GetBucket(ctx, s.Buckets, ch.UserId()).DeleteChannel(ch)
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

	// OnOpen  可以发送消息了
	if s.handler != nil {
		go s.handler.OnReady(ctx, r, s, ch)
	}

	for {
		messageType, msg, err := ch.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				if s.handler != nil {
					s.handler.OnError(ctx, r, s, ch, err)
				}
				return
			} else {
				if s.handler != nil {
					s.handler.OnClose(ctx, r, s, ch)
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
			if s.handler != nil {
				s.handler.OnError(ctx, r, s, ch, err)
			}
			continue
		}
		tlvFrame, err := tlv.Deserialize(m)
		if err == nil {
			m = tlvFrame.Value()
		}
		if _, err := fn.Action(m); err == nil {
			if err := s.HandleFn(ctx, r, ch, m); err != nil {
				if s.handler != nil {
					s.handler.OnError(ctx, r, s, ch, err)
				}
			}
			continue
		}
		if s.handler != nil {
			s.handler.OnData(ctx, r, s, ch, messageType, m)
		}
	}
}

func (s *WsServer) HandleFn(ctx context.Context, r *http.Request, ch *WsChannelServer, data []byte) error {
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
			resp, lerr := s.Connect.CallNetFunc(ctx, r, fx.Method, id, data)
			ch.Reply(id, resp, lerr)
			return nil
		}
		// 链接通道
		// fx.Channel = ch
		// 调用 connect.CallFunc 方法
		rst, err := s.Connect.CallFunc(ctx, r, s, &trpc.RpcCaller{
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
		log.Printf("server readPump，action:%d is not valid", action)
		return nil
	}
}

//
