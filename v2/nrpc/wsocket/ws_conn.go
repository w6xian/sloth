package wsocket

import (
	"context"
	"time"

	"github.com/w6xian/sloth/v2/nrpc"

	"github.com/gorilla/websocket"
)

// WsConn 实现 nrpc.RawConn 接口，封装 WebSocket 连接。
type WsConn struct {
	conn *websocket.Conn
}

func NewWsConn(conn *websocket.Conn) *WsConn {
	return &WsConn{conn: conn}
}

// ReadFrame 读取 WebSocket 帧
func (c *WsConn) ReadFrame(ctx context.Context) (byte, []byte, error) {
	// WebSocket 协议：使用 websocket.TextMessage 或 BinaryMessage
	// 这里简化处理，实际需要根据应用层协议解析 frameType
	_, msg, err := c.conn.ReadMessage()
	if err != nil {
		return 0, nil, err
	}
	return nrpc.FrameTypeCall, msg, nil
}

// WriteFrame 写入 WebSocket 帧
func (c *WsConn) WriteFrame(ctx context.Context, frameType byte, payload []byte, timeout time.Duration) error {
	c.conn.SetWriteDeadline(time.Now().Add(timeout))
	msgType := websocket.TextMessage
	if frameType == nrpc.FrameTypePush {
		msgType = websocket.BinaryMessage
	}
	return c.conn.WriteMessage(msgType, payload)
}

// Close 关闭连接
func (c *WsConn) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// SetReadDeadline 设置读截止时间
func (c *WsConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline 设置写截止时间
func (c *WsConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}
