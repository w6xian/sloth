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
	MaxLength  byte
	MinLength  byte
}

func (opt *Option) Decode(v []byte) (byte, []byte, error) {
	return tlv_decode_opt(v, opt)
}

func (opt *Option) Encode(tag byte, data []byte) ([]byte, error) {
	return tlv_encode_opt(tag, data, opt)
}

func newOption(opts ...FrameOption) *Option {
	opt := &Option{
		CheckCRC:   false,
		LengthSize: 1,
		MaxLength:  0x02,
		MinLength:  0x01,
	}
	for _, o := range opts {
		o(opt)
	}
	if opt.LengthSize < opt.MinLength {
		opt.LengthSize = opt.MinLength
	}
	if opt.LengthSize > opt.MaxLength {
		opt.LengthSize = opt.MaxLength
	}
	return opt
}

func (opt *Option) EmptyFrame() TLVFrame {
	r, err := tlv_encode_opt(0x00, []byte(""), opt)
	if err != nil {
		return []byte{}
	}
	return r
}
func EmptyFrame(opts ...FrameOption) TLVFrame {
	r, err := tlv_encode(0x00, []byte(""), opts...)
	if err != nil {
		return []byte{}
	}
	return r
}

func (opt *Option) FrameFromString(v string) TLVFrame {
	r, err := tlv_encode_opt(TLV_TYPE_STRING, []byte(v), opt)
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

func (opt *Option) FrameFromJson(v any) TLVFrame {
	jsonData, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	r, err := tlv_encode_opt(TLV_TYPE_JSON, jsonData, opt)
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

func (opt *Option) FrameFromBinary(v Bin) TLVFrame {
	r, err := tlv_encode_opt(TLV_TYPE_BINARY, v, opt)
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

func (opt *Option) FrameFromFloat64(v float64) TLVFrame {
	bits := math.Float64bits(v)
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, bits)
	r, err := tlv_encode_opt(TLV_TYPE_FLOAT64, bytes, opt)
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
func (opt *Option) FrameFromInt64(v int64) TLVFrame {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, uint64(v))
	r, err := tlv_encode_opt(TLV_TYPE_INT64, bytes, opt)
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
func (opt *Option) FrameFromByte(v byte) TLVFrame {
	bytes := make([]byte, 1)
	bytes[0] = v
	r, err := tlv_encode_opt(TLV_TYPE_BYTE, bytes, opt)
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

// Nil 从nil编码为tlv
func (opt *Option) FrameFromNil() TLVFrame {
	r, err := tlv_encode_opt(TLV_TYPE_NIL, []byte{}, opt)
	if err != nil {
		return []byte{}
	}
	return r
}

// Nil 从nil编码为tlv
func FrameFromNil(opts ...FrameOption) TLVFrame {
	r, err := tlv_encode(TLV_TYPE_NIL, []byte{}, opts...)
	if err != nil {
		return []byte{}
	}
	return r
}

// Uint64 从uint64编码为tlv
func (opt *Option) FrameFromUint64(v uint64) TLVFrame {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, v)
	r, err := tlv_encode_opt(TLV_TYPE_UINT64, bytes, opt)
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

// FrameToFloat64 从tlv解码为float64
func (opt *Option) FrameToFloat64(v TLVFrame) (float64, error) {
	tag, data, err := tlv_decode_opt(v, opt)
	if err != nil {
		return 0, err
	}
	if tag != TLV_TYPE_FLOAT64 {
		return 0, ErrInvalidFloat64Type
	}
	fv := Bytes2Float64(data)
	return fv, nil
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

// FrameToInt64 从tlv解码为int64
func (opt *Option) FrameToInt64(v TLVFrame) (int64, error) {
	tag, data, err := tlv_decode_opt(v, opt)
	if err != nil {
		return 0, err
	}
	if tag != TLV_TYPE_INT64 {
		return 0, ErrInvalidInt64Type
	}
	bits := Bytes2Uint64(data)
	return int64(bits), nil
}

func Bytes2Uint64(v []byte) uint64 {
	return binary.BigEndian.Uint64(v)
}

// Uint64 从tlv解码为uint64
func (opt *Option) FrameToUint64(v TLVFrame) (uint64, error) {
	tag, data, err := tlv_decode_opt(v, opt)
	if err != nil {
		return 0, err
	}
	if tag != TLV_TYPE_UINT64 {
		return 0, ErrInvalidUint64Type
	}
	bits := Bytes2Uint64(data)
	return bits, nil
}

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

func Deserialize(v []byte, opts ...FrameOption) (*TlV, error) {
	if v == nil {
		return nil, ErrInvalidValueLength
	}
	if len(v) < TLVX_HEADER_MIN_SIZE {
		return nil, ErrInvalidValueLength
	}
	tlv, err := NewTLVFromFrame(v, opts...)
	if tlv == nil {
		return nil, err
	}
	return tlv, nil
}

// Unmarshal 从tlv解码为结构体
func Json2Struct(v []byte, t any, opts ...FrameOption) error {
	tlv, err := Deserialize(v, opts...)
	if err != nil {
		return err
	}
	err = json.Unmarshal(tlv.Value(), t)
	if err != nil {
		return err
	}
	return nil
}

// Marshal 从结构体编码为tlv
func Marshal(v any, opts ...FrameOption) []byte {
	return FrameFromJson(v, opts...)
}

func Serialize(v any, opts ...FrameOption) []byte {
	if v == nil {
		return []byte{TLV_TYPE_NIL, 0}
	}
	switch ft := v.(type) {
	case float64:
		// fmt.Println("float64", ft)
		return FrameFromFloat64(float64(ft), opts...)
	case float32:
		// fmt.Println("float32", ft)
		return FrameFromFloat64(float64(ft), opts...)
	case int:
		// fmt.Println("int", ft)
		return FrameFromInt64(int64(ft), opts...)
	case uint:
		// fmt.Println("uint", ft)
		return FrameFromUint64(uint64(ft), opts...)
	case int8:
		// fmt.Println("int8", ft)
		return FrameFromInt64(int64(ft), opts...)
	case uint8:
		// fmt.Println("uint8", ft)
		return FrameFromByte(byte(ft), opts...)
	case int16:
		// fmt.Println("int16", ft)
		return FrameFromInt64(int64(ft), opts...)
	case uint16:
		// fmt.Println("uint16", ft)
		return FrameFromUint64(uint64(ft), opts...)
	case int32:
		// fmt.Println("int32", ft)
		return FrameFromInt64(int64(ft), opts...)
	case uint32:
		// fmt.Println("uint32", ft)
		return FrameFromUint64(uint64(ft), opts...)
	case int64:
		// fmt.Println("int64", ft)
		return FrameFromInt64(ft, opts...)
	case uint64:
		// fmt.Println("uint64", ft)
		return FrameFromUint64(ft, opts...)
	case Bin:
		// fmt.Println("Bin", ft)
		return FrameFromBinary(ft, opts...)
	case []byte:
		// fmt.Println("[]byte", ft)
		return FrameFromBinary(ft, opts...)
	case string:
		// fmt.Println("string", ft)
		return FrameFromString(ft, opts...)
	default:
		// fmt.Println("default", ft)
		return FrameFromJson(v, opts...)
	}
}

// DefaultEncoder is the default encoder.
func DefaultEncoder(v any, opts ...FrameOption) ([]byte, error) {
	return Serialize(v, opts...), nil
}

// DefaultDecoder is the default decoder.
func DefaultDecoder(data []byte, opts ...FrameOption) ([]byte, error) {
	// 空数据
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) < TLVX_HEADER_MIN_SIZE {
		return data, nil
	}
	d, err := Deserialize(data, opts...)
	if err != nil {
		return data, nil
	}
	return d.Value(), nil
}
