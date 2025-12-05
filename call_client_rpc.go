package sloth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/w6xian/sloth/decoder"
	"github.com/w6xian/sloth/group"
	"github.com/w6xian/sloth/message"
)

var once sync.Once
var ClientObjc *ClientRpc

type IServer interface {
	Bucket(userId int64) *group.Bucket
	Room(roomId int64) *group.Room
	Channel(userId int64) group.IChannel
	Broadcast(ctx context.Context, msg *message.Msg) error
}

type ClientRpc struct {
	Serve IServer
}

func NewClientRpc() *ClientRpc {
	once.Do(func() {
		ClientObjc = &ClientRpc{}
	})
	return ClientObjc
}

// @call client
func (c *ClientRpc) Call(ctx context.Context, userId int64, mtd string, data []byte) ([]byte, error) {
	if c.Serve == nil {
		return nil, errors.New("server not found")
	}
	b := c.Serve.Bucket(userId)
	ch := b.Channel(userId)
	if ch == nil {
		return nil, errors.New("channel not found")
	}

	resp, err := ch.Call(ctx, mtd, data)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ClientRpc) Channel(ctx context.Context, userId int64, action int, data string) {
	if c.Serve == nil {
		return
	}
	b := c.Serve.Bucket(userId)
	ch := b.Channel(userId)
	if ch == nil {
		return
	}
	cmd := message.CmdReq{
		Id:     decoder.NextId(),
		Ts:     time.Now().Unix(),
		Action: action,
		Data:   data,
	}
	msg := message.NewTextMessage(cmd.Bytes())
	if err := ch.Push(ctx, msg); err != nil {
		fmt.Println("Connect layer Push() error", err)
	}
}

func (c *ClientRpc) Room(ctx context.Context, roomId int64, action int, data string) {
	if c.Serve == nil {
		return
	}
	room := c.Serve.Room(roomId)
	if room == nil {
		return
	}
	if room.Drop {
		return
	}
	cmd := message.CmdReq{
		Id:     decoder.NextId(),
		Ts:     time.Now().Unix(),
		Action: action,
		Data:   data,
	}
	msg := message.NewTextMessage(cmd.Bytes())
	// fmt.Println("Connect layer Push() roomId", roomId)
	room.Push(ctx, msg)
}

func (c *ClientRpc) Broadcast(ctx context.Context, action int, data string) {
	if c.Serve == nil {
		return
	}
	cmd := message.CmdReq{
		Id:     decoder.NextId(),
		Ts:     time.Now().Unix(),
		Action: action,
		Data:   data,
	}
	msg := message.NewTextMessage(cmd.Bytes())
	if err := c.Serve.Broadcast(ctx, msg); err != nil {
		return
	}
}
