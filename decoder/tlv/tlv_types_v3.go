package tlv

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"reflect"
)

func tlv_frame_from_string_v3(v string, opts *Option) int {
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_STRING, []byte(v), opts)
	if err != nil {
		return 0
	}
	return r
}

func tlv_frame_from_json_v3(v any, opts *Option) int {
	jsonData, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_JSON, jsonData, opts)
	if err != nil {
		return 0
	}
	return r
}

func tlv_frame_from_slice_v3(v any, opts *Option) int {
	tag, total := int_data_size(v, opts)
	if total == 0 {
		return 0
	}
	// fmt.Println(tag, total)
	if tag > 0 && total > 0 {
		tag, size := get_tlv_tag(tag, total, opts)
		// fmt.Println(tag, size)
		opts.WriteByte(tag)
		// return r
		dv := get_tlv_len(total, opts)
		opts.Write(dv)
		r, err := write_any_data(opts, v)
		if err != nil {
			return 0
		}
		return r + int(size) + 1
	}

	jsonData, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_SLICE, jsonData, opts)
	if err != nil {
		return 0
	}
	return r
}

// Float32 从float32编码为tlv
func tlv_frame_from_float32_v3(v float32, opts *Option) int {
	bits := math.Float32bits(v)
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(byte(bits >> 24))
	buf.WriteByte(byte(bits >> 16))
	buf.WriteByte(byte(bits >> 8))
	buf.WriteByte(byte(bits))
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_FLOAT32, buf.Bytes(), opts)
	if err != nil {
		return 0
	}
	return r
}

// Float64 从float64编码为tlv
func tlv_frame_from_float64_v3(v float64, opts *Option) int {
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

	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_FLOAT64, buf.Bytes(), opts)
	if err != nil {
		return 0
	}
	return r
}

// Int32 从int32编码为tlv
func tlv_frame_from_int32_v3(v int32, opts *Option) int {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	// binary.BigEndian.PutUint32(buf[:4], uint32(v))
	buf.WriteByte(byte(v >> 24))
	buf.WriteByte(byte(v >> 16))
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v))
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_INT32, buf.Bytes(), opts)
	if err != nil {
		return 0
	}
	return r
}

// Int8 从int8编码为tlv
func tlv_frame_from_int8_v3(v int8, opts *Option) int {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(byte(v))
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_INT8, buf.Bytes(), opts)
	if err != nil {
		return 0
	}
	return r
}

// Int16 从int16编码为tlv
func tlv_frame_from_int16_v3(v int16, opts *Option) int {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v))
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_INT16, buf.Bytes(), opts)
	if err != nil {
		return 0
	}
	return r
}

// Int 从int编码为tlv
func tlv_frame_from_int_v3(v int, opts *Option) int {
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
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_INT, buf.Bytes(), opts)
	if err != nil {
		return 0
	}
	return r
}

// Int64 从int64编码为tlv
func tlv_frame_from_int64_v3(v int64, opts *Option) int {
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
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_INT64, buf.Bytes(), opts)
	if err != nil {
		return 0
	}
	return r
}

// Bool 从bool编码为tlv
func tlv_frame_from_bool_v3(v bool, opts *Option) int {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(0)
	if v {
		buf.WriteByte(1)
	}
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_BOOL, buf.Bytes(), opts)
	if err != nil {
		return 0
	}
	return r
}

// Complex64 从complex64编码为tlv
func tlv_frame_from_complex64_v3(v complex64, opts *Option) int {
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
	rst, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_COMPLEX64, buf.Bytes(), opts)
	if err != nil {
		return 0
	}
	return rst
}

// Complex128 从complex128编码为tlv
func tlv_frame_from_complex128_v3(v complex128, opts *Option) int {
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
	rst, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_COMPLEX128, buf.Bytes(), opts)
	if err != nil {
		return 0
	}
	return rst
}

// Nil 从nil编码为tlv
func tlv_frame_from_nil_v3(opts *Option) int {
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_NIL, []byte{0}, opts)
	if err != nil {
		return 0
	}
	return r
}

// Uintptr 从uintptr编码为tlv
func tlv_frame_from_uintptr_v3(v uintptr, opts *Option) int {
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
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_UINTPTR, buf.Bytes(), opts)
	if err != nil {
		return 0
	}
	return r
}

// Uint64 从uint64编码为tlv
func tlv_frame_from_uint64_v3(v uint64, opts *Option) int {
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
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_UINT64, buf.Bytes(), opts)
	if err != nil {
		return 0
	}
	return r
}

// Uint 从uint编码为tlv
func tlv_frame_from_uint_v3(v uint, opts *Option) int {
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
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_UINT, buf.Bytes(), opts)
	if err != nil {
		return 0
	}
	return r
}

// Uint32 从uint32编码为tlv
func tlv_frame_from_uint32_v3(v uint32, opts *Option) int {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(byte(v >> 24))
	buf.WriteByte(byte(v >> 16))
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v))
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_UINT32, buf.Bytes(), opts)
	if err != nil {
		return 0
	}
	return r
}

// Uint8 从uint8编码为tlv
func tlv_frame_from_uint8_v3(v uint8, opts *Option) int {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(v)
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_UINT8, buf.Bytes(), opts)
	if err != nil {
		return 0
	}
	return r
}

func tlv_frame_from_uint16_v3(v uint16, opts *Option) int {
	buf := opts.GetEncoder()
	defer opts.PutEncoder(buf)
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v))
	r, err := tlv_encode_option_with_buffer_v3(TLV_TYPE_UINT16, buf.Bytes(), opts)
	if err != nil {
		return 0
	}
	return r
}

