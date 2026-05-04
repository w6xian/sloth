package tcpsock

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/w6xian/sloth/v2/internal/logger"
	"github.com/w6xian/sloth/v2/message"
	"github.com/w6xian/sloth/v2/nrpc"
)

// TcpConn 实现 nrpc.RawConn 接口，封装 TCP 连接。
type TcpConn struct {
	conn net.Conn
}

func NewTcpConn(conn net.Conn) *TcpConn {
	return &TcpConn{conn: conn}
}

// ReadFrame 读取 TLV 帧
func (c *TcpConn) ReadFrame(ctx context.Context) (byte, []byte, error) {
	return ReadFrame(c.conn, 0)
}

// WriteFrame 写入 TLV 帧
func (c *TcpConn) WriteFrame(ctx context.Context, frameType byte, payload []byte, timeout time.Duration) error {
	return WriteFrame(c.conn, frameType, payload, timeout)
}

// Close 关闭连接
func (c *TcpConn) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// SetDeadline 设置截止时间
func (c *TcpConn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

// SetReadDeadline 设置读截止时间
func (c *TcpConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline 设置写截止时间
func (c *TcpConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

// ReplyMessage 服务端返回的 RPC 回复（JSON 格式，与 message.JsonBackObject 对齐）。
type ReplyMessage struct {
	Id    string `json:"id"`
	Data  []byte `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// TcpClient 实现 nrpc.ICall 接口，基于 TcpConn。
type TcpClient struct {
	*TcpConn                       // 嵌入 TcpConn，实现 nrpc.RawConn
	Connect nrpc.ICallRpc

	// 等待中的 RPC 调用
	pendingCalls sync.Map
	callIdMu     sync.Mutex
	callId       int64

	closeOnce sync.Once
	closeChan chan struct{}

	// 认证信息
	authMu   sync.RWMutex
	authInfo *nrpc.AuthInfo
}

// NewTcpClient 创建 TCP 客户端
func NewTcpClient(connect nrpc.ICallRpc) *TcpClient {
	return &TcpClient{
		Connect:   connect,
		closeChan: make(chan struct{}),
	}
}

// Dial 连接远端 TCP 服务端，返回 nrpc.RawConn
func (c *TcpClient) Dial(ctx context.Context, addr string) (nrpc.RawConn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("tcp dial %s err: %w", addr, err)
	}
	c.TcpConn = NewTcpConn(conn)
	go c.readLoop()
	return c, nil
}

// ── nrpc.ICall 接口实现 ───────────────────────────────────────────

// Call 发起 RPC 调用
func (c *TcpClient) Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	// 生成唯一 call id
	c.callIdMu.Lock()
	c.callId++
	id := fmt.Sprintf("tcp-%d", c.callId)
	c.callIdMu.Unlock()

	// 构造 RpcCaller
	callMsg := &nrpc.RpcCaller{
		Id:       id,
		Protocol: 1, // JSON 协议
		Action:   1, // ACTION_CALL
		Header:   header,
		Method:   mtd,
		Args:     args,
	}

	// 注册回复 channel
	replyChan := make(chan *nrpc.ReplyMessage, 1)
	c.pendingCalls.Store(id, replyChan)
	defer c.pendingCalls.Delete(id)

	// 序列化并发送
	data, err := json.Marshal(callMsg)
	if err != nil {
		return nil, fmt.Errorf("marshal call err: %w", err)
	}
	if err := c.WriteFrame(ctx, nrpc.FrameTypeCall, data, 10*time.Second); err != nil {
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
func (c *TcpClient) Push(ctx context.Context, msg *message.Msg) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal push err: %w", err)
	}
	return c.WriteFrame(ctx, nrpc.FrameTypePush, data, 10*time.Second)
}

// GetAuthInfo 获取认证信息
func (c *TcpClient) GetAuthInfo() (*nrpc.AuthInfo, error) {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	return c.authInfo, nil
}

// SetAuthInfo 设置认证信息
func (c *TcpClient) SetAuthInfo(auth *nrpc.AuthInfo) error {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	c.authInfo = auth
	return nil
}

// Close 关闭客户端
func (c *TcpClient) Close() error {
	c.closeOnce.Do(func() {
		if c.closeChan != nil {
			close(c.closeChan)
		}
	})
	return c.TcpConn.Close()
}

// ── 内部方法 ─────────────────────────────────────────────────────

func (c *TcpClient) log(level logger.LogLevel, line string, args ...any) {
	if c.Connect == nil {
		fmt.Println("TcpClient Connect is nil")
		return
	}
	c.Connect.Log(level, "[TcpClient]"+line, args...)
}

// readLoop 读取服务端帧
func (c *TcpClient) readLoop() {
	defer func() {
		if r := recover(); r != nil {
			c.log(logger.Error, "readLoop panic: %v", r)
		}
		c.Close()
	}()

	for {
		select {
		case <-c.closeChan:
			return
		default:
		}

		frameType, payload, err := c.ReadFrame(context.Background())
		if err != nil {
			c.log(logger.Error, "readLoop ReadFrame err: %v", err)
			return
		}

		switch frameType {
		case nrpc.FrameTypeReply, nrpc.FrameTypeError:
			c.handleReply(payload)
		case nrpc.FrameTypePush:
			c.log(logger.Debug, "received push frame, payload len=%d", len(payload))
		case nrpc.FrameTypePong:
			// 心跳回复，忽略
		default:
			c.log(logger.Warning, "readLoop unknown frame type: %d", frameType)
		}
	}
}

// handleReply 处理回复
func (c *TcpClient) handleReply(payload []byte) {
	var reply ReplyMessage
	if err := json.Unmarshal(payload, &reply); err != nil {
		c.log(logger.Error, "handleReply unmarshal err: %v", err)
		return
	}

	v, ok := c.pendingCalls.Load(reply.Id)
	if !ok {
		return
	}
	ch := v.(chan *nrpc.ReplyMessage)
	select {
	case ch <- &nrpc.ReplyMessage{Id: reply.Id, Data: reply.Data, Error: reply.Error}:
	default:
	}
}
