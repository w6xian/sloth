package kcpsock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/w6xian/sloth/v2/bucket"
	"github.com/w6xian/sloth/v2/internal/logger"
	"github.com/w6xian/sloth/v2/message"
	"github.com/w6xian/sloth/v2/nrpc"
	"github.com/w6xian/sloth/v2/nrpc/middleware"
	"github.com/w6xian/sloth/v2/types/auth"
	"github.com/w6xian/sloth/v2/types/trpc"
)

// TcpChannelServer 实现 nrpc.AuthChannel 接口，
// 是服务端对单个 KCP 客户端连接的抽象。
type KcpChannelServer struct {
	Lock sync.Mutex

	// 所属 server 引用（用于获取中间件链、调用 CallFunc）
	server *KcpServer

	// bucket 链表指针
	_next bucket.IChannel
	_prev bucket.IChannel
	_room *bucket.Room

	_userId int64
	_sign   string

	// KCP 连接
	conn net.Conn

	// 服务端 Push 消息队列（writeLoop 消费）
	broadcast chan *message.Msg

	// 保留：供 Call（服务端调客户端）使用
	rpcCaller chan *message.JsonCallObject
	rpcBacker chan *message.JsonBackObject

	Connect trpc.ICallRpc

	rpc_io int

	// 关闭信号
	closeChan chan struct{}
}

func NewKcpChannelServer(connect trpc.ICallRpc, conn net.Conn, server *KcpServer) *KcpChannelServer {
	ch := &KcpChannelServer{
		server:    server,
		conn:      conn,
		Connect:   connect,
		broadcast: make(chan *message.Msg, 64),
		rpcCaller: make(chan *message.JsonCallObject, 10),
		rpcBacker: make(chan *message.JsonBackObject, 10),
		closeChan: make(chan struct{}),
		rpc_io:    0,
	}
	ch._next = nil
	ch._prev = nil
	return ch
}

// ── bucket.IChannel 接口实现 ──────────────────────────────────────────────

func (ch *KcpChannelServer) Next(n ...bucket.IChannel) bucket.IChannel {
	if len(n) > 0 {
		ch._next = n[0]
	}
	return ch._next
}

func (ch *KcpChannelServer) Prev(p ...bucket.IChannel) bucket.IChannel {
	if len(p) > 0 {
		ch._prev = p[0]
	}
	return ch._prev
}

func (ch *KcpChannelServer) Room(r ...*bucket.Room) *bucket.Room {
	if len(r) > 0 {
		ch._room = r[0]
	}
	return ch._room
}

func (ch *KcpChannelServer) UserId(u ...int64) int64 {
	if len(u) > 0 {
		ch._userId = u[0]
	}
	return ch._userId
}

func (ch *KcpChannelServer) Token(t ...string) string {
	if len(t) > 0 {
		ch._sign = t[0]
	}
	return ch._sign
}

