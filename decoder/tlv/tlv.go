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
	TLV_TYPE_INT64   = 0x04
	TLV_TYPE_UINT64  = 0x05
	TLV_TYPE_FLOAT64 = 0x06
)

const TLVX_HEADDER_SIZE = 5

type TlV struct {
	T byte   // tag type
	L uint16 // value length
	V []byte // value
}

func NewTLVFromFrame(b []byte, opts ...FrameOption) (*TlV, error) {
	t := &TlV{
		T: 0,
		L: 0,
		V: []byte{},
	}
	tag, data, err := tlv_decode(b)
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
	_, _, err := tlv_decode(b)
	return err == nil
}

func get_header_size(lLen byte, checkCRC bool) byte {
	c := byte(0x02)
	if !checkCRC {
		c = 0
	}
	return lLen + 1 + c
}

func tlv_encode(tag byte, data []byte, opts ...FrameOption) ([]byte, error) {
	opt := newOption(opts...)
	l := len(data)
	if l == 0x00 {
		return []byte{0, 0}, nil
	}
	if tag > 0x40 {
		return nil, ErrInvalidStructType
	}
	// 最大支持 0xFFFF 字节
	if l > 0xFFFF {
		return nil, ErrInvalidValueLength
	}
	// 根据长度大小判断是否需要扩展tag
	if l > 0xFF {
		tag |= 0x80
		opt.LengthSize = 2
	} else {
		opt.LengthSize = 1
	}

	headerSize := get_header_size(opt.LengthSize, opt.CheckCRC)
	buf := make([]byte, int(headerSize)+int(l))
	buf[0] = tag
	if opt.LengthSize == 2 {
		buf[0] |= 0x80
	}
	if opt.CheckCRC {
		buf[0] |= 0x40
	}

	lb := make([]byte, 2)
	binary.BigEndian.PutUint16(lb, uint16(l))

	switch opt.LengthSize {
	case 1:
		buf[1] = lb[1]
	case 2:
		buf[1] = lb[0]
		buf[2] = lb[1]
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

func tlv_decode(b []byte) (byte, []byte, error) {
	tag := b[0]
	// 64 32 24 16 | 8 4 2 1
	lengthSize := byte(1)
	checkCRC := false
	if tag&0x80 > 0 {
		lengthSize = 2
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
		l = int(binary.BigEndian.Uint16(b[1 : 1+lengthSize]))
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
