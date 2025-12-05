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
	ErrInvalidStructType  = errors.New("invalid type")
	ErrInvalidBinType     = errors.New("invalid binary type")
)

const (
	TLV_TYPE_STRING  = 0x01
	TLV_TYPE_JSON    = 0x02
	TLV_TYPE_BINARY  = 0x03
	TLV_TYPE_INT64   = 0x04
	TLV_TYPE_UINT64  = 0x05
	TLV_TYPE_FLOAT64 = 0x06
)

const TLVX_HEADDER_SIZE = 5

type TlV struct {
	T byte    // tag type
	L uint16  // value length
	C [2]byte // crc16
	V []byte  // value
}

func NewTLVFromFrame(b []byte) (*TlV, error) {
	tag, data, err := tlv_decode(b)
	if err != nil {
		return nil, err
	}
	crc := utils.GetCrC(data)
	return &TlV{
		T: tag,
		L: uint16(len(data)),
		C: [2]byte{crc[0], crc[1]},
		V: data,
	}, nil
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
	l := binary.BigEndian.Uint16(b[1:3])
	crc := b[4:6]
	if len(b) < int(6+l) {
		return false
	}
	if !utils.CheckCRC(b[6:6+l], crc) {
		return false
	}
	return true
}

func tlv_encode(tag byte, data []byte) ([]byte, error) {
	l := len(data)
	if l > 0xFFFF {
		return nil, ErrInvalidValueLength
	}
	header := make([]byte, 6+l)
	header[0] = tag
	lb := make([]byte, 2)
	binary.BigEndian.PutUint16(lb, uint16(l))
	header[1] = lb[0]
	header[2] = lb[1]
	crc := utils.GetCrC(data)
	// 写入crc
	header[4] = crc[0]
	header[5] = crc[1]
	// 写入数据
	copy(header[6:], data)
	return header, nil
}

func tlv_decode(b []byte) (byte, []byte, error) {
	l := binary.BigEndian.Uint16(b[1:3])
	if len(b) < int(6+l) {
		return 0, nil, ErrInvalidValueLength
	}
	crc := b[4:6]
	dataBuf := b[6 : 6+l]
	if !utils.CheckCRC(dataBuf, crc) {
		return 0, nil, ErrInvalidCrc
	}
	return b[0], dataBuf, nil
}
