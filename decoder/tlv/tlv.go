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
	// 整数类型
	TLV_TYPE_INT    = 0x01
	TLV_TYPE_INT8   = 0x02
	TLV_TYPE_INT16  = 0x03
	TLV_TYPE_INT32  = 0x04
	TLV_TYPE_INT64  = 0x05
	TLV_TYPE_UINT   = 0x06
	TLV_TYPE_UINT8  = 0x07
	TLV_TYPE_UINT16 = 0x08
	TLV_TYPE_UINT32 = 0x09
	TLV_TYPE_UINT64 = 0x0A
	// 浮点类型
	TLV_TYPE_FLOAT32 = 0x0B
	TLV_TYPE_FLOAT64 = 0x0C
	// 布尔类型
	TLV_TYPE_BOOL = 0x0D
	// 复数类型
	TLV_TYPE_COMPLEX64  = 0x0E
	TLV_TYPE_COMPLEX128 = 0x0F
	TLV_TYPE_UINTPTR    = 0x10
	// 字节和字符类型
	TLV_TYPE_RUNE = 0x11
	TLV_TYPE_BYTE = 0x12
	// 字符串类型
	TLV_TYPE_STRING = 0x13
	// json 类型
	TLV_TYPE_JSON = 0x14
	// 切片类型
	TLV_TYPE_SLICE        = 0x15
	TLV_TYPE_SLICE_BYTE   = 0x16
	TLV_TYPE_SLICE_INT16  = 0x17
	TLV_TYPE_SLICE_UINT16 = 0x18
	TLV_TYPE_SLICE_INT32  = 0x19
	TLV_TYPE_SLICE_UINT32 = 0x1A
	TLV_TYPE_SLICE_INT64  = 0x1B
	TLV_TYPE_SLICE_UINT64 = 0x1C
	TLV_TYPE_SLICE_STRING = 0x1D
	// nil 类型
	TLV_TYPE_NIL = 0x00
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

func get_header_size(l int, opt *Option) byte {
	mi := opt.MinLength
	length := get_max_value_length(opt.MinLength)
	if l > length {
		mi = opt.MaxLength
	}
	c := byte(0x02)
	if !opt.CheckCRC {
		c = 0
	}
	return 1 + mi + c
}

func get_max_value_length(lengthSize byte) int {
	if lengthSize == 1 {
		return 0x000000FF
	}
	if lengthSize == 2 {
		return 0x0000FFFF
	}
	if lengthSize == 3 {
		return 0x00FFFFFF
	}
	return 0xFFFFFFFF
}

func tlv_empty_frame(opt *Option) []byte {
	// return make([]byte, opt.MinLength+1)
	return opt.EmptyFrame
}

func tlv_length_bytes(l int, opt *Option) []byte {
	m := opt.MinLength
	maxValueLength := get_max_value_length(m)
	if l > maxValueLength {
		m = opt.MaxLength
	}
	binary.BigEndian.PutUint32(opt.size, uint32(l))
	return opt.size[4-m : 4]
}

func tlv_encode_opt(tag byte, data []byte, opt *Option) ([]byte, error) {
	l := len(data)
	if l == 0x00 {
		return tlv_empty_frame(opt), nil
	}
	if tag > 0x40 {
		return nil, ErrInvalidStructType
	}

	headerSize := get_header_size(l, opt)
	pos := 1
	buf := opt.pool.Get()
	defer opt.pool.Put(buf)
	buf[0] = tag
	if l > get_max_value_length(opt.MinLength) {
		buf[0] |= 0x80
	}
	if opt.CheckCRC {
		buf[0] |= 0x40
	}
	// 写入长度
	lb := tlv_length_bytes(l, opt)
	copy(buf[1:headerSize], lb)
	pos += int(headerSize) - 1
	//
	if opt.CheckCRC {
		crc := utils.GetCrC(data)
		// 写入crc
		buf[headerSize-2] = crc[0]
		buf[headerSize-1] = crc[1]
	}
	// 写入数据
	copy(buf[pos:], data)
	pos += l
	return buf[0:pos], nil
}

func Decode(b []byte, opts ...FrameOption) (byte, []byte, error) {
	option := NewOption(opts...)
	return tlv_decode_opt(b, option)
}

func Next(b []byte, opts ...FrameOption) (byte, int, []byte, error) {
	option := NewOption(opts...)
	tag, l, dataBuf, err := tlv_decode_with_len(b, option)
	if err != nil {
		return 0, 0, nil, err
	}
	return tag, l, dataBuf, nil
}

func tlv_decode_opt(b []byte, opt *Option) (byte, []byte, error) {
	tag, _, dataBuf, err := tlv_decode_with_len(b, opt)
	if err != nil {
		return 0, nil, err
	}
	return tag, dataBuf, nil
}

func tlv_decode_with_len(b []byte, opt *Option) (byte, int, []byte, error) {
	if len(b) < TLVX_HEADER_MIN_SIZE {
		return 0, 0, nil, ErrInvalidValueLength
	}
	tag := b[0]
	// 64 32 24 16 | 8 4 2 1
	lengthSize := opt.MinLength
	if lengthSize <= 0 {
		return tag, 0, []byte{}, nil
	}
	crc_len := 0
	if tag&0x80 > 0 {
		lengthSize = opt.MaxLength
	}
	if tag&0x40 > 0 {
		crc_len = 2
	}
	// 需要去掉高2位（64 32）有效tag只有6位 1-63
	tag &= 0x3F
	headerSize := 1 + lengthSize + byte(crc_len)
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
		copy(u32[4-lengthSize:], b[1:minLen])
		l = int(binary.BigEndian.Uint32(u32))
	default:
		return 0, 0, nil, ErrInvalidLengthSize
	}
	if len(b) < int(int(headerSize)+l) {
		return 0, 0, nil, ErrInvalidValueLength
	}
	dataBuf := b[headerSize : int(headerSize)+l] // b[6:6+l]
	if crc_len > 0 {
		crc := b[headerSize-2 : headerSize]
		if !utils.CheckCRC(dataBuf, crc) {
			return 0, 0, nil, ErrInvalidCrc
		}
	}
	return tag, 1 + int(lengthSize) + l, dataBuf, nil
}