func tlv_bytes_to_float64_v3(v []byte) float64 {
	bits := binary.BigEndian.Uint64(v)
	return math.Float64frombits(bits)
}

// Float64 从float64编码为tlv
func tlv_frame_to_float64_v3(v TLVFrame, opts *Option) (float64, error) {
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

func tlv_bytes_to_int64_v3(v []byte) int64 {
	return int64(binary.BigEndian.Uint64(v))
}

func tlv_frame_to_int64_v3(v TLVFrame, opts *Option) (int64, error) {
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

func tlv_bytes_to_uint64_v3(v []byte) uint64 {
	return binary.BigEndian.Uint64(v)
}

func tlv_frame_to_uint64_v3(v TLVFrame, opts *Option) (uint64, error) {
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

func tlv_deserialize_v3(v []byte, opts *Option) (*TlV, error) {
	if v == nil {
		return nil, ErrInvalidValueLength3
	}
	if len(v) < TLVX_HEADER_MIN_SIZE {
		return nil, ErrInvalidValueLength3
	}
	tlv, err := tlv_new_from_frame(v, opts)
	if tlv == nil {
		return nil, err
	}
	return tlv, nil
}

// Unmarshal 从tlv解码为结构体
func tlv_json_struct_v3(v []byte, t any, opts *Option) error {
	tlv, err := tlv_deserialize_v3(v, opts)
	if err != nil {
		return err
	}
	err = json.Unmarshal(tlv.Value(), t)
	if err != nil {
		return err
	}
	return nil
}

// Serialize 从结构体编码为tlv
// func tlv_serialize_v3(v any, opt *Option) int {
// 	if v == nil {
// 		return 0
// 	}
// 	buf := opt.GetEncoder()
// 	defer opt.PutEncoder(buf)
// 	switch ft := v.(type) {
// 	case float64:
// 		return tlv_frame_from_float64_v3(float64(ft), opt)
// 	case float32:
// 		return tlv_frame_from_float32_v3(float32(ft), opt)
// 	case int:
// 		return tlv_frame_from_int64_v3(int64(ft), opt)
// 	case uint:
// 		return tlv_frame_from_uint_v3(uint(ft), opt)
// 	case int8:
// 		return tlv_frame_from_int8_v3(int8(ft), opt)
// 	case int16:
// 		return tlv_frame_from_int16_v3(int16(ft), opt)
// 	case int32:
// 		return tlv_frame_from_int32_v3(int32(ft), opt)
// 	case int64:
// 		return tlv_frame_from_int64_v3(ft, opt)
// 	case uint8:
// 		return tlv_frame_from_uint8_v3(uint8(ft), opt)
// 	case uint16:
// 		return tlv_frame_from_uint16_v3(uint16(ft), opt)
// 	case uint32:
// 		return tlv_frame_from_uint32_v3(uint32(ft), opt)
// 	case uint64:
// 		return tlv_frame_from_uint64_v3(ft, opt)
// 	case []byte:
// 		return tlv_frame_from_slice_v3(ft, opt)
// 	case string:
// 		return tlv_frame_from_string_v3(ft, opt)
// 	case bool:
// 		return tlv_frame_from_bool_v3(ft, opt)
// 	case complex64:
// 		return tlv_frame_from_complex64_v3(ft, opt)
// 	case complex128:
// 		return tlv_frame_from_complex128_v3(ft, opt)
// 	case uintptr:
// 		return tlv_frame_from_uintptr_v3(ft, opt)
// 	default:
// 		return tlv_frame_from_json_v3(v, opt)
// 	}
// }

func tlv_serialize_value_v3(f reflect.Value, opt *Option) int {
	v := f.Interface()
	if v == nil {
		return 0
	}
	switch k := f.Kind(); k {
	case reflect.Float64:
		return tlv_frame_from_float64_v3(v.(float64), opt)
	case reflect.Float32:
		return tlv_frame_from_float32_v3(v.(float32), opt)
	case reflect.Int:
		return tlv_frame_from_int_v3(v.(int), opt)
	case reflect.Uint:
		return tlv_frame_from_uint_v3(v.(uint), opt)
	case reflect.Int8:
		return tlv_frame_from_int8_v3(v.(int8), opt)
	case reflect.Int16:
		return tlv_frame_from_int16_v3(v.(int16), opt)
	case reflect.Int32:
		return tlv_frame_from_int32_v3(v.(int32), opt)
	case reflect.Int64:
		return tlv_frame_from_int64_v3(v.(int64), opt)
	case reflect.Uint8:
		return tlv_frame_from_uint8_v3(v.(uint8), opt)
	case reflect.Uint16:
		return tlv_frame_from_uint16_v3(v.(uint16), opt)
	case reflect.Uint32:
		return tlv_frame_from_uint32_v3(v.(uint32), opt)
	case reflect.Uint64:
		return tlv_frame_from_uint64_v3(v.(uint64), opt)
	case reflect.Slice:
		return tlv_frame_from_slice_v3(v, opt)
	case reflect.String:
		return tlv_frame_from_string_v3(v.(string), opt)
	case reflect.Bool:
		return tlv_frame_from_bool_v3(v.(bool), opt)
	case reflect.Complex64:
		return tlv_frame_from_complex64_v3(v.(complex64), opt)
	case reflect.Complex128:
		return tlv_frame_from_complex128_v3(v.(complex128), opt)
	case reflect.Uintptr:
		return tlv_frame_from_uintptr_v3(v.(uintptr), opt)
	default:
		return tlv_frame_from_json_v3(v, opt)
	}
}
