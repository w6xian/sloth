package sloth

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/w6xian/sloth/message"
	"github.com/w6xian/sloth/nrpc"
)

var svr_once sync.Once
var ServerObjc *ServerRpc

type ServerRpc struct {
	Listen nrpc.ICall
	RoomId int64
	UserId int64
}

func NewServerRpc() *ServerRpc {
	svr_once.Do(func() {
		ServerObjc = &ServerRpc{}
	})
	return ServerObjc
}

func (c *ServerRpc) Call(ctx context.Context, mtd string, data any) ([]byte, error) {
	if c.Listen == nil {
		return nil, errors.New("server not found")
	}
	resp, err := c.Listen.Call(ctx, mtd, data)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
func (c *ServerRpc) Send(ctx context.Context, data []byte) error {
	if c.Listen == nil {
		return errors.New("server not found")
	}
	msg := message.NewTextMessage(data)
	err := c.Listen.Push(ctx, msg)
	if err != nil {
		log.Println("Connect layer Push() error\n", err)
		return err
	}
	return nil
}
