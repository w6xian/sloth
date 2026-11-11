// Package proto 定义 tunnel 工具的内部帧格式与编解码。
//
// 数据面复用 sloth 的推送通道：relay 通过 ch.Push 把帧推给 agent，
// agent 通过 client.Send 把帧推回 relay，每条 ws 消息恰好承载一个帧，
// 因此 Decode 只需解析一段完整字节，不需要流式拆包。
//
// 帧布局（大端）：
//
//	+-------+----+--------+--------+---------+
//	| magic | op | connID | length | payload |
//	|  1B   | 1B |   4B   |   4B   |    N    |
//	+-------+----+--------+--------+---------+
//
// magic 选 0xC7：刻意避开 sloth FN 帧的 magic（0x40 0x46，即 "@F"），
// 保证数据面帧不会被 sloth 的 FrameRouter 误认成 RPC 帧而路由进 OnFn。
package proto

import (
	"encoding/binary"
	"errors"
)

// 帧操作类型。
const (
	OpOpen  byte = 0x01 // relay → agent：访客连入，payload 为隧道 ID，agent 据此拨本地目标
	OpData  byte = 0x02 // 双向：业务数据块
	OpClose byte = 0x03 // 双向：关闭该 connID 对应的连接
)

const (
	// Magic 帧首字节。
	Magic = 0xC7
	// HeaderSize 帧头长度：magic(1) + op(1) + connID(4) + length(4)。
	HeaderSize = 10
	// ChunkSize 单帧业务数据上限。32KB 远小于 ws 的分片阈值与 FN 帧上限，
	// 保证每帧都是一条独立 ws 消息，不触发 sloth 的 ws DataSlice 分片。
	ChunkSize = 32 << 10
	// MaxPayload 收帧防御上限：超过即视为畸形帧丢弃。
	MaxPayload = 1 << 20
)

// OPEN 帧的 payload 是隧道 ID，长度给个合理防御上限。
const MaxTunnelIDLen = 128

var (
	ErrShortFrame = errors.New("tunnel: frame too short")
	ErrBadMagic   = errors.New("tunnel: bad frame magic")
	ErrBadLength  = errors.New("tunnel: bad frame length")
)

// Encode 编码一帧。
func Encode(op byte, connID uint32, payload []byte) []byte {
	buf := make([]byte, HeaderSize+len(payload))
	buf[0] = Magic
	buf[1] = op
	binary.BigEndian.PutUint32(buf[2:], connID)
	binary.BigEndian.PutUint32(buf[6:], uint32(len(payload)))
	copy(buf[HeaderSize:], payload)
	return buf
}

// Decode 解析一帧完整字节，返回操作类型、连接 ID 与 payload。
// 解析失败（短帧 / magic 不对 / 长度字段与实际不符）一律返回 error，
// 调用方应丢弃整条消息，不做任何部分处理。
func Decode(b []byte) (op byte, connID uint32, payload []byte, err error) {
	if len(b) < HeaderSize {
		return 0, 0, nil, ErrShortFrame
	}
	if b[0] != Magic {
		return 0, 0, nil, ErrBadMagic
	}
	n := binary.BigEndian.Uint32(b[6:])
	if n > MaxPayload || len(b)-HeaderSize < int(n) {
		return 0, 0, nil, ErrBadLength
	}
	return b[1], binary.BigEndian.Uint32(b[2:]), b[HeaderSize : HeaderSize+int(n)], nil
}
