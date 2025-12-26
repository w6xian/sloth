package tlv

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/w6xian/sloth/internal/utils"
)

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
	t, l, v, err := Next(v)
	if err != nil {
		return "", err
	}
	total := l
	if t != 0x3F {
		return "", errors.New("tlv tag is not struct")
	}
	pos := 0
	rst := []string{}
	for l > 0 && pos+2 < total {
		data := v[pos:]
		if len(data) < 2 {
			break
		}
		if data[0] != 0x3E {
			return "", errors.New("tlv field tag is not 0x3E")
		}
		ft, fl, fv, ferr := read_tlv_field(data)
		if ferr != nil {
			return "", ferr
		}
		if ft == 0x3E {
			nt, nl, nv, nerr := Next(fv)
			if nerr != nil {
				return "", nerr
			}
			if nt != 0x3D {
				return "", errors.New("tlv value tag is not 0x3D")
			}
			name := fmt.Sprintf("\"%s\"", string(nv))
			vt, _, vv, verr := Next(fv[nl:])
			if verr != nil {
				return "", verr
			}
			value := get_value_string(vt, vv)
			rst = append(rst, fmt.Sprintf("%s:%s", name, value))
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
		tags[string(tag)] = tyf.Name
	}
	t, l, v, err := Next(v)
	if err != nil {
		return err
	}
	total := l
	if t != 0x3F {
		return errors.New("tlv tag is not struct")
	}
	pos := 0
	for l > 0 && pos+2 < total {
		ft, fl, fv, ferr := read_tlv_field(v[pos:])
		if ferr != nil {
			return ferr
		}
		if ft == 0x3E {
			nt, nl, nv, nerr := Next(fv)
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
				_, _, vv, verr := Next(fv[nl:])
				if verr != nil {
					return verr
				}
				// 设置值
				value := utils.GetType(isPtr, f.Type().Name(), vv)
				f.Set(value)
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

func get_value_string(tag byte, data []byte) string {
	// []string{"int", "int32", "int64", "uint", "uint32", "uint64", "float32", "float64", "string", "uint8", "bool"}
	switch tag {
	case TLV_TYPE_INT:
		by := utils.BytesToInt(data)
		return strconv.FormatInt(int64(by), 10)
	case TLV_TYPE_INT8:
		by := utils.BytesToByte(data)
		return strconv.FormatInt(int64(by), 10)
	case TLV_TYPE_INT16:
		by := utils.BytesToInt16(data)
		return strconv.FormatInt(int64(by), 10)
	case TLV_TYPE_INT32:
		by := utils.BytesToInt32(data)
		return strconv.FormatInt(int64(by), 10)
	case TLV_TYPE_INT64:
		by := utils.BytesToInt64(data)
		return strconv.FormatInt(by, 10)
	case TLV_TYPE_UINT:
		by := utils.BytesToUint(data)
		return strconv.FormatUint(uint64(by), 10)
	case TLV_TYPE_UINT8:
		by := utils.BytesToByte(data)
		return strconv.FormatUint(uint64(by), 10)
	case TLV_TYPE_UINT16:
		by := utils.BytesToUint16(data)
		return strconv.FormatUint(uint64(by), 10)
	case TLV_TYPE_UINT32:
		by := utils.BytesToUint32(data)
		return strconv.FormatUint(uint64(by), 10)
	case TLV_TYPE_UINT64:
		by := utils.BytesToUint64(data)
		return strconv.FormatUint(by, 10)
	case TLV_TYPE_FLOAT32:
		by := utils.BytesToFloat32(data)
		return strconv.FormatFloat(float64(by), 'f', -1, 32)
	case TLV_TYPE_FLOAT64:
		by := utils.BytesToFloat64(data)
		return strconv.FormatFloat(by, 'f', -1, 64)
	case TLV_TYPE_STRING:
		return fmt.Sprintf("\"%s\"", string(data))
	case TLV_TYPE_BOOL:
		by := utils.BytesToBool(data)
		return strconv.FormatBool(by)
		// 复数类型
	case TLV_TYPE_COMPLEX64:
		by := utils.BytesToComplex64(data)
		return fmt.Sprintf("\"%v\"", by)
	case TLV_TYPE_COMPLEX128:
		by := utils.BytesToComplex128(data)
		return fmt.Sprintf("\"%v\"", by)
	case TLV_TYPE_UINTPTR:
		return fmt.Sprintf("%v", utils.BytesToUintptr(data))
	case TLV_TYPE_RUNE:
		return fmt.Sprintf("\"%s\"", utils.BytesToRune(data))
	case TLV_TYPE_JSON:
		// fmt.Println("TLV_TYPE_JSON:::", data)
		return fmt.Sprintf("%s", data)
	default:
		fmt.Println("tlv type not found", tag, data)
		return reflect.ValueOf(data).String()
	}
}

func read_tlv_field(v []byte) (byte, int, []byte, error) {
	t, l, v, err := Next(v)
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
		return []byte{}, errors.New("tlv struct is not struct")
	}
	fs := []byte{}
	for num := 0; num < sv.NumField(); num++ {
		f := sv.Field(num)
		tyf := ty.Field(num)
		frame := create_tlv_struct_feild_v1(f, tyf, opt)
		// fmt.Println("::", tyf.Name, frame)
		fs = append(fs, frame...)
	}
	fs, err := tlv_encode_opt(0x3F, fs, opt)
	if err != nil {
		return []byte{}, err
	}
	return fs, nil
}

func get_tlv_struct_feild_name(tyf reflect.StructField) ([]byte, error) {
	tag := tyf.Tag.Get("tlv")
	if tag == "" {
		tag = tyf.Name
	}
	//是否为忽略
	if tag == "-" {
		return []byte{}, errors.New("tlv tag is -")
	}
	return []byte(tag), nil
}

func create_tlv_struct_feild(f reflect.Value, tyf reflect.StructField) []byte {
	opt := NewOption()
	tag, err := get_tlv_struct_feild_name(tyf)
	if err != nil {
		return []byte{}
	}
	nam, err := tlv_encode_opt(0x3E, tag, opt)
	if err != nil {
		fmt.Println(err)
	}
	val := get_tlv_feild_value(f.Interface())
	if val == nil {
		return []byte{}
	}
	v, e := tlv_encode_opt(0x3D, val, opt)
	if e != nil {
		fmt.Println(e)
	}
	return get_tlv_struct_feild(nam, v)
}

func create_tlv_struct_feild_v1(f reflect.Value, tyf reflect.StructField, opt *Option) []byte {

	tag, err := get_tlv_struct_feild_name(tyf)
	if err != nil {
		return []byte{}
	}
	nam, err := tlv_encode_opt(0x3D, tag, opt)
	if err != nil {
		fmt.Println(err)
	}
	val := EmptyFrame()
	if f.Kind() == reflect.Struct {
		frame, err := create_tlv_struct(f.Interface(), opt)
		if err == nil {
			val = frame
		}
	} else {
		val = tlv_serialize_sting(tyf.Name, f.Interface(), opt)
	}
	if val == nil {
		val = EmptyFrame()
	}
	return get_tlv_struct_feild(nam, val)
}

// 不用binary.Write，因为它会根据系统字节序编码,语言兼容
func get_tlv_feild_value(val any) []byte {
	switch v := val.(type) {
	case []uint8:
		return v
	case string:
		return []byte(v)
	case int:
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(v))
		return buf
	case int8:
		buf := make([]byte, 1)
		buf[0] = byte(v)
		return buf
	case int16:
		buf := make([]byte, 2)
		binary.BigEndian.PutUint16(buf, uint16(v))
		return buf
	case int32:
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, uint32(v))
		return buf
	case int64:
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(v))
		return buf
	case uint:
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(v))
		return buf
	case uint8:
		return []byte{v}
	case uint16:
		buf := make([]byte, 2)
		binary.BigEndian.PutUint16(buf, v)
		return buf
	case uint32:
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, v)
		return buf
	case uint64:
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, v)
		return buf
	case float32:
		bits := math.Float32bits(v)
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, bits)
		return buf
	case float64:
		bits := math.Float64bits(v)
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, bits)
		return buf
	case bool:
		if v {
			return []byte{1}
		}
		return []byte{0}
	default:
		return nil
	}
}

func get_tlv_struct_feild(name []byte, value []byte) []byte {
	t := []byte{0x3E, byte(len(name) + len(value))}
	t = append(t, name...)
	return append(t, value...)
}
