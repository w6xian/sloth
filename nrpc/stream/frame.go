// Package stream 是"字节流传输"的公共底座。
//
// TCP 连接、QUIC 的一条 stream 在 sloth 眼里完全同构：都是 net.Conn，
// 都没有消息边界（靠 FN 帧分帧），都需要"读循环 + 单一写循环 + per-call
// 回包分发"。这套逻辑原本整份写在 nrpc/tcp 里（TcpChannel / runPump /
// readFrame），接入 QUIC 时只有两条路：复制一份，或者抽出来。
//
// 复制一份的代价是分帧与背压这两处最容易写错、也最容易埋数据竞争的逻辑
// 从此有两份实现——改一处漏一处，而且"两组行为不一致"的 bug 极难定位。
// 所以抽成公共包：这是**第三个实现**逼出来的抽象，只有 TCP / WS 时看不出来。
package stream

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
	errBadMagic = errors.New("stream frame: bad magic")
	// errFrameTooLarge length 声明超过上限。可能是恶意包或流错位，
	// 必须拒绝——否则会照着这个数字分配内存。
	errFrameTooLarge = errors.New("stream frame: length exceeds limit")
)

// headBuf 每连接复用的帧头缓冲：读头不再每帧分配一次。
type headBuf [fn.FnHeaderSize]byte

// ReadFrame 从字节流里读出一个完整帧（导出给同包的 pump 与各传输的测试）。
//
// 字节流没有消息边界，必须自己分帧。这里直接复用 FN 帧自带的 length 字段
// （15 字节定长头 + length 字节载荷），而不是另发明一种帧格式：这样所有
// 传输共用同一套 codec 与 dispatch——**换传输不该换编解码**，这正是传输层
// 抽象要验证的东西。
//
// head 由调用方提供（per-connection 复用），返回的帧是新分配的字节切片。
func ReadFrame(r *bufio.Reader, head []byte) ([]byte, error) {
	if len(head) < fn.FnHeaderSize {
		return nil, fmt.Errorf("stream frame: head buffer too small: %d", len(head))
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
