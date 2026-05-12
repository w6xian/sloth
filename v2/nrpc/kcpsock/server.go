package kcpsock

import (
	"context"
	"crypto/sha1"
	"errors"
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
	"github.com/w6xian/sloth/v2/types/handler"
	"github.com/w6xian/sloth/v2/types/trpc"
	"github.com/xtaci/kcp-go/v5"
	"golang.org/x/crypto/pbkdf2"
)

// KcpServer 实现 nrpc.Listener 接口。
type KcpServer struct {
	ln  net.Listener
	opt *option.Options

	// Bucket 体系（与 WsServer 一致）
	Buckets   []*bucket.Bucket
	bucketIdx uint32

	Connect trpc.ICallRpc
	guard   *tools.ConnGuard

	// 中间件链（通过 Use 注册）
	middlewares []middleware.Middleware

	handler handler.IServerHandleMessage

	closeOnce sync.Once
	closeChan chan struct{}

	defaultHeader message.Header
}

// DefaultHeader 获取默认响应头
func (s *KcpServer) DefaultHeader() message.Header {
	return s.defaultHeader
}

func NewKcpServer(connect trpc.ICallRpc, opt *option.Options) *KcpServer {
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
	s := &KcpServer{
		opt:         opt,
		Buckets:     bs,
		bucketIdx:   uint32(len(bs)),
		Connect:     connect,
		middlewares: []middleware.Middleware{},
		closeChan:   make(chan struct{}),
	}
	if sp, ok := connect.(interface{ ConnGuard() *tools.ConnGuard }); ok {
		s.guard = sp.ConnGuard()
	}
	pprof.New(nil).Buckets = int64(len(bs))
	return s
}

// Use 注册服务端中间件，中间件按注册顺序执行。
//
// 示例：
//
//	server.Use(middleware.Auth(nil))
//	server.Use(middleware.Log(nil, "kcpsock"))
//	server.Use(middleware.Recovery(nil, "kcpsock")	)
func (s *KcpServer) Use(middlewares ...middleware.Middleware) {
	s.middlewares = append(s.middlewares, middlewares...)
}

func (s *KcpServer) UseListener(ln net.Listener) {
	s.ln = ln
}

// Listen 启动 KCP 监听，返回 Listener（即自身，实现 nrpc.Listener）。
func (s *KcpServer) Listen(ctx context.Context, addr string) (*KcpServer, error) {
	key := pbkdf2.Key([]byte("demo pass"), []byte("demo salt"), 1024, 32, sha1.New)
	block, err := kcp.NewAESBlockCrypt(key)
	if err != nil {
		return nil, fmt.Errorf("new aes block crypt err: %w", err)
	}
	ln, err := kcp.ListenWithOptions(addr, block, 10, 3)
	if err != nil {
		return nil, fmt.Errorf("kcp listen %s err: %w", addr, err)
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
func (s *KcpServer) Accept() (nrpc.AuthChannel, error) {
	conn, err := s.ln.Accept()
	if err != nil {
		return nil, err
	}
	if s.guard != nil {
		ip := tools.RemoteIPFromAddr(conn.RemoteAddr().String())
		release, rerr := s.guard.Acquire("kcp", ip)
		if rerr != nil {
			conn.Close()
			return nil, rerr
		}
		ch := NewKcpChannelServer(s.Connect, conn, s)
		ch.releaseConn = release
		return ch, nil
	}
	ch := NewKcpChannelServer(s.Connect, conn, s)
	return ch, nil
}

// Close 关闭监听。
func (s *KcpServer) Close() error {
	s.closeOnce.Do(func() {
		close(s.closeChan)
		if s.ln != nil {
			s.ln.Close()
		}
	})
	return nil
}

// Addr 返回监听地址。
func (s *KcpServer) Addr() string {
	if s.ln != nil {
		return s.ln.Addr().String()
	}
	return ""
}

// ── bucket.IBucket 接口（与 WsServer 对齐，供 Connect 统一调用）────

func (s *KcpServer) Bucket(userId int64) *bucket.Bucket {
	userIdStr := fmt.Sprintf("%d", userId)
	idx := tools.CityHash32([]byte(userIdStr), uint32(len(userIdStr))) % s.bucketIdx
	return s.Buckets[idx]
}

func (s *KcpServer) Channel(userId int64) bucket.IChannel {
	return s.Bucket(userId).Channel(userId)
}

func (s *KcpServer) Room(roomId int64) *bucket.Room {
	for _, b := range s.Buckets {
		if room := b.Room(roomId); room != nil {
			return room
		}
	}
	return nil
}

func (s *KcpServer) Broadcast(ctx context.Context, msg *message.Msg) error {
	for _, b := range s.Buckets {
		for _, room := range b.GetRooms() {
			room.Push(ctx, msg)
		}
	}
	return nil
}

func (s *KcpServer) PProf(ctx context.Context) (*pprof.BucketInfo, error) {
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

// Serve 阻塞运行，循环 Accept 并启动每个连接的 readLoop/writeLoop。
func (s *KcpServer) Serve(ctx context.Context) error {
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
			if errors.Is(err, net.ErrClosed) || (err != nil && err.Error() == "use of closed network connection") {
				return nil
			}
			select {
			case <-ctx.Done():
				return nil
			default:
				continue
			}
		}

		ch := chIface.(*KcpChannelServer)
		go ch.Serve(ctx)
	}
}
