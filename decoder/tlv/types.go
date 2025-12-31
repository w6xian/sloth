package tlv

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"reflect"
)

func NewTLVFromFrame(frame TLVFrame, opts ...FrameOption) (*TlV, error) {
	option := NewOption(opts...)
	tag, data, err := tlv_decode_opt(frame, option)
	if err != nil {
		return nil, err
	}
	return &TlV{T: tag, L: uint16(len(data)), V: data}, nil
}

func EmptyFrame(opts ...FrameOption) TLVFrame {
	option := NewOption(opts...)
	return tlv_empty_frame(option)
}

func FrameFromString(v string, opts ...FrameOption) TLVFrame {
	option := NewOption(opts...)
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_STRING, []byte(v), option)
	if err != nil {
		return []byte{}
	}
	return r
}

func FrameFromJson(v any, opts ...FrameOption) TLVFrame {
	option := newOption(opts...)
	jsonData, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	_, err = tlv_encode_option_with_buffer_v3(TLV_TYPE_JSON, jsonData, option)
	if err != nil {
		return []byte{}
	}
	return option.Bytes()
}

// Float64 从float64编码为tlv
func FrameFromFloat64(v float64, opts ...FrameOption) TLVFrame {
	option := NewOption(opts...)
	bits := math.Float64bits(v)
	bytes := make([]byte, 8+option.MinLength+1)
	binary.BigEndian.PutUint64(bytes[option.MinLength+1:], bits)
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_FLOAT64, bytes, option)
	if err != nil {
		return []byte{}
	}
	return r

}

// Int64 从int64编码为tlv
func FrameFromInt64(v int64, opts ...FrameOption) TLVFrame {
	option := NewOption(opts...)
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, uint64(v))
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_INT64, bytes, option)
	if err != nil {
		return []byte{}
	}
	return r
}

// Byte 从byte编码为tlv
func FrameFromByte(v byte, opts ...FrameOption) TLVFrame {
	option := NewOption(opts...)
	bytes := make([]byte, 1)
	bytes[0] = v
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_UINT8, bytes, option)
	if err != nil {
		return []byte{}
	}
	return r
}

// Nil 从nil编码为tlv
func FrameFromNil(opts ...FrameOption) TLVFrame {
	option := NewOption(opts...)
	return tlv_frame_from_nil(option)
}

// Uint64 从uint64编码为tlv
func FrameFromUint64(v uint64, opts ...FrameOption) TLVFrame {
	option := NewOption(opts...)
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, v)
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_UINT64, bytes, option)
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
func FrameToStruct(v TLVFrame, t any, opts ...FrameOption) error {
	if v == nil {
		return ErrInvalidValueLength2
	}
	if len(v) < TLVX_HEADER_SIZE {
		return ErrInvalidValueLength2
	}

	option := NewOption(opts...)
	_, data, err := tlv_decode_opt(v, option)
	if err != nil {
		return err
	}
	err = json.Unmarshal(data, t)
	if err != nil {
		return err
	}
	return nil
}

