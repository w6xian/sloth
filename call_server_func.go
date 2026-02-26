package sloth

import (
	"context"
	"errors"
	"log"

	"github.com/w6xian/sloth/message"
	"github.com/w6xian/sloth/nrpc"
	"github.com/w6xian/tlv"
)

type ServerRpc struct {
	Listen  nrpc.ICall
	RoomId  int64
	UserId  int64
	Auth    string
	Encoder func(any) ([]byte, error)
	Decoder func([]byte) ([]byte, error)
	Header  message.Header
}

func (c *ServerRpc) SetEncoder(encoder Encoder) {
	c.Encoder = encoder
}

func (c *ServerRpc) SetDecoder(decoder Decoder) {
	c.Decoder = decoder
}
func (c *ServerRpc) SetAuthInfo(auth *nrpc.AuthInfo) error {
	if auth == nil {
		return errors.New("auth is nil")
	}
	if c.Listen == nil {
		return errors.New("server not found")
	}
	c.RoomId = auth.RoomId
	c.UserId = auth.UserId
	return c.Listen.SetAuthInfo(auth)
}

// GetAuthInfo 获取认证信息
func (c *ServerRpc) GetAuthInfo() (*nrpc.AuthInfo, error) {
	if c.Listen == nil {
		return nil, errors.New("server not found")
	}
	return c.Listen.GetAuthInfo()
}

func DefaultClient(opts ...IRpcOption) *ServerRpc {
	svr := &ServerRpc{
		Encoder: tlv.DefaultEncoder,
		Decoder: tlv.DefaultDecoder,
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
	if c.Listen == nil {
		return nil, errors.New("server not found")
	}
	// fmt.Println("Call arg:", arg)
	args := [][]byte{}
	for _, v := range arg {
		// fmt.Println(v)
		b, err := c.Encoder(v)
		if err != nil {
			return nil, err
		}
		args = append(args, b)
	}
	// fmt.Println("Call args:", args)

	resp, err := c.Listen.Call(ctx, c.Header.Clone(), mtd, args...)
	// fmt.Println("Call resp:", resp, err)
	if err != nil {
		return nil, err
	}
	// 解码
	resp, err = c.Decoder(resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServerRpc) CallWithHeader(ctx context.Context, header message.Header, mtd string, arg ...any) ([]byte, error) {
	if c.Listen == nil {
		return nil, errors.New("server not found")
	}
	// fmt.Println("Call arg:", arg)
	args := [][]byte{}
	for _, v := range arg {
		b, err := c.Encoder(v)
		if err != nil {
			return nil, err
		}
		args = append(args, b)
	}
	// fmt.Println("Call args:", args)

	resp, err := c.Listen.Call(ctx, header, mtd, args...)
	// fmt.Println("Call resp:", resp, err)
	if err != nil {
		return nil, err
	}
	// 解码
	resp, err = c.Decoder(resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServerRpc) Send(ctx context.Context, data any) error {
	if c.Listen == nil {
		return errors.New("server not found")
	}
	// 编码
	attr, err := c.Encoder(data)
	if err != nil {
		return err
	}
	msg := message.NewTextMessage(attr)
	err = c.Listen.Push(ctx, msg)
	if err != nil {
		log.Println("Connect layer Push() error\n", err)
		return err
	}
	return nil
}
