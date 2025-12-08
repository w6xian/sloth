package tlv

import (
	"encoding/binary"
	"encoding/json"
	"math"
)

type Bin []byte
type TLVFrame []byte

type Option struct {
	CheckCRC   bool
	LengthSize byte
}

func newOption(opts ...FrameOption) Option {
	opt := Option{
		CheckCRC:   false,
		LengthSize: 1,
	}
	for _, o := range opts {
		o(&opt)
	}
	return opt
}
func EmptyFrame(opts ...FrameOption) TLVFrame {
	r, err := tlv_encode(0x00, []byte(""), opts...)
	if err != nil {
		return []byte{}
	}
	return r
}

func FrameFromString(v string, opts ...FrameOption) TLVFrame {
	r, err := tlv_encode(TLV_TYPE_STRING, []byte(v), opts...)
	if err != nil {
		return []byte{}
	}
	return r
}

func FrameFromJson(v any, opts ...FrameOption) TLVFrame {
	jsonData, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	r, err := tlv_encode(TLV_TYPE_JSON, jsonData, opts...)
	if err != nil {
		return []byte{}
	}
	return r
}

func FrameFromBinary(v Bin, opts ...FrameOption) TLVFrame {
	r, err := tlv_encode(TLV_TYPE_BINARY, v, opts...)
	if err != nil {
		return []byte{}
	}
	return r
}

// Float64 从float64编码为tlv
func FrameFromFloat64(v float64, opts ...FrameOption) TLVFrame {
	bits := math.Float64bits(v)
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, bits)
	r, err := tlv_encode(TLV_TYPE_FLOAT64, bytes, opts...)
	if err != nil {
		return []byte{}
	}
	return r
}

// Int64 从int64编码为tlv
func FrameFromInt64(v int64, opts ...FrameOption) TLVFrame {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, uint64(v))
	r, err := tlv_encode(TLV_TYPE_INT64, bytes, opts...)
	if err != nil {
		return []byte{}
	}
	return r
}

// Byte 从byte编码为tlv
func FrameFromByte(v byte, opts ...FrameOption) TLVFrame {
	bytes := make([]byte, 1)
	bytes[0] = v
	r, err := tlv_encode(TLV_TYPE_BYTE, bytes, opts...)
	if err != nil {
		return []byte{}
	}
	return r
}

// Uint64 从uint64编码为tlv
func FrameFromUint64(v uint64, opts ...FrameOption) TLVFrame {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, v)
	r, err := tlv_encode(TLV_TYPE_UINT64, bytes, opts...)
	if err != nil {
		return []byte{}
	}
	return r
}

func Bytes2Float64(v []byte) float64 {
	bits := binary.BigEndian.Uint64(v)
	return math.Float64frombits(bits)
}

func FrameToFloat64(v TLVFrame) (float64, error) {
	if len(v) != 8+TLVX_HEADER_SIZE {
		return 0, ErrInvalidFloat64
	}
	if v[0] != TLV_TYPE_FLOAT64 {
		return 0, ErrInvalidFloat64Type
	}
	fv := Bytes2Float64(v[TLVX_HEADER_SIZE:])
	return fv, nil
}

func Bytes2Int64(v []byte) int64 {
	return int64(binary.BigEndian.Uint64(v))
}

func FrameToInt64(v TLVFrame) (int64, error) {
	if len(v) != 8+TLVX_HEADER_SIZE {
		return 0, ErrInvalidInt64
	}
	if v[0] != TLV_TYPE_INT64 {
		return 0, ErrInvalidInt64Type
	}
	bits := Bytes2Uint64(v[TLVX_HEADER_SIZE:])
	return int64(bits), nil
}

func Bytes2Uint64(v []byte) uint64 {
	return binary.BigEndian.Uint64(v)
}

// Uint64 从tlv解码为uint64
func FrameToUint64(v TLVFrame) (uint64, error) {
	if len(v) != 8+TLVX_HEADER_SIZE {
		return 0, ErrInvalidUint64
	}
	if v[0] != TLV_TYPE_UINT64 {
		return 0, ErrInvalidUint64Type
	}
	bits := Bytes2Uint64(v[TLVX_HEADER_SIZE:])
	return bits, nil
}

// Int64 从int64编码为tlv
func FrameToStruct(v TLVFrame, t any) error {
	if v == nil {
		return ErrInvalidValueLength
	}
	if len(v) < TLVX_HEADER_SIZE {
		return ErrInvalidValueLength
	}
	if v[0] != TLV_TYPE_JSON {
		return ErrInvalidStructType
	}
	_, data, err := tlv_decode(v)
	if err != nil {
		return err
	}
	err = json.Unmarshal(data, t)
	if err != nil {
		return err
	}
	return nil
}

func FrameToBin(v TLVFrame) (Bin, error) {
	if v == nil {
		return nil, ErrInvalidValueLength
	}
	if len(v) < TLVX_HEADER_SIZE {
		return nil, ErrInvalidValueLength
	}
	if v[0] != TLV_TYPE_BINARY {
		return nil, ErrInvalidBinType
	}
	_, data, err := tlv_decode(v)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func Deserialize(v []byte) (*TlV, error) {
	if v == nil {
		return nil, ErrInvalidValueLength
	}
	if len(v) < TLVX_HEADER_MIN_SIZE {
		return nil, ErrInvalidValueLength
	}
	tlv, err := NewTLVFromFrame(v)
	if tlv == nil {
		return nil, err
	}
	return tlv, nil
}
func Serialize(v any) []byte {
	var key string
	if v == nil {
		return []byte(key)
	}
	switch ft := v.(type) {
	case float64:
		return FrameFromFloat64(float64(ft))
	case float32:
		return FrameFromFloat64(float64(ft))
	case int:
		return FrameFromInt64(int64(ft))
	case uint:
		return FrameFromUint64(uint64(ft))
	case int8:
		return FrameFromInt64(int64(ft))
	case uint8:
		return FrameFromByte(byte(ft))
	case int16:
		return FrameFromInt64(int64(ft))
	case uint16:
		return FrameFromUint64(uint64(ft))
	case int32:
		return FrameFromInt64(int64(ft))
	case uint32:
		return FrameFromUint64(uint64(ft))
	case int64:
		return FrameFromInt64(ft)
	case uint64:
		return FrameFromUint64(ft)
	case Bin:
		return FrameFromBinary(ft)
	case []byte:
		return FrameFromBinary(ft)
	case string:
		return FrameFromString(ft)
	default:
		return FrameFromJson(v)
	}
}

// DefaultEncoder is the default encoder.
func DefaultEncoder(v any) ([]byte, error) {
	return Serialize(v), nil
}

// DefaultDecoder is the default decoder.
func DefaultDecoder(data []byte) ([]byte, error) {
	d, err := Deserialize(data)
	if err != nil {
		return nil, err
	}
	return d.Value(), nil
}
