package sloth

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/w6xian/sloth/v3/decoder"
	"github.com/w6xian/sloth/v3/decoder/ag"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/types/auth"
	"github.com/w6xian/sloth/v3/types/trpc"
)

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
		return errors.New("server not found")
	}
	c.RoomId = auth.RoomId
	c.UserId = auth.UserId
	return listen.SetAuthInfo(auth)
}

// GetAuthInfo 获取认证信息
func (c *ServerRpc) GetAuthInfo() (*auth.AuthInfo, error) {
	listen := c.getListen()
	if listen == nil {
		return nil, errors.New("server not found")
	}
	return listen.GetAuthInfo()
}

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
		return nil, errors.New("server not found")
	}
	args, err := decoder.EncodeArgs(arg, c.Encoder)
	if err != nil {
		return nil, err
	}
	// 调用服务器方法,这里对应的是 channel_client.go 中的Call方法
	resp, err := listen.Call(ctx, c.Header.Clone(), mtd, args...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServerRpc) CallWithHeader(ctx context.Context, header message.Header, mtd string, arg ...any) ([]byte, error) {
	listen := c.getListen()
	if listen == nil {
		return nil, errors.New("server not found")
	}
	args, err := decoder.EncodeArgs(arg, c.Encoder)
	if err != nil {
		return nil, err
	}

	usePoolHeader := false
	mergedHeader := header
	if len(c.Header) != 0 {
		usePoolHeader = true
		mergedHeader = message.GetHeader()
		for k, v := range c.Header {
			mergedHeader[k] = v
		}
		for k, v := range header {
			mergedHeader[k] = v
		}
	}
	if usePoolHeader {
		defer message.PutHeader(mergedHeader)
	}

	resp, err := listen.Call(ctx, mergedHeader, mtd, args...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServerRpc) Send(ctx context.Context, data any) error {
	listen := c.getListen()
	if listen == nil {
		return errors.New("server not found")
	}
	// 编码
	attr, err := c.Encoder(data)
	if err != nil {
		return err
	}
	msg := message.NewTextMessage(attr)
	err = listen.Push(ctx, msg)
	if err != nil {
		log.Println("Connect layer Push() error\n", err)
		return err
	}
	return nil
}