func FrameToBin(v TLVFrame, opts ...FrameOption) (Bin, error) {
	if v == nil {
		return nil, ErrInvalidValueLength2
	}
	if len(v) < TLVX_HEADER_SIZE {
		return nil, ErrInvalidValueLength2
	}

	option := NewOption(opts...)
	_, data, err := tlv_decode_opt(v, option)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func Deserialize(v []byte, opts ...FrameOption) (*TlV, error) {
	if v == nil {
		return nil, ErrInvalidValueLength2
	}
	if len(v) < TLVX_HEADER_MIN_SIZE {
		return nil, ErrInvalidValueLength2
	}
	newOpt := NewOption(opts...)
	tlv, err := tlv_new_from_frame(v, newOpt)
	if tlv == nil {
		return nil, err
	}
	return tlv, nil
}

// Unmarshal 从tlv解码为结构体
func Json2Struct(v []byte, t any, opts ...FrameOption) error {
	option := NewOption(opts...)
	tlv, err := tlv_deserialize(v, option)
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
func Json(v any, opts ...FrameOption) []byte {
	return FrameFromJson(v, opts...)
}

func Serialize(v any, opts ...FrameOption) []byte {
	newOpt := NewOption(opts...)
	return tlv_serialize(v, newOpt)
}

// DefaultEncoder is the default encoder.
func DefaultEncoder(v any) ([]byte, error) {
	return Serialize(v), nil
}

// DefaultDecoder is the default decoder.
func DefaultDecoder(data []byte) ([]byte, error) {
	// 空数据
	newOpt := NewOption()
	_, data, err := tlv_decode_opt(data, newOpt)
	if err != nil {
		return data, nil
	}
	return data, nil
}

func GetType(needPtr bool, name string, data []byte) reflect.Value {
	// []string{"int", "int32", "int64", "uint", "uint32", "uint64", "float32", "float64", "string", "uint8", "bool"}
	switch name {
	case "int":
		by := bytes_to_int(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "int16":
		by := bytes_to_int16(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "int32":
		by := bytes_to_int32(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "int64":
		by := bytes_to_int64(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uint":
		by := bytes_to_uint(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uint16":
		by := bytes_to_uint16(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uint32":
		by := bytes_to_uint32(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uint64":
		by := bytes_to_uint64(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "float32":
		by := bytes_to_float32(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "float64":
		by := bytes_to_float64(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "string":
		str := string(data)
		if needPtr {
			return reflect.ValueOf(&str)
		}
		return reflect.ValueOf(str)
	case "uint8":
		by := data[0]
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "int8":
		by := int8(data[0])
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uintptr":
		by := bytes_to_uintptr(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "bool":
		by := bytes_to_bool(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	default:
		return reflect.ValueOf(data)
	}
}

func set_filed_value(prt bool, tag byte, data []byte, opt *Option) reflect.Value {
	// []string{"int", "int32", "int64", "uint", "uint32", "uint64", "float32", "float64", "string", "uint8", "bool"}
	// fmt.Println(tag, len(data), data)
	switch tag {
	case TLV_TYPE_INT:
		by := bytes_to_int(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_INT8:
		by := bytes_to_int8(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_INT16:
		by := bytes_to_int16(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_INT32:
		by := bytes_to_int32(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_INT64:
		by := bytes_to_int64(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_UINT:
		by := bytes_to_uint(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_UINT8:
		by := bytes_to_uint8(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_UINT16:
		by := bytes_to_uint16(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_UINT32:
		by := bytes_to_uint32(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_UINT64:
		by := bytes_to_uint64(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_FLOAT32:
		by := bytes_to_float32(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_FLOAT64:
		by := bytes_to_float64(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_STRING:
		str := string(data)
		if prt {
			return reflect.ValueOf(&str)
		}
		return reflect.ValueOf(str)
	case TLV_TYPE_BOOL:
		by := bytes_to_bool(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
		// 复数类型
	case TLV_TYPE_COMPLEX64:
		by := bytes_to_complex64(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_COMPLEX128:
		by := bytes_to_complex128(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_UINTPTR:
		by := bytes_to_uintptr(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_RUNE:
		by := bytes_to_rune(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_SLICE:
		if prt {
			return reflect.ValueOf(&data)
		}
		return reflect.ValueOf(data)
	case TLV_TYPE_SLICE_BYTE:
		if prt {
			return reflect.ValueOf(&data)
		}
		return reflect.ValueOf(data)
	case TLV_TYPE_SLICE_INT:
		by := conv_to_slice_int(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_SLICE_INT64:
		by := conv_to_slice_int64(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_SLICE_UINT:
		by := conv_to_slice_uint(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_SLICE_UINT64:
		by := conv_to_slice_uint64(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_SLICE_INT32:
		by := conv_to_slice_int32(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_SLICE_UINT32:
		by := conv_to_slice_uint32(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_SLICE_INT16:
		by := conv_to_slice_int16(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_SLICE_UINT16:
		by := conv_to_slice_uint16(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_SLICE_STRING:
		by := slice_bytes_to_slice_strings(data, opt)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_SLICE_FLOAT32:
		by := conv_to_slice_float32(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_SLICE_FLOAT64:
		by := conv_to_slice_float64(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	default:
		if prt {
			return reflect.ValueOf(&data)
		}
		return reflect.ValueOf(data)
	}
}

func int_data_size(data any, opt *Option) (byte, int) {
	//fmt.Println(data, reflect.TypeOf(data))
	switch data := data.(type) {
	case bool, *bool:
		return TLV_TYPE_BOOL, 1
	case int8, *int8:
		return TLV_TYPE_INT8, 1
	case uint8, *uint8:
		return TLV_TYPE_UINT8, 1
	case int16, *int16:
		return TLV_TYPE_INT16, 2
	case uint16, *uint16:
		return TLV_TYPE_UINT16, 2
	case []bool:
		return TLV_TYPE_SLICE_BOOL, len(data)
	case []int8:
		return TLV_TYPE_SLICE_INT8, len(data)
	case []uint8:
		return TLV_TYPE_SLICE_UINT8, len(data)
	case []int16:
		return TLV_TYPE_SLICE_INT16, 2 * len(data)
	case []uint16:
		return TLV_TYPE_SLICE_UINT16, 2 * len(data)
	case int32, uint32, *int32, *uint32:
		return TLV_TYPE_INT32, 4
	case []int32:
		return TLV_TYPE_SLICE_INT32, 4 * len(data)
	case []int:
		return TLV_TYPE_SLICE_INT, 8 * len(data)
	case []uint:
		return TLV_TYPE_SLICE_UINT, 8 * len(data)
	case []uint32:
		return TLV_TYPE_SLICE_UINT32, 4 * len(data)
	case int64, uint64, *int64, *uint64:
		return TLV_TYPE_INT64, 8
	case []int64:
		return TLV_TYPE_SLICE_INT64, 8 * len(data)
	case []uint64:
		return TLV_TYPE_SLICE_UINT64, 8 * len(data)
	case float32, *float32:
		return TLV_TYPE_FLOAT32, 4
	case float64, *float64:
		return TLV_TYPE_FLOAT64, 8
	case []float32:
		return TLV_TYPE_SLICE_FLOAT32, 4 * len(data)
	case []float64:
		return TLV_TYPE_SLICE_FLOAT64, 8 * len(data)
	case complex64, *complex64:
		return TLV_TYPE_COMPLEX64, 8
	case complex128, *complex128:
		return TLV_TYPE_COMPLEX128, 16
	case []complex64:
		return TLV_TYPE_SLICE_COMPLEX64, 8 * len(data)
	case []complex128:
		return TLV_TYPE_SLICE_COMPLEX128, 16 * len(data)
	case []string:
		total := 0
		for _, s := range data {
			l := len([]byte(s))
			total += l
			total += int(get_tlv_len_size(l, opt))
			total += 1
		}
		return TLV_TYPE_SLICE_STRING, total
	default:
		return 0, 0
	}
}

func write_any_data(opt *Option, data any) (int, error) {
	switch data := data.(type) {
	case bool, int8, uint8, *bool, *int8, *uint8:
		opt.WriteByte(data.(byte))
		return 1, nil
	case []bool:
		for _, b := range data {
			if b {
				opt.WriteByte(1)
			} else {
				opt.WriteByte(0)
			}
		}
		return len(data), nil
	case []int8:
		for _, b := range data {
			opt.WriteByte(byte(b))
		}
		return len(data), nil
	case []uint8:
		for _, b := range data {
			opt.WriteByte(b)
		}
		return len(data), nil
	case int16, uint16, *int16, *uint16:
		binary.Write(opt, binary.BigEndian, data)
		return 2, nil
	case []int16:
		for _, b := range data {
			binary.Write(opt, binary.BigEndian, b)
		}
		return 2 * len(data), nil
	case []uint16:
		for _, b := range data {
			opt.WriteByte(byte(b >> 8))
			opt.WriteByte(byte(b))
		}
		return 2 * len(data), nil
	case int32, uint32, *int32, *uint32:
		b := data.(uint32)
		opt.WriteByte(byte(b >> 24))
		opt.WriteByte(byte(b >> 16))
		opt.WriteByte(byte(b >> 8))
		opt.WriteByte(byte(b))
		return 4, nil
	case []int32:
		for _, b := range data {
			opt.WriteByte(byte(b >> 24))
			opt.WriteByte(byte(b >> 16))
			opt.WriteByte(byte(b >> 8))
			opt.WriteByte(byte(b))
		}
		return 4 * len(data), nil
	case []uint32:
		for _, b := range data {
			opt.WriteByte(byte(b >> 24))
			opt.WriteByte(byte(b >> 16))
			opt.WriteByte(byte(b >> 8))
			opt.WriteByte(byte(b))
		}
		return 4 * len(data), nil
	case int64, uint64, *int64, *uint64:
		b := data.(uint64)
		opt.WriteByte(byte(b >> 56))
		opt.WriteByte(byte(b >> 48))
		opt.WriteByte(byte(b >> 40))
		opt.WriteByte(byte(b >> 32))
		opt.WriteByte(byte(b >> 24))
		opt.WriteByte(byte(b >> 16))
		opt.WriteByte(byte(b >> 8))
		opt.WriteByte(byte(b))
		return 8, nil
	case []int64:
		for _, b := range data {
			opt.WriteByte(byte(b >> 56))
			opt.WriteByte(byte(b >> 48))
			opt.WriteByte(byte(b >> 40))
			opt.WriteByte(byte(b >> 32))
			opt.WriteByte(byte(b >> 24))
			opt.WriteByte(byte(b >> 16))
			opt.WriteByte(byte(b >> 8))
			opt.WriteByte(byte(b))
		}
		return 8 * len(data), nil
	case []int:
		for _, b := range data {
			opt.WriteByte(byte(b >> 56))
			opt.WriteByte(byte(b >> 48))
			opt.WriteByte(byte(b >> 40))
			opt.WriteByte(byte(b >> 32))
			opt.WriteByte(byte(b >> 24))
			opt.WriteByte(byte(b >> 16))
			opt.WriteByte(byte(b >> 8))
			opt.WriteByte(byte(b))
		}
		return 8 * len(data), nil
	case []uint64:
		for _, b := range data {
			opt.WriteByte(byte(b >> 56))
			opt.WriteByte(byte(b >> 48))
			opt.WriteByte(byte(b >> 40))
			opt.WriteByte(byte(b >> 32))
			opt.WriteByte(byte(b >> 24))
			opt.WriteByte(byte(b >> 16))
			opt.WriteByte(byte(b >> 8))
			opt.WriteByte(byte(b))
		}
		return 8 * len(data), nil
	case []uint:
		for _, b := range data {
			opt.WriteByte(byte(b >> 56))
			opt.WriteByte(byte(b >> 48))
			opt.WriteByte(byte(b >> 40))
			opt.WriteByte(byte(b >> 32))
			opt.WriteByte(byte(b >> 24))
			opt.WriteByte(byte(b >> 16))
			opt.WriteByte(byte(b >> 8))
			opt.WriteByte(byte(b))
		}
		return 8 * len(data), nil
	case float32, *float32:
		b := data.(float32)
		bits := math.Float32bits(b)
		opt.WriteByte(byte(bits >> 24))
		opt.WriteByte(byte(bits >> 16))
		opt.WriteByte(byte(bits >> 8))
		opt.WriteByte(byte(bits))
		return 4, nil
	case float64, *float64:
		b := data.(float64)
		bits := math.Float64bits(b)
		opt.WriteByte(byte(bits >> 56))
		opt.WriteByte(byte(bits >> 48))
		opt.WriteByte(byte(bits >> 40))
		opt.WriteByte(byte(bits >> 32))
		opt.WriteByte(byte(bits >> 24))
		opt.WriteByte(byte(bits >> 16))
		opt.WriteByte(byte(bits >> 8))
		opt.WriteByte(byte(bits))
		return 8, nil
	case []float32:
		for _, b := range data {
			bits := math.Float32bits(b)
			opt.WriteByte(byte(bits >> 24))
			opt.WriteByte(byte(bits >> 16))
			opt.WriteByte(byte(bits >> 8))
			opt.WriteByte(byte(bits))
		}
		return 4 * len(data), nil
	case []float64:
		for _, b := range data {
			bits := math.Float64bits(b)
			opt.WriteByte(byte(bits >> 56))
			opt.WriteByte(byte(bits >> 48))
			opt.WriteByte(byte(bits >> 40))
			opt.WriteByte(byte(bits >> 32))
			opt.WriteByte(byte(bits >> 24))
			opt.WriteByte(byte(bits >> 16))
			opt.WriteByte(byte(bits >> 8))
			opt.WriteByte(byte(bits))
		}
		return 8 * len(data), nil
	case []string:
		l := 0
		for _, s := range data {
			p, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_STRING, []byte(s), opt)
			if err != nil {
				return 0, err
			}
			l += p
		}
		return l, nil
	}
	return 0, errors.New("invalid data type")
}
