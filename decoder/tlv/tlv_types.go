package tlv

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"reflect"
)

func tlv_frame_from_string(v string, opts *Option) TLVFrame {
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_STRING, []byte(v), opts)
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
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_JSON, jsonData, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_frame_from_slice(v any, opts *Option) TLVFrame {
	switch v := v.(type) {
	// 1
	case []byte, []int8:
		r, err := tlv_encode_option_with_buffer(TLV_TYPE_SLICE_BYTE, v.([]byte), opts)
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
		r, err := tlv_encode_option_with_buffer(TLV_TYPE_SLICE_INT16, data, opts)
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
		r, err := tlv_encode_option_with_buffer(TLV_TYPE_SLICE_UINT16, data, opts)
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
		r, err := tlv_encode_option_with_buffer(TLV_TYPE_SLICE_INT32, data, opts)
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
		r, err := tlv_encode_option_with_buffer(TLV_TYPE_SLICE_UINT32, data, opts)
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
		r, err := tlv_encode_option_with_buffer(TLV_TYPE_SLICE_INT64, data, opts)
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
		r, err := tlv_encode_option_with_buffer(TLV_TYPE_SLICE_INT, data, opts)
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
		r, err := tlv_encode_option_with_buffer(TLV_TYPE_SLICE_UINT64, data, opts)
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
		r, err := tlv_encode_option_with_buffer(TLV_TYPE_SLICE_UINT, data, opts)
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
		r, err := tlv_encode_option_with_buffer(TLV_TYPE_SLICE_STRING, data, opts)
		if err != nil {
			return []byte{}
		}
		return r
	}
	jsonData, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_SLICE, jsonData, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

// Float32 从float32编码为tlv
func tlv_frame_from_float32(v float32, opts *Option) TLVFrame {
	bits := math.Float32bits(v)
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(byte(bits >> 24))
	buf.WriteByte(byte(bits >> 16))
	buf.WriteByte(byte(bits >> 8))
	buf.WriteByte(byte(bits))
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_FLOAT32, buf.Bytes(), opts)
	if err != nil {
		return []byte{}
	}
	return r
}

// Float64 从float64编码为tlv
func tlv_frame_from_float64(v float64, opts *Option) TLVFrame {
	vf := math.Float64bits(v)
	// buf := opts.pool.Get()
	// defer opts.pool.Put(buf)
	// binary.BigEndian.PutUint64(buf[:8], bits)
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(byte(vf >> 56))
	buf.WriteByte(byte(vf >> 48))
	buf.WriteByte(byte(vf >> 40))
	buf.WriteByte(byte(vf >> 32))
	buf.WriteByte(byte(vf >> 24))
	buf.WriteByte(byte(vf >> 16))
	buf.WriteByte(byte(vf >> 8))
	buf.WriteByte(byte(vf))

	r, err := tlv_encode_option_with_buffer(TLV_TYPE_FLOAT64, buf.Bytes(), opts)
	if err != nil {
		return []byte{}
	}
	return r
}

// Int32 从int32编码为tlv
func tlv_frame_from_int32(v int32, opts *Option) TLVFrame {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	// binary.BigEndian.PutUint32(buf[:4], uint32(v))
	buf.WriteByte(byte(v >> 24))
	buf.WriteByte(byte(v >> 16))
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v))
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_INT32, buf.Bytes(), opts)
	if err != nil {
		return []byte{}
	}
	return r
}

// Int8 从int8编码为tlv
func tlv_frame_from_int8(v int8, opts *Option) TLVFrame {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(byte(v))
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_INT8, buf.Bytes(), opts)
	if err != nil {
		return []byte{}
	}
	return r
}

// Int16 从int16编码为tlv
func tlv_frame_from_int16(v int16, opts *Option) TLVFrame {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v))
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_INT16, buf.Bytes(), opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_frame_from_int(v int, opts *Option) TLVFrame {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(byte(v >> 56))
	buf.WriteByte(byte(v >> 48))
	buf.WriteByte(byte(v >> 40))
	buf.WriteByte(byte(v >> 32))
	buf.WriteByte(byte(v >> 24))
	buf.WriteByte(byte(v >> 16))
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v))
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_INT, buf.Bytes(), opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_frame_from_int64(v int64, opts *Option) TLVFrame {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(byte(v >> 56))
	buf.WriteByte(byte(v >> 48))
	buf.WriteByte(byte(v >> 40))
	buf.WriteByte(byte(v >> 32))
	buf.WriteByte(byte(v >> 24))
	buf.WriteByte(byte(v >> 16))
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v))
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_INT64, buf.Bytes(), opts)
	if err != nil {
		return []byte{}
	}
	return r
}

