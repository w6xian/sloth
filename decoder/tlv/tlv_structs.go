package tlv

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

func Marshal(v any, opts ...FrameOption) ([]byte, error) {
	option := newOptionV1(opts...)
	_, err := create_tlv_struct_v3(v, option)
	if err != nil {
		return nil, err
	}
	return option.Bytes(), nil
}
func Unmarshal(v []byte, s any, opts ...FrameOption) error {
	option := newOptionV1(opts...)
	return read_tlv_struct(v, s, option)
}

func FromStruct(t any, opts ...FrameOption) ([]byte, error) {
	option := NewOption(opts...)
	return create_tlv_struct(t, option)
}

func ToStruct(v []byte, s any, opts ...FrameOption) error {
	option := NewOption(opts...)
	return read_tlv_struct(v, s, option)
}

func ToString(v []byte, opts ...FrameOption) (string, error) {
	option := NewOption(opts...)
	return read_tlv_struct_string(v, option)
}

func read_tlv_struct_string(v []byte, opt *Option) (string, error) {
	t, l, v, err := Next(v, opt)
	if err != nil {
		return "", err
	}
	total := l
	if (t & 0x3F) != 0x3F {
		return "", errors.New("tlv tag is not struct")
	}
	pos := 0
	rst := []string{}
	for l > 0 && pos+2 < total {
		data := v[pos:]

		if len(data) < 2 {
			break
		}
		if (data[0] & 0x3E) != 0x3E {
			return "", errors.New("tlv field tag is not 0x3E")
		}
		ft, fl, fv, ferr := read_tlv_field(data, opt)
		if ferr != nil {
			return "", ferr
		}
		if (ft & 0x3E) == 0x3E {
			nt, nl, nv, nerr := Next(fv, opt)
			if nerr != nil {
				return "", nerr
			}
			if (nt & 0x3D) != 0x3D {
				return "", errors.New("tlv value tag is not 0x3D")
			}
			name := fmt.Sprintf("\"%s\"", string(nv))
			data := fv[nl:]
			tag := data[0]
			if (tag & 0x3F) == 0x3F {
				value, err := read_tlv_struct_string(data, opt)
				if err != nil {
					return "", err
				}
				rst = append(rst, fmt.Sprintf("%s:%s", name, value))
			} else {
				vt, _, vv, verr := Next(data, opt)
				if verr != nil {
					return "", verr
				}
				value := get_value_string(vt, vv, opt)
				rst = append(rst, fmt.Sprintf("%s:%s", name, value))
			}
		}
		pos += fl
		l -= fl
	}
	str := strings.Join(rst, ",")
	return fmt.Sprintf("{%s}", str), nil
}

func read_tlv_struct(v []byte, s any, opt *Option) error {
	sv := reflect.ValueOf(s)
	if sv.Kind() == reflect.Pointer {
		sv = sv.Elem()
	}
	ty := sv.Type()
	tags := map[string]string{}
	// 遍历结构体字段
	for num := 0; num < sv.NumField(); num++ {
		tyf := ty.Field(num)
		tag, err := get_tlv_struct_feild_name(tyf)
		if err != nil {
			continue
		}
		tags[tag] = tyf.Name
	}
	t, l, v, err := Next(v, opt)
	if err != nil {
		return err
	}
	total := len(v)
	if t != 0x3F {
		return errors.New("tlv tag is not struct")
	}
	pos := 0
	for l > 0 && pos+2 < total {
		ft, fl, fv, ferr := read_tlv_field(v[pos:], opt)
		if ferr != nil {
			return ferr
		}
		if ft == 0x3E {
			nt, nl, nv, nerr := Next(fv, opt)
			if nerr != nil {
				return nerr
			}
			if nt != 0x3D {
				return errors.New("tlv value tag is not 0x3D")
			}
			// 查找字段
			f := sv.FieldByName(tags[string(nv)])
			if !f.IsValid() {
				return errors.New("tlv field not found")
			}
			isPtr := f.Kind() == reflect.Pointer
			isStruct := f.Kind() == reflect.Struct
			if !isStruct {
				vt, _, vv, verr := Next(fv[nl:], opt)
				if verr != nil {
					return verr
				}
				// 设置值
				if vt == TLV_TYPE_JSON {
					instance := reflect.New(f.Type())
					err := json.Unmarshal(vv, instance.Interface())
					if err == nil {
						f.Set(instance.Elem())
					}
				} else {
					value := set_filed_value(isPtr, vt, vv, opt)
					f.Set(value)
				}
			} else {
				instance := reflect.New(f.Type())
				// 递归解析结构体
				err := read_tlv_struct(fv[nl:], instance.Interface(), opt)
				if err == nil {
					f.Set(instance.Elem())
				}
			}

		}
		pos += fl
		l -= fl
	}
	return nil
}

