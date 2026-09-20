package fn

import (
	"encoding/binary"
	"errors"
	"fmt"
)

/**
 * ============================================================
 * FN 协议帧编解码器 (Go 版本)
 * ============================================================
 *
 * 【协议帧结构】
 *
 *  偏移量   长度    字段名      类型             说明
 *  ------  ------  ----------  ---------------  ---------------------------
 *   0       2      Magic       uint8[2]         魔术字 = 0x40 0x46 ("@F")
 *   2       1      Action      uint8            动作类型 (不可为0)
 *   3       8      ID          uint64 BE        消息ID (大端序)
 *  11       4      Length      uint32 BE        Data 字段的字节长度 (大端序)
 *  15       N      Data        uint8[N]         数据载荷，长度 = Length
 *
 *  总头部长度 (HeaderSize) = 2 + 1 + 8 + 4 = 15 字节
 *  最大数据长度 (MaxDataSize) = 1 << 30 = 1,073,741,824 字节 (~1GB)
 *
 * ============================================================
 */

const (
	FnMagic1 byte = 0x40
	FnMagic2 byte = 0x46

	FnHeaderSize = 2 + 1 + 8 + 4

	FnMaxDataSize = 1 << 30
)

var (
	ErrFnTooShort       = errors.New("fn: frame too short")
	ErrFnBadMagic       = errors.New("fn: bad magic header")
	ErrFnLengthMismatch = errors.New("fn: length field mismatch actual data")
	ErrFnDataTooLarge   = errors.New("fn: data size exceeds limit")
	ErrFnNilFrame       = errors.New("fn: nil frame")
	ErrFnInvalidFrame   = errors.New("fn: invalid frame")
	ErrFnInvalidAction  = errors.New("fn: invalid action (must be non-zero)")
)

type FnFrame struct {
	Action uint8
	ID     uint64
	Data   []byte
}

func FnHeader() []byte {
	return []byte{FnMagic1, FnMagic2}
}

// encode 唯一的编码实现：EncodeFn 与 Encode 都走这里。
//
// 此前两个函数各写了一遍组帧逻辑（约 15 行重复），任何协议细节的调整
// 都要改两处，改漏一处就是"编码出来的帧自己解不开"。现在只有这一处。
func encode(action uint8, id uint64, data []byte) ([]byte, error) {
	dataLen := len(data)
	if dataLen > FnMaxDataSize {
		return nil, ErrFnDataTooLarge
	}
	buf := make([]byte, FnHeaderSize+dataLen)
	buf[0] = FnMagic1
	buf[1] = FnMagic2
	buf[2] = action
	binary.BigEndian.PutUint64(buf[3:11], id)
	binary.BigEndian.PutUint32(buf[11:15], uint32(dataLen))
	if dataLen > 0 {
		copy(buf[FnHeaderSize:], data)
	}
	return buf, nil
}

func EncodeFn(f *FnFrame) ([]byte, error) {
	if f == nil {
		return nil, ErrFnNilFrame
	}
	return encode(f.Action, f.ID, f.Data)
}

func Encode(action uint8, id uint64, data []byte) ([]byte, error) {
	// 与 EncodeFn 的唯一差异：入参本身已是 FN 帧时只取载荷，避免套娃编码。
	if IsFn(data) {
		data = Data(data)
	}
	return encode(action, id, data)
}

// fnHeader 帧头字段。
type fnHeader struct {
	action uint8
	id     uint64
	length uint32
}

// parseHeader 校验并解析帧头，是"帧头解析"的唯一实现。
//
// 此前 ValidateFn / ParseFnHeader / Decode / DecodeFn / IsFn 各自写了一遍
// magic 与长度校验（5 份几乎相同的代码），协议规则一改就得同步 5 处，
// 漏一处就表现为"有的函数认这个帧、有的不认"。现在全部基于它实现。
func parseHeader(b []byte) (h fnHeader, err error) {
	if len(b) < FnHeaderSize {
		return h, fmt.Errorf("%w: need %d, got %d", ErrFnTooShort, FnHeaderSize, len(b))
	}
	if b[0] != FnMagic1 || b[1] != FnMagic2 {
		return h, fmt.Errorf("%w: got 0x%02X%02X", ErrFnBadMagic, b[0], b[1])
	}
	h.action = b[2]
	h.id = binary.BigEndian.Uint64(b[3:11])
	h.length = binary.BigEndian.Uint32(b[11:15])
	return h, nil
}