// Bool 从bool编码为tlv
func tlv_frame_from_bool(v bool, opts *Option) TLVFrame {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(0)
	if v {
		buf.WriteByte(1)
	}
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_BOOL, buf.Bytes(), opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_frame_from_complex64(v complex64, opts *Option) TLVFrame {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	r := math.Float32bits(float32(real(complex128(v))))
	i := math.Float32bits(float32(imag(complex128(v))))
	buf.WriteByte(byte(r >> 24))
	buf.WriteByte(byte(r >> 16))
	buf.WriteByte(byte(r >> 8))
	buf.WriteByte(byte(r))
	buf.WriteByte(byte(i >> 24))
	buf.WriteByte(byte(i >> 16))
	buf.WriteByte(byte(i >> 8))
	buf.WriteByte(byte(i))
	rst, err := tlv_encode_option_with_buffer(TLV_TYPE_COMPLEX64, buf.Bytes(), opts)
	if err != nil {
		return []byte{}
	}
	return rst
}

func tlv_frame_from_complex128(v complex128, opts *Option) TLVFrame {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	r := math.Float64bits(real(v))
	i := math.Float64bits(imag(v))
	buf.WriteByte(byte(r >> 56))
	buf.WriteByte(byte(r >> 48))
	buf.WriteByte(byte(r >> 40))
	buf.WriteByte(byte(r >> 32))
	buf.WriteByte(byte(r >> 24))
	buf.WriteByte(byte(r >> 16))
	buf.WriteByte(byte(r >> 8))
	buf.WriteByte(byte(r))
	buf.WriteByte(byte(i >> 56))
	buf.WriteByte(byte(i >> 48))
	buf.WriteByte(byte(i >> 40))
	buf.WriteByte(byte(i >> 32))
	buf.WriteByte(byte(i >> 24))
	buf.WriteByte(byte(i >> 16))
	buf.WriteByte(byte(i >> 8))
	buf.WriteByte(byte(i))
	rst, err := tlv_encode_option_with_buffer(TLV_TYPE_COMPLEX128, buf.Bytes(), opts)
	if err != nil {
		return []byte{}
	}
	return rst
}

// Nil 从nil编码为tlv
func tlv_frame_from_nil(opts *Option) TLVFrame {
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_NIL, []byte{0}, opts)
	if err != nil {
		return []byte{}
	}
	return r
}

// Uint64 从uintptr编码为tlv
func tlv_frame_from_uintptr(v uintptr, opts *Option) TLVFrame {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(byte(v >> 56))
	buf.WriteByte(byte(v >> 48))
	buf.WriteByte(byte(v >> 40))
	buf.WriteByte(byte(v >> 32))
	buf.WriteByte(byte(v >> 24))
	buf.WriteByte(byte(v >> 16))
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v))
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_UINTPTR, buf.Bytes(), opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_frame_from_uint64(v uint64, opts *Option) TLVFrame {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(byte(v >> 56))
	buf.WriteByte(byte(v >> 48))
	buf.WriteByte(byte(v >> 40))
	buf.WriteByte(byte(v >> 32))
	buf.WriteByte(byte(v >> 24))
	buf.WriteByte(byte(v >> 16))
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v))
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_UINT64, buf.Bytes(), opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_frame_from_uint(v uint, opts *Option) TLVFrame {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(byte(v >> 56))
	buf.WriteByte(byte(v >> 48))
	buf.WriteByte(byte(v >> 40))
	buf.WriteByte(byte(v >> 32))
	buf.WriteByte(byte(v >> 24))
	buf.WriteByte(byte(v >> 16))
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v))
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_UINT, buf.Bytes(), opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_frame_from_uint32(v uint32, opts *Option) TLVFrame {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(byte(v >> 24))
	buf.WriteByte(byte(v >> 16))
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v))
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_UINT32, buf.Bytes(), opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_frame_from_uint8(v uint8, opts *Option) TLVFrame {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(v)
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_UINT8, buf.Bytes(), opts)
	if err != nil {
		return []byte{}
	}
	return r
}

