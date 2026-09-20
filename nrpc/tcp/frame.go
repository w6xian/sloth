package tcp

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/w6xian/sloth/v3/decoder/fn"
)

var (
	// errBadMagic 帧头 magic 不对：对端发的不是 FN 帧，或字节流已经错位。
	// 错位之后无法重新同步（没有分隔符），只能断开让对端重连。
	errBadMagic = errors.New("tcp frame: bad magic")
	// errFrameTooLarge length 声明超过上限。可能是恶意包或流错位，
	// 必须拒绝——否则会照着这个数字分配内存。
	errFrameTooLarge = errors.New("tcp frame: length exceeds limit")
)

// headBuf 每连接复用的帧头缓冲：读头不再每帧分配一次。
type headBuf [fn.FnHeaderSize]byte

// readFrame 从 TCP 字节流里读出一个完整帧。
//
// TCP 是字节流，没有消息边界，必须自己分帧。这里直接复用 FN 帧自带的
// length 字段（15 字节定长头 + length 字节载荷），而不是另发明一种帧格式：
// 这样 TCP 与 WebSocket 共用同一套 codec 与 dispatch（P2 收敛出来的那个入口），
// **换传输不该换编解码**——这正是传输层抽象要验证的东西。
//
// head 由调用方提供（per-connection 复用），返回的帧是新分配的字节切片。
func readFrame(r *bufio.Reader, head []byte) ([]byte, error) {
	if len(head) < fn.FnHeaderSize {
		return nil, fmt.Errorf("tcp frame: head buffer too small: %d", len(head))
	}
	if _, err := io.ReadFull(r, head[:fn.FnHeaderSize]); err != nil {
		return nil, err
	}
	if !fn.HasMagic(head) {
		return nil, fmt.Errorf("%w: got 0x%02X%02X", errBadMagic, head[0], head[1])
	}
	length := binary.BigEndian.Uint32(head[11:15])
	if uint64(length) > uint64(fn.FnMaxDataSize) {
		return nil, fmt.Errorf("%w: length=%d, limit=%d", errFrameTooLarge, length, fn.FnMaxDataSize)
	}
	frame := make([]byte, fn.FnHeaderSize+int(length))
	copy(frame, head[:fn.FnHeaderSize])
	if length > 0 {
		if _, err := io.ReadFull(r, frame[fn.FnHeaderSize:]); err != nil {
			return nil, err
		}
	}
	return frame, nil
}
