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

func EncodeFn(f *FnFrame) ([]byte, error) {
	if f == nil {
		return nil, ErrFnNilFrame
	}
	dataLen := len(f.Data)
	if dataLen > FnMaxDataSize {
		return nil, ErrFnDataTooLarge
	}
	buf := make([]byte, FnHeaderSize+dataLen)
	buf[0] = FnMagic1
	buf[1] = FnMagic2
	buf[2] = f.Action
	binary.BigEndian.PutUint64(buf[3:11], f.ID)
	binary.BigEndian.PutUint32(buf[11:15], uint32(dataLen))
	if dataLen > 0 {
		copy(buf[FnHeaderSize:], f.Data)
	}
	return buf, nil
}
func Encode(action uint8, id uint64, data []byte) ([]byte, error) {
	if IsFn(data) {
		data = Data(data)
	}
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

func DecodeFn(b []byte) (*FnFrame, error) {
	if len(b) < FnHeaderSize {
		return nil, fmt.Errorf("%w: need %d, got %d", ErrFnTooShort, FnHeaderSize, len(b))
	}
	if b[0] != FnMagic1 || b[1] != FnMagic2 {
		return nil, fmt.Errorf("%w: got 0x%02X%02X", ErrFnBadMagic, b[0], b[1])
	}
	length := binary.BigEndian.Uint32(b[11:15])
	if length > FnMaxDataSize {
		return nil, ErrFnDataTooLarge
	}
	totalLen := FnHeaderSize + int(length)
	if len(b) < totalLen {
		return nil, fmt.Errorf("%w: length=%d total need %d, got %d", ErrFnLengthMismatch, length, totalLen, len(b))
	}
	f := &FnFrame{
		Action: b[2],
		ID:     binary.BigEndian.Uint64(b[3:11]),
	}
	if length > 0 {
		f.Data = make([]byte, length)
		copy(f.Data, b[FnHeaderSize:totalLen])
	}
	return f, nil
}

func Decode(b []byte) (action uint8, id uint64, data []byte, err error) {
	if len(b) < FnHeaderSize {
		return 0, 0, nil, fmt.Errorf("%w: need %d, got %d", ErrFnTooShort, FnHeaderSize, len(b))
	}
	if b[0] != FnMagic1 || b[1] != FnMagic2 {
		return 0, 0, nil, fmt.Errorf("%w: got 0x%02X%02X", ErrFnBadMagic, b[0], b[1])
	}
	length := binary.BigEndian.Uint32(b[11:15])
	if length > FnMaxDataSize {
		return 0, 0, nil, ErrFnDataTooLarge
	}
	totalLen := FnHeaderSize + int(length)
	if len(b) < totalLen {
		return 0, 0, nil, fmt.Errorf("%w: length=%d total need %d, got %d", ErrFnLengthMismatch, length, totalLen, len(b))
	}
	action = b[2]
	id = binary.BigEndian.Uint64(b[3:11])
	if length > 0 {
		data = make([]byte, length)
		copy(data, b[FnHeaderSize:totalLen])
	}
	return action, id, data, nil
}

// call getAction first, then getId, no check again
func Id(b []byte) uint64 {
	if len(b) < 11 {
		return 0
	}
	return binary.BigEndian.Uint64(b[3:11])
}

func Action(b []byte) (uint8, error) {
	if len(b) < FnHeaderSize || b[0] != FnMagic1 || b[1] != FnMagic2 {
		return 0, ErrFnInvalidFrame
	}
	return b[2], nil
}

// call getAction first, then getData,no check again
func Data(b []byte) []byte {
	if !IsFn(b) {
		return b
	}
	return b[FnHeaderSize:]
}

func ValidateFn(b []byte) error {
	if len(b) < FnHeaderSize {
		return fmt.Errorf("%w: need %d, got %d", ErrFnTooShort, FnHeaderSize, len(b))
	}
	if b[0] != FnMagic1 || b[1] != FnMagic2 {
		return fmt.Errorf("%w: got 0x%02X%02X", ErrFnBadMagic, b[0], b[1])
	}
	if b[2] == 0 {
		return ErrFnInvalidAction
	}
	length := binary.BigEndian.Uint32(b[11:15])
	if length > FnMaxDataSize {
		return ErrFnDataTooLarge
	}
	totalLen := FnHeaderSize + int(length)
	if len(b) < totalLen {
		return fmt.Errorf("%w: length=%d total need %d, got %d", ErrFnLengthMismatch, length, totalLen, len(b))
	}
	return nil
}

func ParseFnHeader(b []byte) (action uint8, id uint64, length uint32, err error) {
	if len(b) < FnHeaderSize {
		err = fmt.Errorf("%w: need %d, got %d", ErrFnTooShort, FnHeaderSize, len(b))
		return
	}
	if b[0] != FnMagic1 || b[1] != FnMagic2 {
		err = fmt.Errorf("%w: got 0x%02X%02X", ErrFnBadMagic, b[0], b[1])
		return
	}
	action = b[2]
	id = binary.BigEndian.Uint64(b[3:11])
	length = binary.BigEndian.Uint32(b[11:15])
	return
}

func IsFn(b []byte) bool {
    if len(b) < 2 || b[0] != FnMagic1 || b[1] != FnMagic2 {
        return false
    }
    if len(b) < 3 {
        return true
    }
    if len(b) < FnHeaderSize {
        return true
    }
    length := binary.BigEndian.Uint32(b[11:15])
    if length > FnMaxDataSize {
        return false
    }
    totalLen := FnHeaderSize + int(length)
    if len(b) < totalLen {
        return false
    }
    return true
}
