package nrpc

import (
	"context"
	"time"
)

// RawConn 底层字节流连接抽象，屏蔽协议差异。
// 所有协议（TCP/WebSocket/QUIC/KCP/UDP）只需实现此接口，
// 即可接入中间件链和统一调用逻辑。
type RawConn interface {
	// ReadFrame 读取一个完整帧，返回 (frameType, payload, error)
	ReadFrame(ctx context.Context) (frameType byte, payload []byte, err error)

	// WriteFrame 写入一个帧
	WriteFrame(ctx context.Context, frameType byte, payload []byte, timeout time.Duration) error

	// Close 关闭连接
	Close() error
}

// ReadWriteCloser 组合了 RawConn 和基本 IO 操作
type ReadWriteCloser interface {
	RawConn
	// SetDeadline 设置读写截止时间
	SetDeadline(t time.Time) error
	// SetReadDeadline 设置读截止时间
	SetReadDeadline(t time.Time) error
	// SetWriteDeadline 设置写截止时间
	SetWriteDeadline(t time.Time) error
}
