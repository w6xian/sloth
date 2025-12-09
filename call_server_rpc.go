package sloth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/w6xian/sloth/message"
	"github.com/w6xian/sloth/nrpc"
)

var svr_once sync.Once
var ServerObjc *ServerRpc

type ServerRpc struct {
	Listen  nrpc.ICall
	RoomId  int64
	UserId  int64
	Auth    string
	Encoder func(any) ([]byte, error)
	Decoder func([]byte) ([]byte, error)
}

func (c *ServerRpc) SetEncoder(encoder Encoder) {
	c.Encoder = encoder
}

func (c *ServerRpc) SetDecoder(decoder Decoder) {
	c.Decoder = decoder
}

func ConnectServerRpc(opts ...IRpcOption) *ServerRpc {
	svr_once.Do(func() {
		ServerObjc = &ServerRpc{
			Encoder: nrpc.DefaultEncoder,
			Decoder: nrpc.DefaultDecoder,
		}
	})
	for _, opt := range opts {
		opt(ServerObjc)
	}
	return ServerObjc
}

// @call server
func (c *ServerRpc) Call(ctx context.Context, mtd string, arg ...any) ([]byte, error) {
	if c.Listen == nil {
		return nil, errors.New("server not found")
	}
	data := any(nil)
	if len(arg) > 0 {
		data = arg[0]
	}
	if len(arg) > 1 {
		fmt.Println("只会使用第一个参数")
	}
	// 编码
	args, err := c.Encoder(data)
	if err != nil {
		return nil, err
	}

	resp, err := c.Listen.Call(ctx, mtd, args)
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
