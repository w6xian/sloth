package tlv

import (
	"encoding/binary"
	"encoding/json"
	"math"
)

func tlv_empty_frame(opts *Option) TLVFrame {
	r, err := tlv_encode_opt(TLV_TYPE_NIL, []byte{}, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_frame_from_string(v string, opts *Option) TLVFrame {
	r, err := tlv_encode_opt(TLV_TYPE_STRING, []byte(v), opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_frame_from_json(v any, opts *Option) TLVFrame {
	jsonData, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	r, err := tlv_encode_opt(TLV_TYPE_JSON, jsonData, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_frame_from_binary(v Bin, opts *Option) TLVFrame {
	r, err := tlv_encode_opt(TLV_TYPE_BINARY, v, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

// Float32 从float32编码为tlv
func tlv_frame_from_float32(v float32, opts *Option) TLVFrame {
	bits := math.Float32bits(v)
	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, bits)
	r, err := tlv_encode_opt(TLV_TYPE_FLOAT32, bytes, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

// Float64 从float64编码为tlv
func tlv_frame_from_float64(v float64, opts *Option) TLVFrame {
	bits := math.Float64bits(v)
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, bits)
	r, err := tlv_encode_opt(TLV_TYPE_FLOAT64, bytes, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

// Int32 从int32编码为tlv
func tlv_frame_from_int32(v int32, opts *Option) TLVFrame {
	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, uint32(v))
	r, err := tlv_encode_opt(TLV_TYPE_INT32, bytes, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

// Int8 从int8编码为tlv
func tlv_frame_from_int8(v int8, opts *Option) TLVFrame {
	bytes := make([]byte, 1)
	bytes[0] = byte(v)
	r, err := tlv_encode_opt(TLV_TYPE_INT8, bytes, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

// Int16 从int16编码为tlv
func tlv_frame_from_int16(v int16, opts *Option) TLVFrame {
	bytes := make([]byte, 2)
	binary.BigEndian.PutUint16(bytes, uint16(v))
	r, err := tlv_encode_opt(TLV_TYPE_INT16, bytes, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_frame_from_int64(v int64, opts *Option) TLVFrame {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, uint64(v))
	r, err := tlv_encode_opt(TLV_TYPE_INT64, bytes, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

// Byte 从byte编码为tlv
func tlv_frame_from_byte(v byte, opts *Option) TLVFrame {
	bytes := make([]byte, 1)
	bytes[0] = v
	r, err := tlv_encode_opt(TLV_TYPE_BYTE, bytes, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

// Nil 从nil编码为tlv
func tlv_frame_from_nil(opts *Option) TLVFrame {
	r, err := tlv_encode_opt(TLV_TYPE_NIL, []byte{}, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

// Uint64 从uint64编码为tlv
func tlv_frame_from_uint64(v uint64, opts *Option) TLVFrame {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, v)
	r, err := tlv_encode_opt(TLV_TYPE_UINT64, bytes, opts)
	if err != nil {
		return []byte{}
	}
	return r
}
func tlv_frame_from_uint32(v uint32, opts *Option) TLVFrame {
	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, v)
	r, err := tlv_encode_opt(TLV_TYPE_UINT32, bytes, opts)
	if err != nil {
		return []byte{}
	}
	return r
}
func tlv_frame_from_uint8(v uint8, opts *Option) TLVFrame {
	bytes := make([]byte, 1)
	bytes[0] = v
	r, err := tlv_encode_opt(TLV_TYPE_UINT8, bytes, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_frame_from_uint16(v uint16, opts *Option) TLVFrame {
	bytes := make([]byte, 2)
	binary.BigEndian.PutUint16(bytes, v)
	r, err := tlv_encode_opt(TLV_TYPE_UINT16, bytes, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_bytes_to_float64(v []byte) float64 {
	bits := binary.BigEndian.Uint64(v)
	return math.Float64frombits(bits)
}

func tlv_frame_to_float64(v TLVFrame, opts *Option) (float64, error) {
	t, data, err := tlv_decode_opt(v, opts)
	if err != nil {
		return 0, err
	}
	if len(data) != 8 {
		return 0, ErrInvalidFloat64
	}
	if t != TLV_TYPE_FLOAT64 {
		return 0, ErrInvalidFloat64Type
	}
	fv := tlv_bytes_to_float64(data)
	return fv, nil
}

func tlv_bytes_to_int64(v []byte) int64 {
	return int64(binary.BigEndian.Uint64(v))
}

func tlv_frame_to_int64(v TLVFrame, opts *Option) (int64, error) {
	t, data, err := tlv_decode_opt(v, opts)
	if err != nil {
		return 0, err
	}
	if len(data) != 8 {
		return 0, ErrInvalidInt64
	}
	if t != TLV_TYPE_INT64 {
		return 0, ErrInvalidInt64Type
	}
	return tlv_bytes_to_int64(data), nil
}

func tlv_bytes_to_uint64(v []byte) uint64 {
	return binary.BigEndian.Uint64(v)
}

func tlv_frame_to_uint64(v TLVFrame, opts *Option) (uint64, error) {
	t, data, err := tlv_decode_opt(v, opts)
	if err != nil {
		return 0, err
	}
	if len(data) != 8 {
		return 0, ErrInvalidUint64
	}
	if t != TLV_TYPE_UINT64 {
		return 0, ErrInvalidUint64Type
	}
	return tlv_bytes_to_uint64(data), nil
}

// Uint64 从uint64编码为tlv
func tlv_frame_to_struct(v TLVFrame, t any, opts *Option) error {
	if v == nil {
		return ErrInvalidValueLength
	}
	t, data, err := tlv_decode_opt(v, opts)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return ErrInvalidValueLength
	}
	if t != TLV_TYPE_JSON {
		return ErrInvalidStructType
	}
	err = json.Unmarshal(data, t)
	if err != nil {
		return err
	}
	return nil
}

func tlv_frame_to_binary(v TLVFrame, opts *Option) (Bin, error) {
	if v == nil {
		return nil, ErrInvalidValueLength
	}
	if len(v) < TLVX_HEADER_SIZE {
		return nil, ErrInvalidValueLength
	}
	t, data, err := tlv_decode_opt(v, opts)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, ErrInvalidValueLength
	}
	if t != TLV_TYPE_BINARY {
		return nil, ErrInvalidBinType
	}
	return data, nil
}

func tlv_deserialize(v []byte, opts *Option) (*TlV, error) {
	if v == nil {
		return nil, ErrInvalidValueLength
	}
	if len(v) < TLVX_HEADER_MIN_SIZE {
		return nil, ErrInvalidValueLength
	}
	tlv, err := tlv_new_from_frame(v, opts)
	if tlv == nil {
		return nil, err
	}
	return tlv, nil
}

// Unmarshal 从tlv解码为结构体
func tlv_json_struct(v []byte, t any, opts *Option) error {
	tlv, err := tlv_deserialize(v, opts)
	if err != nil {
		return err
	}
	err = json.Unmarshal(tlv.Value(), t)
	if err != nil {
		return err
	}
	return nil
}

func tlv_serialize(v any, opt *Option) []byte {
	if v == nil {
		return []byte{TLV_TYPE_NIL, 0}
	}
	switch ft := v.(type) {
	case float64:
		// fmt.Println("float64", ft)
		return tlv_frame_from_float64(float64(ft), opt)
	case float32:
		// fmt.Println("float32", ft)
		return tlv_frame_from_float32(float32(ft), opt)
	case int:
		// fmt.Println("int", ft)
		return tlv_frame_from_int64(int64(ft), opt)
	case uint:
		// fmt.Println("uint", ft)
		return tlv_frame_from_uint64(uint64(ft), opt)
	case int8:
		// fmt.Println("int8", ft)
		return tlv_frame_from_int8(int8(ft), opt)
	case int16:
		// fmt.Println("int16", ft)
		return tlv_frame_from_int16(int16(ft), opt)
	case int32:
		// fmt.Println("int32", ft)
		return tlv_frame_from_int32(int32(ft), opt)
	case int64:
		// fmt.Println("int64", ft)
		return tlv_frame_from_int64(ft, opt)
	case uint8:
		// fmt.Println("uint8", ft)
		return tlv_frame_from_uint8(uint8(ft), opt)
	case uint16:
		// fmt.Println("uint16", ft)
		return tlv_frame_from_uint16(uint16(ft), opt)
	case uint32:
		// fmt.Println("uint32", ft)
		return tlv_frame_from_uint32(uint32(ft), opt)
	case uint64:
		// fmt.Println("uint64", ft)
		return tlv_frame_from_uint64(ft, opt)
	case Bin:
		// fmt.Println("Bin", ft)
		return tlv_frame_from_binary(ft, opt)
	case []byte:
		// fmt.Println("[]byte", ft)
		return tlv_frame_from_binary(ft, opt)
	case string:
		// fmt.Println("string", ft)
		return tlv_frame_from_string(ft, opt)
	default:
		// fmt.Println("default", ft)
		return tlv_frame_from_json(v, opt)
	}
}