// Push 向客户端推送一条消息（服务端 → 客户端）。
func (ch *KcpChannelServer) Push(ctx context.Context, msg *message.Msg) (err error) {
	select {
	case ch.broadcast <- msg:
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return nil
}

// ReplySuccess 向客户端发送 RPC 调用成功回复。
func (ch *KcpChannelServer) ReplySuccess(id string, data []byte) error {
	select {
	case ch.rpcBacker <- message.NewWsJsonBackSuccess(id, data):
	default:
	}
	return nil
}

// ReplyError 向客户端发送 RPC 调用失败回复。
func (ch *KcpChannelServer) ReplyError(id string, errBytes []byte) error {
	select {
	case ch.rpcBacker <- message.NewWsJsonBackError(id, errBytes):
	default:
	}
	return nil
}

// Call 服务端主动调用客户端方法（反向 RPC），暂未实现。
func (ch *KcpChannelServer) Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {
	return nil, errors.New("KcpChannelServer.Call not implemented")
}

// ── AuthChannel 接口实现 ──────────────────────────────────────────────────

func (ch *KcpChannelServer) GetAuthInfo() (*auth.AuthInfo, error) {
	if ch._userId == 0 {
		return nil, errors.New("user id is 0")
	}
	if ch._room == nil {
		return nil, errors.New("room is nil")
	}
	if ch._sign == "" {
		return nil, errors.New("token is empty")
	}
	return &auth.AuthInfo{
		UserId: ch._userId,
		RoomId: ch._room.Id,
		Token:  ch._sign,
	}, nil
}

func (ch *KcpChannelServer) SetAuthInfo(auth *auth.AuthInfo) error {
	return errors.New("server does not support SetAuthInfo")
}

// Close 关闭 KCP 连接并释放资源。
func (ch *KcpChannelServer) Close() error {
	ch.Lock.Lock()
	defer ch.Lock.Unlock()

	select {
	case <-ch.closeChan:
	default:
		close(ch.closeChan)
	}
	if ch.conn != nil {
		ch.conn.Close()
		ch.conn = nil
	}
	ch._userId = 0
	return nil
}

// ── 内部方法 ─────────────────────────────────────────────────────────────

func (ch *KcpChannelServer) log(level logger.LogLevel, line string, args ...any) {
	if ch.Connect == nil {
		fmt.Println("KcpChannelServer Connect is nil")
		return
	}
	ch.Connect.Log(level, "[KcpChannel]"+line, args...)
}

// Serve 启动 readLoop + writeLoop，阻塞直到连接关闭。
func (ch *KcpChannelServer) Serve(ctx context.Context) {
	go ch.readLoop(ctx)
	go ch.writeLoop(ctx)
	<-ch.closeChan
}

// readLoop 读取客户端发来的 TLV 帧并处理。
func (ch *KcpChannelServer) readLoop(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			ch.log(logger.Error, "readLoop panic: %v", r)
		}
		ch.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ch.closeChan:
			return
		default:
		}

		frameType, payload, err := ReadFrame(ch.conn, 0)
		if err != nil {
			ch.log(logger.Error, "readLoop ReadFrame err: %v", err)
			return
		}

		switch frameType {
		case nrpc.FrameTypeCall:
			ch.handleCall(payload)
		case nrpc.FrameTypePing:
			_ = WriteFrame(ch.conn, nrpc.FrameTypePong, nil, 10*time.Second)
		default:
			ch.log(logger.Warning, "readLoop unknown frame type: %d", frameType)
		}
	}
}

// writeLoop 将 Push/Reply 消息写入 KCP 连接。
func (ch *KcpChannelServer) writeLoop(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			ch.log(logger.Error, "writeLoop panic: %v", r)
		}
	}()

	for {
		select {
		case msg, ok := <-ch.broadcast:
			if !ok {
				return
			}
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			_ = WriteFrame(ch.conn, nrpc.FrameTypePush, data, 10*time.Second)

		case msg, ok := <-ch.rpcBacker:
			if !ok {
				return
			}
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			_ = WriteFrame(ch.conn, nrpc.FrameTypeReply, data, 10*time.Second)

		case <-ch.closeChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

// handleCall 处理客户端发来的 RPC Call 帧，通过中间件链执行。
func (ch *KcpChannelServer) handleCall(payload []byte) {
	var caller trpc.RpcCaller
	if err := json.Unmarshal(payload, &caller); err != nil {
		ch.log(logger.Error, "handleCall unmarshal err: %v", err)
		return
	}
	caller.Channel = ch // 绑定当前 channel，使 ReplySuccess/ReplyError 能找到 ch

	// 构建最终处理函数（调用业务层）
	final := func(ctx context.Context, header message.Header, mtd string, argBytes ...[]byte) ([]byte, error) {
		data := caller.Data
		moreArgs := caller.Args
		if data == nil && len(argBytes) > 0 {
			data = argBytes[0]
			if len(argBytes) > 1 {
				moreArgs = argBytes[1:]
			} else {
				moreArgs = nil
			}
		}
		msg := &trpc.RpcCaller{
			Id:       caller.Id,
			Protocol: caller.Protocol,
			Action:   caller.Action,
			Channel:  ch,
			Header:   header,
			Method:   mtd,
			Data:     data,
			Args:     moreArgs,
		}
		rst, err := ch.server.Connect.CallFunc(ctx, ch.server, msg)
		if err != nil {
			msg.Channel.ReplyError(msg.Id, []byte(err.Error()))
			return nil, err
		}
		msg.Channel.ReplySuccess(msg.Id, rst)
		return rst, nil
	}

	// 用中间件链包装
	handler := middleware.Chain(ch.server.middlewares, final)
	handler(context.Background(), message.Header(caller.Header), caller.Method, caller.Args...)
}
