package kcpsock

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/w6xian/sloth/v2/internal/logger"
	"github.com/w6xian/sloth/v2/message"
	"github.com/w6xian/sloth/v2/nrpc"
	"github.com/w6xian/sloth/v2/types/auth"
	"github.com/w6xian/sloth/v2/types/trpc"
	"github.com/xtaci/kcp-go/v5"
	"golang.org/x/crypto/pbkdf2"
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

// KcpClient 实现 nrpc.ICall 接口，基于 TcpConn。
type KcpClient struct {
	*TcpConn // 嵌入 TcpConn，实现 nrpc.RawConn
	Connect  trpc.ICallRpc

	// 等待中的 RPC 调用
	pendingCalls sync.Map
	callIdMu     sync.Mutex
	callId       int64

	rpcCallerPool sync.Pool

	closeOnce sync.Once
	closeChan chan struct{}

	// 认证信息
	authMu   sync.RWMutex
	authInfo *auth.AuthInfo
}

// NewKcpClient 创建 KCP 客户端
func NewKcpClient(connect trpc.ICallRpc) *KcpClient {
	c := &KcpClient{
		Connect:   connect,
		closeChan: make(chan struct{}),
	}
	c.rpcCallerPool = sync.Pool{
		New: func() any {
			return &trpc.RpcCaller{}
		},
	}
	return c
}

func (c *KcpClient) getRpcCaller() *trpc.RpcCaller {
	req := c.rpcCallerPool.Get()
	if req == nil {
		return &trpc.RpcCaller{}
	}
	return req.(*trpc.RpcCaller)
}

func (c *KcpClient) putRpcCaller(req *trpc.RpcCaller) {
	if req == nil {
		return
	}
	req.Id = ""
	req.Protocol = 0
	req.Action = 0
	req.Header = nil
	req.Method = ""
	req.Data = nil
	req.Args = nil
	req.Error = ""
	req.Channel = nil
	c.rpcCallerPool.Put(req)
}

// Dial 连接远端 KCP 服务端，返回 nrpc.RawConn
func (c *KcpClient) Dial(ctx context.Context, addr string) (nrpc.RawConn, error) {

	// TODO: 支持 KCP 连接参数
	key := pbkdf2.Key([]byte("demo pass"), []byte("demo salt"), 1024, 32, sha1.New)
	block, err := kcp.NewAESBlockCrypt(key)
	if err != nil {
		return nil, fmt.Errorf("new aes block crypt err: %w", err)
	}
	conn, err := kcp.DialWithOptions(addr, block, 10, 3)
	if err != nil {
		return nil, fmt.Errorf("kcp dial %s err: %w", addr, err)
	}
	c.TcpConn = NewTcpConn(conn)
	go c.readLoop()
	return c, nil
}

// ── nrpc.ICall 接口实现 ───────────────────────────────────────────

// Call 发起 RPC 调用
func (c *KcpClient) Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	// 生成唯一 call id
	c.callIdMu.Lock()
	c.callId++
	id := fmt.Sprintf("kcp-%d", c.callId)
	c.callIdMu.Unlock()

	// 构造 RpcCaller
	callMsg := c.getRpcCaller()
	callMsg.Id = id
	callMsg.Protocol = 0 // TLV 协议（返回值/参数按框架默认编码，避免客户端 tlv decoder 失败）
	callMsg.Action = 1   // ACTION_CALL
	callMsg.Header = header
	callMsg.Method = mtd
	callMsg.Data = nil
	callMsg.Args = nil
	if len(args) > 0 {
		callMsg.Data = args[0]
		if len(args) > 1 {
			callMsg.Args = args[1:]
		}
	}

	// 注册回复 channel
	replyChan := make(chan *nrpc.ReplyMessage, 1)
	c.pendingCalls.Store(id, replyChan)
	defer c.pendingCalls.Delete(id)

	// 序列化并发送
	data, err := json.Marshal(callMsg)
	c.putRpcCaller(callMsg)
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
func (c *KcpClient) Push(ctx context.Context, msg *message.Msg) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal push err: %w", err)
	}
	return c.WriteFrame(ctx, nrpc.FrameTypePush, data, 10*time.Second)
}

// GetAuthInfo 获取认证信息
func (c *KcpClient) GetAuthInfo() (*auth.AuthInfo, error) {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	return c.authInfo, nil
}

// SetAuthInfo 设置认证信息
func (c *KcpClient) SetAuthInfo(auth *auth.AuthInfo) error {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	c.authInfo = auth
	return nil
}

// Close 关闭客户端
func (c *KcpClient) Close() error {
	c.closeOnce.Do(func() {
		if c.closeChan != nil {
			close(c.closeChan)
		}
	})
	return c.TcpConn.Close()
}

// ── 内部方法 ─────────────────────────────────────────────────────

func (c *KcpClient) log(level logger.LogLevel, line string, args ...any) {
	if c.Connect == nil {
		fmt.Println("KcpClient Connect is nil")
		return
	}
	c.Connect.Log(level, "[KcpClient]"+line, args...)
}

// readLoop 读取服务端帧
func (c *KcpClient) readLoop() {
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
func (c *KcpClient) handleReply(payload []byte) {
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
