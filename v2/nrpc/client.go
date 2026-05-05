package nrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/w6xian/sloth/v2/message"
	"github.com/w6xian/sloth/v2/types/auth"
	"github.com/w6xian/sloth/v2/types/trpc"
)

// ReplyMessage 服务端回复结构
type ReplyMessage struct {
	Id    string
	Error string
	Data  []byte
}

// unifiedClient 统一的客户端实现，基于 RawConn
type unifiedClient struct {
	conn     RawConn
	connType string   // "tcp" / "ws" / ...
	pending  sync.Map // id -> chan *ReplyMessage
	callId   int64
	callIdMu sync.Mutex
	authInfo *auth.AuthInfo
	authMu   sync.RWMutex
}

// NewUnifiedClient 创建基于 RawConn 的统一客户端
func NewUnifiedClient(conn RawConn, connType string) *unifiedClient {
	c := &unifiedClient{
		conn:     conn,
		connType: connType,
	}
	go c.recvLoop()
	return c
}

// recvLoop 接收服务端消息
func (c *unifiedClient) recvLoop() {
	for {
		ft, payload, err := c.conn.ReadFrame(context.Background())
		if err != nil {
			return
		}

		switch ft {
		case FrameTypeReply, FrameTypeError:
			var reply ReplyMessage
			if err := json.Unmarshal(payload, &reply); err != nil {
				continue
			}
			if ch, ok := c.pending.Load(reply.Id); ok {
				ch.(chan *ReplyMessage) <- &reply
			}
		}
	}
}

// Call 发起 RPC 调用
func (c *unifiedClient) Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {
	c.callIdMu.Lock()
	c.callId++
	id := fmt.Sprintf("%s-%d", c.connType, c.callId)
	c.callIdMu.Unlock()

	// 构造 RpcCaller
	callMsg := &trpc.RpcCaller{
		Id:       id,
		Protocol: 1, // JSON 协议
		Action:   1, // ACTION_CALL
		Header:   header,
		Method:   mtd,
		Args:     args,
	}

	// 注册回复 channel
	replyChan := make(chan *ReplyMessage, 1)
	c.pending.Store(id, replyChan)
	defer c.pending.Delete(id)

	// 序列化并发送
	data, err := json.Marshal(callMsg)
	if err != nil {
		return nil, fmt.Errorf("marshal call err: %w", err)
	}

	if err := c.conn.WriteFrame(ctx, FrameTypeCall, data, 10*time.Second); err != nil {
		return nil, fmt.Errorf("write frame err: %w", err)
	}

	// 等待回复或超时
	select {
	case reply := <-replyChan:
		if reply.Error != "" {
			return nil, fmt.Errorf("%s", reply.Error)
		}
		return reply.Data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Push 向服务端推送消息
func (c *unifiedClient) Push(ctx context.Context, msg *message.Msg) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal push err: %w", err)
	}
	return c.conn.WriteFrame(ctx, FrameTypePush, data, 10*time.Second)
}

// GetAuthInfo 获取认证信息
func (c *unifiedClient) GetAuthInfo() (*auth.AuthInfo, error) {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	return c.authInfo, nil
}

// SetAuthInfo 设置认证信息
func (c *unifiedClient) SetAuthInfo(auth *auth.AuthInfo) error {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	c.authInfo = auth
	return nil
}

// Close 关闭连接
func (c *unifiedClient) Close() error {
	return c.conn.Close()
}