func get_value_string(tag byte, data []byte, opt *Option) string {
	// []string{"int", "int32", "int64", "uint", "uint32", "uint64", "float32", "float64", "string", "uint8", "bool"}
	// fmt.Println("tag", tag, data)
	switch tag {
	case TLV_TYPE_INT:
		by := bytes_to_int(data)
		return strconv.FormatInt(int64(by), 10)
	case TLV_TYPE_INT8:
		by := bytes_to_int8(data)
		return strconv.FormatInt(int64(by), 10)
	case TLV_TYPE_INT16:
		by := bytes_to_int16(data)
		return strconv.FormatInt(int64(by), 10)
	case TLV_TYPE_INT32:
		by := bytes_to_int32(data)
		return strconv.FormatInt(int64(by), 10)
	case TLV_TYPE_INT64:
		by := bytes_to_int64(data)
		return strconv.FormatInt(by, 10)
	case TLV_TYPE_UINT:
		by := bytes_to_uint(data)
		return strconv.FormatUint(uint64(by), 10)
	case TLV_TYPE_UINT8:
		by := bytes_to_byte(data)
		return strconv.FormatUint(uint64(by), 10)
	case TLV_TYPE_UINT16:
		by := bytes_to_uint16(data)
		return strconv.FormatUint(uint64(by), 10)
	case TLV_TYPE_UINT32:
		by := bytes_to_uint32(data)
		return strconv.FormatUint(uint64(by), 10)
	case TLV_TYPE_UINT64:
		by := bytes_to_uint64(data)
		return strconv.FormatUint(by, 10)
	case TLV_TYPE_FLOAT32:
		by := bytes_to_float32(data)
		return strconv.FormatFloat(float64(by), 'f', -1, 32)
	case TLV_TYPE_FLOAT64:
		by := bytes_to_float64(data)
		return strconv.FormatFloat(by, 'f', -1, 64)
	case TLV_TYPE_STRING:
		return fmt.Sprintf("\"%s\"", string(data))
	case TLV_TYPE_BOOL:
		by := bytes_to_bool(data)
		return strconv.FormatBool(by)
		// 复数类型
	case TLV_TYPE_COMPLEX64:
		by := bytes_to_complex64(data)
		return fmt.Sprintf("\"%v\"", by)
	case TLV_TYPE_COMPLEX128:
		by := bytes_to_complex128(data)
		return fmt.Sprintf("\"%v\"", by)
	case TLV_TYPE_UINTPTR:
		return fmt.Sprintf("%v", bytes_to_uintptr(data))
	case TLV_TYPE_RUNE:
		return fmt.Sprintf("\"%s\"", bytes_to_rune(data))
	case TLV_TYPE_SLICE:
		return fmt.Sprintf("%s", data)
	case TLV_TYPE_SLICE_BYTE, TLV_TYPE_SLICE_UINT8:
		return slice_byte_to_string(data)
	case TLV_TYPE_SLICE_INT64, TLV_TYPE_SLICE_INT:
		return slice_int64_to_string(data)
	case TLV_TYPE_SLICE_UINT64, TLV_TYPE_SLICE_UINT:
		return slice_uint64_to_string(data)
	case TLV_TYPE_SLICE_INT32:
		return slice_int32_to_string(data)
	case TLV_TYPE_SLICE_UINT32:
		return slice_uint32_to_string(data)
	case TLV_TYPE_SLICE_INT16:
		return slice_int16_to_string(data)
	case TLV_TYPE_SLICE_UINT16:
		return slice_uint16_to_string(data)
	case TLV_TYPE_SLICE_STRING:
		return slice_string_to_string(data, opt)
	case TLV_TYPE_SLICE_FLOAT32:
		return slice_float32_to_string(data)
	case TLV_TYPE_SLICE_FLOAT64:
		return slice_float64_to_string(data)
	case TLV_TYPE_JSON:
		// fmt.Println("TLV_TYPE_JSON:::", data)
		return fmt.Sprintf("%s", data)
	default:
		fmt.Println("tlv type not found", tag, data)
		return reflect.ValueOf(data).String()
	}
}

func read_tlv_field(v []byte, opt *Option) (byte, int, []byte, error) {
	t, l, v, err := Next(v, opt)
	if err != nil {
		return 0, 0, nil, err
	}
	return t, l, v, nil
}