func tlv_frame_from_uint16(v uint16, opts *Option) TLVFrame {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v))
	r, err := tlv_encode_option_with_buffer(TLV_TYPE_UINT16, buf.Bytes(), opts)
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
// func tlv_frame_to_struct(v TLVFrame, t any, opts *Option) error {
// 	if v == nil {
// 		return ErrInvalidValueLength
// 	}
// 	t, data, err := tlv_decode_opt(v, opts)
// 	if err != nil {
// 		return err
// 	}
// 	if len(data) == 0 {
// 		return ErrInvalidValueLength
// 	}
// 	if t != TLV_TYPE_JSON {
// 		return ErrInvalidStructType
// 	}
// 	err = json.Unmarshal(data, t)
// 	if err != nil {
// 		return err
// 	}
// 	return nil
// }

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
	buf := opt.GetEncoder()
	defer opt.PutEncoder(buf)
	switch ft := v.(type) {
	case float64:
		return tlv_frame_from_float64(float64(ft), opt)
	case float32:
		return tlv_frame_from_float32(float32(ft), opt)
	case int:
		return tlv_frame_from_int64(int64(ft), opt)
	case uint:
		return tlv_frame_from_uint64(uint64(ft), opt)
	case int8:
		return tlv_frame_from_int8(int8(ft), opt)
	case int16:
		return tlv_frame_from_int16(int16(ft), opt)
	case int32:
		return tlv_frame_from_int32(int32(ft), opt)
	case int64:
		return tlv_frame_from_int64(ft, opt)
	case uint8:
		return tlv_frame_from_uint8(uint8(ft), opt)
	case uint16:
		return tlv_frame_from_uint16(uint16(ft), opt)
	case uint32:
		return tlv_frame_from_uint32(uint32(ft), opt)
	case uint64:
		return tlv_frame_from_uint64(ft, opt)
	case []byte:
		return tlv_frame_from_slice(ft, opt)
	case string:
		return tlv_frame_from_string(ft, opt)
	case bool:
		return tlv_frame_from_bool(ft, opt)
	case complex64:
		return tlv_frame_from_complex64(ft, opt)
	case complex128:
		return tlv_frame_from_complex128(ft, opt)
	case uintptr:
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
		return tlv_frame_from_float64(v.(float64), opt)
	case reflect.Float32:
		return tlv_frame_from_float32(v.(float32), opt)
	case reflect.Int:
		return tlv_frame_from_int(v.(int), opt)
	case reflect.Uint:
		return tlv_frame_from_uint(v.(uint), opt)
	case reflect.Int8:
		return tlv_frame_from_int8(v.(int8), opt)
	case reflect.Int16:
		return tlv_frame_from_int16(v.(int16), opt)
	case reflect.Int32:
		return tlv_frame_from_int32(v.(int32), opt)
	case reflect.Int64:
		return tlv_frame_from_int64(v.(int64), opt)
	case reflect.Uint8:
		return tlv_frame_from_uint8(v.(uint8), opt)
	case reflect.Uint16:
		return tlv_frame_from_uint16(v.(uint16), opt)
	case reflect.Uint32:
		return tlv_frame_from_uint32(v.(uint32), opt)
	case reflect.Uint64:
		return tlv_frame_from_uint64(v.(uint64), opt)
	case reflect.Slice:
		return tlv_frame_from_slice(v, opt)
	case reflect.String:
		return tlv_frame_from_string(v.(string), opt)
	case reflect.Bool:
		return tlv_frame_from_bool(v.(bool), opt)
	case reflect.Complex64:
		return tlv_frame_from_complex64(v.(complex64), opt)
	case reflect.Complex128:
		return tlv_frame_from_complex128(v.(complex128), opt)
	case reflect.Uintptr:
		return tlv_frame_from_uintptr(v.(uintptr), opt)
	default:
		return tlv_frame_from_json(v, opt)
	}
}
