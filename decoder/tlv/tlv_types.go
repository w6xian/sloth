package tlv

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
)

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

func tlv_frame_from_slice(v any, opts *Option) TLVFrame {
	switch v := v.(type) {
	// 1
	case []byte, []int8:
		r, err := tlv_encode_opt(TLV_TYPE_SLICE_BYTE, v.([]byte), opts)
		if err != nil {
			return []byte{}
		}
		return r
	case []int16:
		// 2字节为一个int16
		d := []int16(v)
		data := []byte{}
		// 2字节为一个int16
		for i := 0; i < len(d); i++ {
			data = append(data, Int16ToBytes(d[i])...)
		}
		r, err := tlv_encode_opt(TLV_TYPE_SLICE_INT16, data, opts)
		if err != nil {
			return []byte{}
		}
		return r
	case []uint16:
		// 2字节为一个uint16
		d := []uint16(v)
		data := []byte{}
		// 2字节为一个uint16
		for i := 0; i < len(d); i++ {
			data = append(data, Uint16ToBytes(d[i])...)
		}
		r, err := tlv_encode_opt(TLV_TYPE_SLICE_UINT16, data, opts)
		if err != nil {
			return []byte{}
		}
		return r
	case []int32:
		// 4字节为一个int32
		d := []int32(v)
		data := []byte{}
		// 4字节为一个int32
		for i := 0; i < len(d); i++ {
			data = append(data, Int32ToBytes(d[i])...)
		}
		r, err := tlv_encode_opt(TLV_TYPE_SLICE_INT32, data, opts)
		if err != nil {
			return []byte{}
		}
		return r
	case []uint32:
		// 4字节为一个uint32
		d := []uint32(v)
		data := []byte{}
		// 4字节为一个uint32
		for i := 0; i < len(d); i++ {
			data = append(data, Uint32ToBytes(d[i])...)
		}
		r, err := tlv_encode_opt(TLV_TYPE_SLICE_UINT32, data, opts)
		if err != nil {
			return []byte{}
		}
		return r
	case []int64:
		// 8字节为一个int64
		d := []int64(v)
		data := []byte{}
		// 8字节为一个int64
		for i := 0; i < len(d); i++ {
			data = append(data, IntToBytes(d[i])...)
		}
		r, err := tlv_encode_opt(TLV_TYPE_SLICE_INT64, data, opts)
		if err != nil {
			return []byte{}
		}
		return r
	case []int:
		// 8字节为一个int64
		d := []int(v)
		data := []byte{}
		// 8字节为一个int64
		for i := 0; i < len(d); i++ {
			data = append(data, IntToBytes(int64(d[i]))...)
		}
		r, err := tlv_encode_opt(TLV_TYPE_SLICE_INT64, data, opts)
		if err != nil {
			return []byte{}
		}
		return r
	case []uint64:
		// 8字节为一个uint64
		d := []uint64(v)
		data := []byte{}
		// 8字节为一个uint64
		for i := 0; i < len(d); i++ {
			data = append(data, UintToBytes(d[i])...)
		}
		r, err := tlv_encode_opt(TLV_TYPE_SLICE_UINT64, data, opts)
		if err != nil {
			return []byte{}
		}
		return r
	case []uint:
		// 8字节为一个uint64
		d := []uint(v)
		data := []byte{}
		// 8字节为一个uint64
		for i := 0; i < len(d); i++ {
			data = append(data, UintToBytes(uint64(d[i]))...)
		}
		r, err := tlv_encode_opt(TLV_TYPE_SLICE_UINT64, data, opts)
		if err != nil {
			return []byte{}
		}
		return r
	case []string:
		// 字符串编码为json
		data := []byte{}
		for i := 0; i < len(v); i++ {
			data = append(data, tlv_frame_from_string(v[i], opts)...)
		}
		r, err := tlv_encode_opt(TLV_TYPE_SLICE_STRING, data, opts)
		if err != nil {
			return []byte{}
		}
		return r
	}
	jsonData, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	r, err := tlv_encode_opt(TLV_TYPE_SLICE, jsonData, opts)
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

// Rune 从rune编码为tlv
func tlv_frame_from_rune(v rune, opts *Option) TLVFrame {
	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, uint32(v))
	r, err := tlv_encode_opt(TLV_TYPE_RUNE, bytes, opts)
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
	r, err := tlv_encode_opt(TLV_TYPE_UINT8, bytes, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

// Bool 从bool编码为tlv
func tlv_frame_from_bool(v bool, opts *Option) TLVFrame {
	bytes := make([]byte, 1)
	if v {
		bytes[0] = 1
	} else {
		bytes[0] = 0
	}
	r, err := tlv_encode_opt(TLV_TYPE_BOOL, bytes, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_frame_from_complex64(v complex64, opts *Option) TLVFrame {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint32(bytes, math.Float32bits(float32(real(complex128(v)))))
	binary.BigEndian.PutUint32(bytes[4:], math.Float32bits(float32(imag(complex128(v)))))
	r, err := tlv_encode_opt(TLV_TYPE_COMPLEX64, bytes, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_frame_from_complex128(v complex128, opts *Option) TLVFrame {
	bytes := make([]byte, 16)
	binary.BigEndian.PutUint64(bytes, math.Float64bits(real(v)))
	binary.BigEndian.PutUint64(bytes[8:], math.Float64bits(imag(v)))
	r, err := tlv_encode_opt(TLV_TYPE_COMPLEX128, bytes, opts)
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

// Uint64 从uintptr编码为tlv
func tlv_frame_from_uintptr(v uintptr, opts *Option) TLVFrame {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, uint64(v))
	r, err := tlv_encode_opt(TLV_TYPE_UINTPTR, bytes, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

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
		fmt.Println("int32", ft)
		return tlv_frame_from_int32(int32(ft), opt)
	case int64:
		// fmt.Println("int64", ft)
		return tlv_frame_from_int64(ft, opt)
	case uint8:
		// fmt.Println("uint8", ft)
		return tlv_frame_from_uint8(uint8(ft), opt)
	case uint16:
		return tlv_frame_from_uint16(uint16(ft), opt)
		// fmt.Println("uint16", ft)
	case uint32:
		// fmt.Println("uint32", ft)
		return tlv_frame_from_uint32(uint32(ft), opt)
	case uint64:
		// fmt.Println("uint64", ft)
		return tlv_frame_from_uint64(ft, opt)
	case []byte:
		fmt.Println("[]byte", ft)
		return tlv_frame_from_slice(ft, opt)
	case string:
		// fmt.Println("string", ft)
		return tlv_frame_from_string(ft, opt)
	case bool:
		// fmt.Println("bool", ft)
		return tlv_frame_from_bool(ft, opt)
	case complex64:
		// fmt.Println("complex64", ft)
		return tlv_frame_from_complex64(ft, opt)
	case complex128:
		// fmt.Println("complex128", ft)
		return tlv_frame_from_complex128(ft, opt)
	case uintptr:
		// fmt.Println("uintptr:", ft)
		return tlv_frame_from_uintptr(ft, opt)
	default:
		return tlv_frame_from_json(v, opt)
	}
}

func tlv_serialize_value(f reflect.Value, opt *Option) []byte {
	v := f.Interface()
	if v == nil {
		return []byte{TLV_TYPE_NIL, 0}
	}
	switch k := f.Kind(); k {
	case reflect.Float64:
		// fmt.Println("float64", ft)
		return tlv_frame_from_float64(v.(float64), opt)
	case reflect.Float32:
		// fmt.Println("float32", ft)
		return tlv_frame_from_float32(v.(float32), opt)
	case reflect.Int:
		// fmt.Println("int", ft)
		return tlv_frame_from_int64(int64(v.(int)), opt)
	case reflect.Uint:
		// fmt.Println("uint", ft)
		return tlv_frame_from_uint64(uint64(v.(uint)), opt)
	case reflect.Int8:
		// fmt.Println("int8", ft)
		return tlv_frame_from_int8(v.(int8), opt)
	case reflect.Int16:
		// fmt.Println("int16", ft)
		return tlv_frame_from_int16(v.(int16), opt)
	case reflect.Int32:
		// fmt.Println("int32", ft)
		return tlv_frame_from_int32(v.(int32), opt)
	case reflect.Int64:
		// fmt.Println("int64", ft)
		return tlv_frame_from_int64(v.(int64), opt)
	case reflect.Uint8:
		// fmt.Println("uint8", ft)
		return tlv_frame_from_uint8(v.(uint8), opt)
	case reflect.Uint16:
		// fmt.Println("uint16", ft)
		return tlv_frame_from_uint16(v.(uint16), opt)
	case reflect.Uint32:
		// fmt.Println("uint32", ft)
		return tlv_frame_from_uint32(v.(uint32), opt)
	case reflect.Uint64:
		// fmt.Println("uint64", ft)
		return tlv_frame_from_uint64(v.(uint64), opt)
	case reflect.Slice:
		return tlv_frame_from_slice(v, opt)
	case reflect.String:
		// fmt.Println("string", ft)
		return tlv_frame_from_string(v.(string), opt)
	case reflect.Bool:
		// fmt.Println("bool", ft)
		return tlv_frame_from_bool(v.(bool), opt)
	case reflect.Complex64:
		// fmt.Println("complex64", ft)
		return tlv_frame_from_complex64(v.(complex64), opt)
	case reflect.Complex128:
		// fmt.Println("complex128", ft)
		return tlv_frame_from_complex128(v.(complex128), opt)
	case reflect.Uintptr:
		// fmt.Println("uintptr:", ft)
		return tlv_frame_from_uintptr(v.(uintptr), opt)
	default:
		return tlv_frame_from_json(v, opt)
	}
}