// checkPayload 校验 length 声明是否可信：上限 + 实际字节数是否够。
func (h fnHeader) checkPayload(b []byte) error {
	if h.length > FnMaxDataSize {
		return ErrFnDataTooLarge
	}
	total := FnHeaderSize + int(h.length)
	if len(b) < total {
		return fmt.Errorf("%w: length=%d total need %d, got %d", ErrFnLengthMismatch, h.length, total, len(b))
	}
	return nil
}

// payload 取出载荷的副本：不与入参共享底层数组，调用方改写入参后仍安全。
func (h fnHeader) payload(b []byte) []byte {
	if h.length == 0 {
		return nil
	}
	data := make([]byte, h.length)
	copy(data, b[FnHeaderSize : FnHeaderSize+int(h.length)])
	return data
}

func DecodeFn(b []byte) (*FnFrame, error) {
	h, err := parseHeader(b)
	if err != nil {
		return nil, err
	}
	if err := h.checkPayload(b); err != nil {
		return nil, err
	}
	return &FnFrame{Action: h.action, ID: h.id, Data: h.payload(b)}, nil
}

func Decode(b []byte) (action uint8, id uint64, data []byte, err error) {
	h, err := parseHeader(b)
	if err != nil {
		return 0, 0, nil, err
	}
	if err := h.checkPayload(b); err != nil {
		return 0, 0, nil, err
	}
	return h.action, h.id, h.payload(b), nil
}

// call getAction first, then getId, no check again
func Id(b []byte) uint64 {
	if len(b) < 11 {
		return 0
	}
	return binary.BigEndian.Uint64(b[3:11])
}

func Action(b []byte) (uint8, error) {
	h, err := parseHeader(b)
	if err != nil {
		return 0, ErrFnInvalidFrame
	}
	return h.action, nil
}

// call getAction first, then getData,no check again
func Data(b []byte) []byte {
	if !IsFn(b) {
		return b
	}
	// IsFn 对"magic 正确但帧头没收全"也返回 true，此时切片会越界 panic。
	if len(b) < FnHeaderSize {
		return nil
	}
	return b[FnHeaderSize:]
}

func ValidateFn(b []byte) error {
	h, err := parseHeader(b)
	if err != nil {
		return err
	}
	if h.action == 0 {
		return ErrFnInvalidAction
	}
	return h.checkPayload(b)
}

// ParseFnHeader 只解析帧头字段，不校验载荷长度（调用方可能只关心头）。
func ParseFnHeader(b []byte) (action uint8, id uint64, length uint32, err error) {
	h, err := parseHeader(b)
	if err != nil {
		return 0, 0, 0, err
	}
	return h.action, h.id, h.length, nil
}

// HasMagic 只判断帧头 magic，不看载荷长度声明。
//
// 用于"这段字节归哪个 codec 处理"的判定（codec.Detect）：magic 对就该由它处理，
// 格式对不对是 Decode 的事——用 IsFn 做判定的话，帧会被完整解析两次
// （判定一次、解码一次），而把畸形帧误判成"不是 FN 帧"还会让它被当成
// 业务数据静默放行，反而不如报错。
func HasMagic(b []byte) bool {
	return len(b) >= 2 && b[0] == FnMagic1 && b[1] == FnMagic2
}

// IsFn 判断一段字节是否是（或看起来是）FN 帧。
//
// 语义要点：magic 正确但帧头尚未收全时也返回 true —— 按 FN 帧处理，
// "帧不完整"这类具体错误留给 Decode 报告。只有 magic 不对，或长度声明
// 明显不可信（超过上限 / 超出实际字节数）时才判为不是 FN 帧。
func IsFn(b []byte) bool {
	if len(b) < 2 || b[0] != FnMagic1 || b[1] != FnMagic2 {
		return false
	}
	if len(b) < FnHeaderSize {
		return true
	}
	h, err := parseHeader(b)
	if err != nil {
		return false
	}
	return h.checkPayload(b) == nil
}
