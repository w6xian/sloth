package tcpsock

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"sync"

	"github.com/w6xian/sloth/v2/bucket"
	"github.com/w6xian/sloth/v2/internal/tools"
	"github.com/w6xian/sloth/v2/message"
	"github.com/w6xian/sloth/v2/nrpc"
	"github.com/w6xian/sloth/v2/nrpc/middleware"
	"github.com/w6xian/sloth/v2/option"
	"github.com/w6xian/sloth/v2/pprof"
)

// TcpServer 实现 nrpc.Listener 接口。
type TcpServer struct {
	ln  net.Listener
	opt *option.Options

	// Bucket 体系（与 WsServer 一致）
	Buckets   []*bucket.Bucket
	bucketIdx uint32

	Connect nrpc.ICallRpc

	// 中间件链（通过 Use 注册）
	middlewares []middleware.Middleware

	handler option.IServerHandleMessage

	closeOnce sync.Once
	closeChan chan struct{}
}

func NewTcpServer(connect nrpc.ICallRpc, opt *option.Options) *TcpServer {
	bsNum := max(1, runtime.NumCPU())
	bs := make([]*bucket.Bucket, bsNum)
	for i := 0; i < bsNum; i++ {
		bs[i] = bucket.NewBucket(
			bucket.WithChannelSize(opt.ChannelSize),
			bucket.WithRoomSize(opt.RoomSize),
			bucket.WithRoutineAmount(opt.RoutineAmount),
			bucket.WithRoutineSize(opt.RoutineSize),
		)
	}
	s := &TcpServer{
		opt:         opt,
		Buckets:     bs,
		bucketIdx:   uint32(len(bs)),
		Connect:     connect,
		middlewares: []middleware.Middleware{},
		closeChan:   make(chan struct{}),
	}
	pprof.New(nil).Buckets = int64(len(bs))
	return s
}

// Use 注册服务端中间件，中间件按注册顺序执行。
//
// 示例：
//
//	server.Use(middleware.Auth(nil))
//	server.Use(middleware.Log(nil, "tcpsock"))
//	server.Use(middleware.Recovery(nil, "tcpsock"))
func (s *TcpServer) Use(middlewares ...middleware.Middleware) {
	s.middlewares = append(s.middlewares, middlewares...)
}

// Listen 启动 TCP 监听，返回 Listener（即自身，实现 nrpc.Listener）。
func (s *TcpServer) Listen(ctx context.Context, addr string) (*TcpServer, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("tcp listen %s err: %w", addr, err)
	}
	s.ln = ln

	go func() {
		<-ctx.Done()
		s.Close()
	}()

	return s, nil
}

// ── nrpc.Listener 接口实现 ──────────────────────────────────────────────

// Accept 阻塞直到有新客户端连接，返回 AuthChannel。
func (s *TcpServer) Accept() (nrpc.AuthChannel, error) {
	conn, err := s.ln.Accept()
	if err != nil {
		return nil, err
	}
	ch := NewTcpChannelServer(s.Connect, conn, s)
	return ch, nil
}

// Close 关闭监听。
func (s *TcpServer) Close() error {
	s.closeOnce.Do(func() {
		close(s.closeChan)
		if s.ln != nil {
			s.ln.Close()
		}
	})
	return nil
}

// Addr 返回监听地址。
func (s *TcpServer) Addr() string {
	if s.ln != nil {
		return s.ln.Addr().String()
	}
	return ""
}

// ── bucket.IBucket 接口（与 WsServer 对齐，供 Connect 统一调用）────

func (s *TcpServer) Bucket(userId int64) *bucket.Bucket {
	userIdStr := fmt.Sprintf("%d", userId)
	idx := tools.CityHash32([]byte(userIdStr), uint32(len(userIdStr))) % s.bucketIdx
	return s.Buckets[idx]
}

func (s *TcpServer) Channel(userId int64) bucket.IChannel {
	return s.Bucket(userId).Channel(userId)
}

func (s *TcpServer) Room(roomId int64) *bucket.Room {
	for _, b := range s.Buckets {
		if room := b.Room(roomId); room != nil {
			return room
		}
	}
	return nil
}

func (s *TcpServer) Broadcast(ctx context.Context, msg *message.Msg) error {
	for _, b := range s.Buckets {
		for _, room := range b.GetRooms() {
			room.Push(ctx, msg)
		}
	}
	return nil
}

// Serve 阻塞运行，循环 Accept 并启动每个连接的 readLoop/writeLoop。
func (s *TcpServer) Serve(ctx context.Context) error {
	s.handler = nil // TCP 暂不使用 IServerHandleMessage
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.closeChan:
			return nil
		default:
		}

		chIface, err := s.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				continue
			}
		}

		ch := chIface.(*TcpChannelServer)
		go ch.Serve(ctx)
	}
}
