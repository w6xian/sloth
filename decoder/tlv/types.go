package tlv

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
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
	option := NewOption(opts...)
	jsonData, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_JSON, jsonData, option)
	if err != nil {
		return []byte{}
	}
	return r
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
		return ErrInvalidValueLength
	}
	if len(v) < TLVX_HEADER_SIZE {
		return ErrInvalidValueLength
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
		return nil, ErrInvalidValueLength
	}
	if len(v) < TLVX_HEADER_SIZE {
		return nil, ErrInvalidValueLength
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
		return nil, ErrInvalidValueLength
	}
	if len(v) < TLVX_HEADER_MIN_SIZE {
		return nil, ErrInvalidValueLength
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
	fmt.Println(name, len(data))
	switch name {
	case "int":
		by := BytesToInt(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "int16":
		by := BytesToInt16(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "int32":
		by := BytesToInt32(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "int64":
		by := BytesToInt64(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uint":
		by := BytesToUint(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uint16":
		by := BytesToUint16(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uint32":
		by := BytesToUint32(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uint64":
		by := BytesToUint64(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "float32":
		by := BytesToFloat32(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "float64":
		by := BytesToFloat64(data)
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
		by := BytesToUintptr(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "bool":
		by := BytesToBool(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	default:
		return reflect.ValueOf(data)
	}
}

func set_filed_value(prt bool, tag byte, data []byte) reflect.Value {
	// []string{"int", "int32", "int64", "uint", "uint32", "uint64", "float32", "float64", "string", "uint8", "bool"}
	// fmt.Println(tag, len(data), data)
	switch tag {
	case TLV_TYPE_INT:
		by := BytesToInt(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_INT8:
		by := BytesToInt8(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_INT16:
		by := BytesToInt16(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_INT32:
		by := BytesToInt32(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_INT64:
		by := BytesToInt64(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_UINT:
		by := BytesToUint(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_UINT8:
		by := BytesToUint8(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_UINT16:
		by := BytesToUint16(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_UINT32:
		by := BytesToUint32(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_UINT64:
		by := BytesToUint64(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_FLOAT32:
		by := BytesToFloat32(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_FLOAT64:
		by := BytesToFloat64(data)
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
		by := BytesToBool(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
		// 复数类型
	case TLV_TYPE_COMPLEX64:
		by := BytesToComplex64(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_COMPLEX128:
		by := BytesToComplex128(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_UINTPTR:
		by := BytesToUintptr(data)
		if prt {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case TLV_TYPE_RUNE:
		by := BytesToRune(data)
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
		by := slice_bytes_to_slice_strings(data)
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
