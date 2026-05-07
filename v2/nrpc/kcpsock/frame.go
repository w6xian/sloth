package kcpsock

import (
	"encoding/binary"
	"io"
	"net"
	"time"
)

// WriteFrame 向 conn 写入一个 TLV 帧。
// 格式：[Type:1B][Length:4B][Value:Length B]
func WriteFrame(conn net.Conn, frameType byte, payload []byte, timeout time.Duration) error {
	if timeout > 0 {
		_ = conn.SetWriteDeadline(time.Now().Add(timeout))
	}
	length := uint32(len(payload))
	header := make([]byte, 5)
	header[0] = frameType
	binary.BigEndian.PutUint32(header[1:5], length)
	if _, err := conn.Write(header); err != nil {
		return err
	}
	if length > 0 {
		_, err := conn.Write(payload)
		return err
	}
	return nil
}

// ReadFrame 从 conn 读取一个完整 TLV 帧，处理半包。
// 返回 frameType 和 payload。
func ReadFrame(conn net.Conn, timeout time.Duration) (frameType byte, payload []byte, err error) {
	if timeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
	}
	header := make([]byte, 5)
	if _, err = io.ReadFull(conn, header); err != nil {
		return 0, nil, err
	}
	frameType = header[0]
	length := binary.BigEndian.Uint32(header[1:5])
	if length == 0 {
		return frameType, nil, nil
	}
	payload = make([]byte, length)
	if _, err = io.ReadFull(conn, payload); err != nil {
		return 0, nil, err
	}
	return frameType, payload, nil
}

// WriteFrameNoTimeout 无超时的帧写入（用于已设置 deadline 的场景）。
func WriteFrameNoTimeout(conn net.Conn, frameType byte, payload []byte) error {
	length := uint32(len(payload))
	header := make([]byte, 5)
	header[0] = frameType
	binary.BigEndian.PutUint32(header[1:5], length)
	if _, err := conn.Write(header); err != nil {
		return err
	}
	if length > 0 {
		_, err := conn.Write(payload)
		return err
	}
	return nil
}

// ReadFrameNoTimeout 无超时的帧读取。
func ReadFrameNoTimeout(conn net.Conn) (frameType byte, payload []byte, err error) {
	header := make([]byte, 5)
	if _, err = io.ReadFull(conn, header); err != nil {
		return 0, nil, err
	}
	frameType = header[0]
	length := binary.BigEndian.Uint32(header[1:5])
	if length == 0 {
		return frameType, nil, nil
	}
	payload = make([]byte, length)
	if _, err = io.ReadFull(conn, payload); err != nil {
		return 0, nil, err
	}
	return frameType, payload, nil
}