func get_any_info(v any) (reflect.Kind, reflect.Type, reflect.Value) {
	sv := reflect.ValueOf(v)
	if sv.Kind() == reflect.Pointer {
		sv = sv.Elem()
	}
	ty := sv.Type()
	return ty.Kind(), ty, sv
}

func create_tlv_struct(t any, opt *Option) ([]byte, error) {
	kind, ty, sv := get_any_info(t)
	if kind != reflect.Struct {
		return nil, errors.New("tlv struct is not struct")
	}
	bg := opt.Encoder().Len()
	// tag
	opt.WriteByte(0x3F | 0x80)
	//length
	opt.Write(get_tlv_max_len_bytes(0, opt))
	obj_size := 0
	for num := 0; num < sv.NumField(); num++ {
		f := sv.Field(num)
		tyf := ty.Field(num)
		l, err := create_tlv_struct_feild_v3(f, tyf, opt)
		if err != nil {
			continue
		}
		obj_size += l
	}
	// fmt.Println("create_tlv_struct len:", opt.Encoder().Len())

	ls := get_tlv_max_len_bytes(obj_size, opt)
	// fmt.Printf("create_tlv_struct: %d\n", size)
	// fmt.Println(data[0], data[0]&0x3F)
	copy(opt.Bytes()[bg+1:bg+1+len(ls)], ls)
	return opt.Bytes(), nil
	// fs, err := tlv_encode_option_with_buffer_v1(0x3F, opt.Bytes(), opt)
	// if err != nil {
	// 	return nil, err
	// }
	// return fs, nil
}

func create_tlv_struct_v3(t any, opt *Option) (int, error) {
	kind, ty, sv := get_any_info(t)
	if kind != reflect.Struct {
		return 0, errors.New("tlv struct is not struct")
	}
	stat := opt.Encoder().Len()
	// tag
	opt.WriteByte(0x3F | 0x80)
	//length
	structLen := get_tlv_max_len_bytes(0, opt)
	opt.Write(structLen)
	obj_size := 0
	for num := 0; num < sv.NumField(); num++ {
		f := sv.Field(num)
		tyf := ty.Field(num)
		l, err := create_tlv_struct_feild_v3(f, tyf, opt)
		if err != nil {
			continue
		}
		obj_size += l
	}
	ls := get_tlv_max_len_bytes(obj_size, opt)
	copy(opt.Bytes()[stat+1:stat+1+len(structLen)], ls)
	return obj_size + 1 + int(opt.MaxLength), nil
}

func get_tlv_len_size(l int, opt *Option) byte {
	s := opt.MinLength
	if l > get_max_value_length(opt.MinLength) {
		s = opt.MaxLength
	}
	return s
}

func get_tlv_len(l int, opt *Option) []byte {
	s := opt.MinLength
	if l > get_max_value_length(opt.MinLength) {
		s = opt.MaxLength
	}
	binary.BigEndian.PutUint32(opt.size, uint32(l))
	return opt.size[4-s : 4]
}

func get_tlv_max_len_bytes(l int, opt *Option) []byte {
	s := opt.MaxLength
	binary.BigEndian.PutUint32(opt.size, uint32(l))
	return opt.size[4-s : 4]
}

func get_tlv_struct_feild_name(tyf reflect.StructField) (string, error) {
	tag := tyf.Tag.Get("tlv")
	if tag == "" {
		tag = tyf.Name
	}
	//是否为忽略
	if tag == "-" {
		return "", errors.New("tlv tag is -")
	}
	return tag, nil
}

// func create_tlv_struct_feild(f reflect.Value, tyf reflect.StructField) []byte {
// 	opt := NewOption()
// 	tag, err := get_tlv_struct_feild_name(tyf)
// 	if err != nil {
// 		return []byte{}
// 	}
// 	nam, err := tlv_encode_opt(0x3E, tag, opt)
// 	if err != nil {
// 		fmt.Println(err)
// 	}
// 	val := get_tlv_feild_value(f.Interface())
// 	if val == nil {
// 		return []byte{}
// 	}
// 	v, e := tlv_encode_opt(0x3D, val, opt)
// 	if e != nil {
// 		fmt.Println(e)
// 	}
// 	return get_tlv_struct_feild(nam, v)
// }

// func create_tlv_struct_feild_label(nam []byte, opt *Option) []byte {
// 	ln := len(nam)
// 	hsize := get_header_size(ln, opt)
// 	total := int(hsize) + ln
// 	frame := opt.pool.Get()
// 	defer opt.pool.Put(frame)
// 	frame[0] = 0x3D
// 	l := tlv_length_bytes(ln, opt)
// 	copy(frame[1:hsize], l)
// 	copy(frame[hsize:total], nam)
// 	return frame[0:total]
// }

