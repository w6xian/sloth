package tlv

import (
	"encoding/binary"
	"encoding/json"
	"errors"

	"github.com/w6xian/sloth/internal/utils"
)

var (
	ErrInvalidValueLength = errors.New("value length is too long")
	ErrInvalidCrc         = errors.New("invalid crc")
	ErrInvalidFloat64     = errors.New("invalid float64")
	ErrInvalidFloat64Type = errors.New("invalid float64 type")
	ErrInvalidInt64       = errors.New("invalid int64")
	ErrInvalidInt64Type   = errors.New("invalid int64 type")
	ErrInvalidUint64      = errors.New("invalid uint64")
	ErrInvalidUint64Type  = errors.New("invalid uint64 type")
	ErrInvalidStructType  = errors.New("invalid type 0x00< tax >0x40(64)")
	ErrInvalidBinType     = errors.New("invalid binary type")
	ErrInvalidLengthSize  = errors.New("invalid length size,1-4")
)

// tag/type 只支持 0x01-0x40（1-63）
const (
	TLV_TYPE_FRAME   = 0x00
	TLV_TYPE_STRING  = 0x01
	TLV_TYPE_JSON    = 0x02
	TLV_TYPE_BINARY  = 0x03
	TLV_TYPE_INT8    = 0x04
	TLV_TYPE_INT16   = 0x05
	TLV_TYPE_INT32   = 0x06
	TLV_TYPE_INT64   = 0x07
	TLV_TYPE_UINT8   = 0x08
	TLV_TYPE_UINT16  = 0x09
	TLV_TYPE_UINT32  = 0x0A
	TLV_TYPE_UINT64  = 0x0B
	TLV_TYPE_FLOAT32 = 0x0C
	TLV_TYPE_FLOAT64 = 0x0D
	TLV_TYPE_BYTE    = 0x08
	TLV_TYPE_NIL     = 0x0F
)

const TLVX_HEADER_SIZE = 5
const TLVX_HEADER_MIN_SIZE = 2

type TlV struct {
	T byte   // tag type
	L uint16 // value length
	V []byte // value
}

func tlv_new_from_frame(b []byte, opts *Option) (*TlV, error) {
	t := &TlV{
		T: 0,
		L: 0,
		V: []byte{},
	}
	tag, data, err := tlv_decode_opt(b, opts)
	if err != nil {
		return nil, err
	}
	t.T = tag
	t.L = uint16(len(data))
	t.V = data
	return t, nil
}
func (t *TlV) Tag() byte {
	return t.T
}
func (t *TlV) Type() byte {
	return t.T
}
func (t *TlV) Value() []byte {
	return t.V
}
func (t *TlV) String() string {
	return string(t.V)
}
func (t *TlV) Json(v any) error {
	return json.Unmarshal(t.V, v)
}

func IsTLVFrame(b []byte) bool {
	option := NewOption()
	_, _, err := tlv_decode_opt(b, option)
	return err == nil
}

func get_header_size(lLen byte, checkCRC bool) byte {
	c := byte(0x02)
	if !checkCRC {
		c = 0
	}
	return lLen + 1 + c
}

func Encode(tag byte, data []byte, opts ...FrameOption) ([]byte, error) {
	option := NewOption(opts...)
	return tlv_encode_opt(tag, data, option)

}

func get_max_value_length(lengthSize byte) int {
	if lengthSize == 1 {
		return 0xFF
	}
	if lengthSize == 2 {
		return 0xFFFF
	}
	if lengthSize == 3 {
		return 0xFFFFFF
	}
	return 0xFFFFFFFF
}

func tlv_encode_opt(tag byte, data []byte, opt *Option) ([]byte, error) {
	l := len(data)
	if l == 0x00 {
		return []byte{tag, 0}, nil
	}
	if tag > 0x40 {
		return nil, ErrInvalidStructType
	}
	// 最大支持 0xFFFF 字节
	if l > get_max_value_length(opt.MaxLength) {
		return nil, ErrInvalidValueLength
	}
	// 根据长度大小判断是否需要扩展tag
	if l > get_max_value_length(opt.MinLength) {
		tag |= 0x80
		opt.LengthSize = opt.MaxLength
	} else {
		opt.LengthSize = opt.MinLength
	}

	headerSize := get_header_size(opt.LengthSize, opt.CheckCRC)
	buf := make([]byte, int(headerSize)+int(l))
	buf[0] = tag
	if opt.LengthSize > opt.MinLength {
		buf[0] |= 0x80
	}
	if opt.CheckCRC {
		buf[0] |= 0x40
	}

	lb := make([]byte, 4)
	binary.BigEndian.PutUint32(lb, uint32(l))
	switch opt.LengthSize {
	case 1:
		buf[1] = lb[3]
	case 2:
		buf[1] = lb[2]
		buf[2] = lb[3]
	case 3:
		buf[1] = lb[1]
		buf[2] = lb[2]
		buf[3] = lb[3]
	case 4:
		buf[1] = lb[0]
		buf[2] = lb[1]
		buf[3] = lb[2]
		buf[4] = lb[3]
	default:
		return nil, ErrInvalidLengthSize
	}
	//
	if opt.CheckCRC {
		crc := utils.GetCrC(data)
		// 写入crc
		buf[headerSize-2] = crc[0]
		buf[headerSize-1] = crc[1]
	}
	// 写入数据
	copy(buf[headerSize:], data)
	return buf, nil
}

func Decode(b []byte, opts ...FrameOption) (byte, []byte, error) {
	option := NewOption(opts...)
	return tlv_decode_opt(b, option)
}

func tlv_decode_opt(b []byte, opt *Option) (byte, []byte, error) {
	if len(b) < TLVX_HEADER_MIN_SIZE {
		return 0, nil, ErrInvalidValueLength
	}
	tag := b[0]
	// 64 32 24 16 | 8 4 2 1
	lengthSize := opt.MinLength
	if lengthSize <= 0 {
		return tag, []byte{}, nil
	}
	checkCRC := false
	if tag&0x80 > 0 {
		lengthSize = opt.MaxLength
	}
	if tag&0x40 > 0 {
		checkCRC = true
	}
	// 需要去掉高2位（64 32）有效tag只有6位 1-63
	tag &= 0x3F
	headerSize := get_header_size(lengthSize, checkCRC)
	l := 0
	switch lengthSize {
	case 1:
		l = int(b[1])
	case 2:
		pos := int(max(0, min(1+lengthSize, byte(len(b)))))
		u16 := []byte{0, 0}
		copy(u16, b[1:pos])
		l = int(binary.BigEndian.Uint16(u16))
	case 3, 4:
		minLen := int(max(1, min(1+lengthSize, byte(len(b)))))
		u32 := []byte{0, 0, 0, 0}
		copy(u32, b[1:minLen])
		l = int(binary.BigEndian.Uint32(u32))
	default:
		return 0, nil, ErrInvalidLengthSize
	}
	if len(b) < int(int(headerSize)+l) {
		return 0, nil, ErrInvalidValueLength
	}
	dataBuf := b[headerSize : int(headerSize)+l] // b[6:6+l]
	if checkCRC {
		crc := b[headerSize-2 : headerSize]
		if !utils.CheckCRC(dataBuf, crc) {
			return 0, nil, ErrInvalidCrc
		}
	}
	return tag, dataBuf, nil
}
