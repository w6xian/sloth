package sloth

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/w6xian/sloth/v4/decoder"
	"github.com/w6xian/sloth/v4/decoder/ag"
	"github.com/w6xian/sloth/v4/internal/errs"
	"github.com/w6xian/sloth/v4/internal/logger"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/types/auth"
	"github.com/w6xian/sloth/v4/types/trpc"
)

// ServerRpc 是「打给服务端」的 RPC 调用端，由客户端程序持有。
//
// 名字里的 Server 指的是**调用目标**，不是持有者——全库按对端命名，自成一套：
//   - 客户端程序：client := DefaultClient()  // *ServerRpc，经 Listen 打到服务端
//   - 服务端程序：server := DefaultServer()  // *ClientRpc，经 userId 打到客户端
//   - 建连接：ClientConn(client) 收 *ServerRpc，ServerConn(server) 收 *ClientRpc
//
// 另见 ClientRpc 的说明。
type ServerRpc struct {
	// mu 保护 Listen 字段：Dial 在连接 goroutine 中写入，
	// Call/SetAuthInfo 等可能在另一 goroutine 读取
	mu      sync.RWMutex
	Listen  trpc.ICall
	RoomId  int64
	UserId  int64
	Auth    string
	Encoder func(any) ([]byte, error)
	Decoder func([]byte) ([]byte, error)
	Header  message.Header
}

// setListen 在 Dial 建立连接时写入底层调用通道
func (c *ServerRpc) setListen(l trpc.ICall) {
	c.mu.Lock()
	c.Listen = l
	c.mu.Unlock()
}

// getListen 返回底层调用通道（调用方持引用在锁外使用）
func (c *ServerRpc) getListen() trpc.ICall {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Listen
}

func (c *ServerRpc) SetEncoder(encoder Encoder) {
	c.Encoder = encoder
}

func (c *ServerRpc) SetDecoder(decoder Decoder) {
	c.Decoder = decoder
}
func (c *ServerRpc) SetAuthInfo(auth *auth.AuthInfo) error {
	if auth == nil {
		return errors.New("auth is nil")
	}
	listen := c.getListen()
	if listen == nil {
		return fmt.Errorf("server not found: %w", errs.ErrNotServing)
	}
	c.RoomId = auth.RoomId
	c.UserId = auth.UserId
	return listen.SetAuthInfo(auth)
}

// GetAuthInfo 获取认证信息
func (c *ServerRpc) GetAuthInfo() (*auth.AuthInfo, error) {
	listen := c.getListen()
	if listen == nil {
		return nil, fmt.Errorf("server not found: %w", errs.ErrNotServing)
	}
	return listen.GetAuthInfo()
}

// DefaultClient 返回客户端程序使用的 RPC 调用端（*ServerRpc，调用目标是服务端）。
// 名字里的 Client 指使用者，ServerRpc 里的 Server 指调用目标，别按"谁持有"理解，
// 详见 ServerRpc 的注释。
func DefaultClient(opts ...IRpcOption) *ServerRpc {
	svr := &ServerRpc{
		Encoder: ag.Encoder,
		Decoder: ag.Decoder,
		Header:  message.Header{},
	}
	for _, opt := range opts {
		opt(svr)
	}

	return svr
}

func LinkServerFunc(opts ...IRpcOption) *ServerRpc {
	return DefaultClient(opts...)
}

// @call server
func (c *ServerRpc) Call(ctx context.Context, mtd string, arg ...any) ([]byte, error) {
	listen := c.getListen()
	if listen == nil {
		return nil, fmt.Errorf("server not found: %w", errs.ErrNotServing)
	}
	args, err := decoder.EncodeArgs(arg, c.Encoder)
	if err != nil {
		return nil, err
	}
	// 带上本次调用的 trace id（键 X-Trace-Id）：服务端 handleCall 会沿用，
	// 两端日志才能按 trace 对账。
	hdr, put := callHeader(ctx, c.Header, nil, "")
	defer put()
	// 调用服务器方法,这里对应的是 channel_client.go 中的Call方法
	resp, err := listen.Call(ctx, hdr, mtd, args...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServerRpc) CallWithHeader(ctx context.Context, header message.Header, mtd string, arg ...any) ([]byte, error) {
	listen := c.getListen()
	if listen == nil {
		return nil, fmt.Errorf("server not found: %w", errs.ErrNotServing)
	}
	args, err := decoder.EncodeArgs(arg, c.Encoder)
	if err != nil {
		return nil, err
	}

	// 与 Call 一致：合并共享头与调用方头，并带上本次调用的 trace。
	// 两份 map 都不能直接写（共享头有并发 race，调用方头会被污染），
	// 池对象的归还也由 put 负责。
	hdr, put := callHeader(ctx, c.Header, header, "")
	defer put()

	resp, err := listen.Call(ctx, hdr, mtd, args...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServerRpc) Send(ctx context.Context, data any) error {
	listen := c.getListen()
	if listen == nil {
		return fmt.Errorf("server not found: %w", errs.ErrNotServing)
	}
	// 编码
	attr, err := c.Encoder(data)
	if err != nil {
		return err
	}
	msg := message.NewTextMessage(attr)
	err = listen.Push(ctx, msg)
	if err != nil {
		logger.Errorw(ctx, "connect layer Push failed", "err", err)
		return err
	}
	return nil
}