func create_tlv_struct_feild_label_use_buffer(nam []byte, opt *Option) []byte {
	ln := len(nam)
	es := opt.GetEncoder()
	defer opt.PutEncoder(es)
	es.WriteByte(0x3D)
	l := tlv_length_bytes(ln, opt)
	es.Write(l)
	es.Write(nam)
	return es.Bytes()
}

// 最小长度为1字节
func create_tlv_struct_feild_label_use_buffer_v3(nam []byte, opt *Option) int {
	// binary.BigEndian.PutUint32(opt.size, uint32(ln))
	// opt.WriteByte(0x3D)
	// opt.WriteByte(opt.size[3])
	// opt.Write(nam)
	// fmt.Println("create_tlv_struct_feild_label_use_buffer_v3:Nam:", nam)
	tag, _ := get_tlv_tag(0x3D, len(nam), opt)
	s, err := tlv_encode_option_with_buffer_v3(tag, nam, opt)
	if err != nil {
		return 0
	}
	// fmt.Println("create_tlv_struct_feild_label_use_buffer_v3:Label:", s)
	return s
}

// func create_tlv_struct_feild_v1(f reflect.Value, tyf reflect.StructField, opt *Option) ([]byte, error) {
// 	label, err := get_tlv_struct_feild_name(tyf)
// 	if err != nil {
// 		return nil, err
// 	}

// 	pos := 0
// 	buf := opt.pool.Get()
// 	defer opt.pool.Put(buf)
// 	nam := create_tlv_struct_feild_label_use_buffer([]byte(label), opt)
// 	if err != nil {
// 		return nil, err
// 	}
// 	copy(buf[pos:], nam)
// 	pos += len(nam)
// 	if f.Kind() == reflect.Struct {
// 		frame, err := create_tlv_struct(f.Interface(), opt)
// 		if err != nil {
// 			return nil, err
// 		}
// 		copy(buf[pos:], frame)
// 		pos += len(frame)
// 	} else {
// 		val := tlv_serialize_value(f, opt)
// 		copy(buf[pos:], val)
// 		pos += len(val)
// 	}
// 	rst := opt.pool.Get()
// 	defer opt.pool.Put(rst)
// 	rst[0] = 0x3E
// 	hsize := get_header_size(pos, opt)
// 	total := int(hsize) + pos
// 	// 写入长度
// 	lb := tlv_length_bytes(pos, opt)
// 	copy(rst[1:hsize], lb)
// 	copy(rst[hsize:total], buf[0:pos])
// 	return rst[0:total], nil
// }

func create_tlv_struct_feild_v2(f reflect.Value, tyf reflect.StructField, opt *Option) ([]byte, error) {
	label, err := get_tlv_struct_feild_name(tyf)
	if err != nil {
		return nil, err
	}

	buf := opt.GetEncoder()
	defer opt.PutEncoder(buf)
	nam := create_tlv_struct_feild_label_use_buffer([]byte(label), opt)
	buf.Write(nam)
	if f.Kind() == reflect.Struct {
		frame, err := create_tlv_struct(f.Interface(), opt)
		if err != nil {
			return nil, err
		}
		buf.Write(frame)
	} else {
		val := tlv_serialize_value(f, opt)
		buf.Write(val)
	}
	return tlv_encode_option_with_buffer(0x3E, buf.Bytes(), opt)
	// return rst[0:total], nil
}

func create_tlv_struct_feild_v3(f reflect.Value, tyf reflect.StructField, opt *Option) (int, error) {
	label, err := get_tlv_struct_feild_name(tyf)

	if err != nil {
		return 0, err
	}
	stat := opt.Encoder().Len()
	opt.WriteByte(0x3E | 0x80)
	structLen := get_tlv_max_len_bytes(0, opt)
	opt.Write(structLen)

	l := 0
	label_l := create_tlv_struct_feild_label_use_buffer_v3([]byte(label), opt)
	l += label_l
	if f.Kind() == reflect.Struct {
		sl, err := create_tlv_struct_v3(f.Interface(), opt)
		if err != nil {
			return 0, err
		}
		l += sl
	} else {
		value_l := tlv_serialize_value_v3(f, opt)
		l += value_l
	}
	maxLen := get_tlv_max_len_bytes(l, opt)
	copy(opt.Bytes()[stat+1:stat+1+len(maxLen)], maxLen)
	return l + int(len(maxLen)) + 1, nil
}
