package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/w6xian/sloth/decoder/tlv"
	"github.com/w6xian/sloth/internal/utils"
)

type T struct {
	Tag   byte
	Value []byte
	A     A
	B     struct {
		C byte
	}
}

type A struct {
	Name string `tlv:"name"`
	Type string `tlv:"type"`
	Time uint32 `tlv:"time"`
	Long string `tlv:"long"`
}

func main() {
	t := A{
		Name: "Leo",
		Type: "裹紧同在",
		Time: uint32(time.Now().Unix()),
	}
	tlv.NewOption(tlv.LengthSize(1, 4))
	fs := create_tlv_struct(t)
	tt := &A{}
	err := read_tlv_struct(fs, tt)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(tt.Type)
	fmt.Println(tt.Time)
	fmt.Println(tt.Long)
	fmt.Println(tt.Name)
}

func read_tlv_struct(v []byte, s any) error {
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
	t, l, v, err := tlv.Next(v)
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
			nt, nl, nv, nerr := tlv.Next(fv)
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

			_, _, vv, verr := tlv.Next(fv[nl:])
			if verr != nil {
				return verr
			}
			isPtr := f.Kind() == reflect.Pointer
			// 设置值
			value := utils.GetType(isPtr, f.Type().Name(), vv)
			f.Set(value)

		}
		pos += fl
		l -= fl
	}
	return nil
}

func read_tlv_field(v []byte) (byte, int, []byte, error) {
	t, l, v, err := tlv.Next(v)
	if err != nil {
		return 0, 0, nil, err
	}
	return t, l, v, nil
}

func create_tlv_struct(t any) []byte {
	v := reflect.ValueOf(t)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	ty := v.Type()
	fs := []byte{}
	for num := 0; num < v.NumField(); num++ {
		f := v.Field(num)
		tyf := ty.Field(num)
		frame := create_tlv_struct_feild_v1(f, tyf)
		fs = append(fs, frame...)
	}
	fs, err := tlv.Encode(0x3F, fs)
	if err != nil {
		fmt.Println(err)
	}
	return fs
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
	tag, err := get_tlv_struct_feild_name(tyf)
	if err != nil {
		return []byte{}
	}
	nam, err := tlv.Encode(0x3E, tag)
	if err != nil {
		fmt.Println(err)
	}
	val := get_tlv_feild_value(f.Interface())
	if val == nil {
		return []byte{}
	}
	v, e := tlv.Encode(0x3D, val)
	if e != nil {
		fmt.Println(e)
	}
	return get_tlv_struct_feild(nam, v)
}

func create_tlv_struct_feild_v1(f reflect.Value, tyf reflect.StructField) []byte {
	tag, err := get_tlv_struct_feild_name(tyf)
	if err != nil {
		return []byte{}
	}
	nam, err := tlv.Encode(0x3D, tag)
	if err != nil {
		fmt.Println(err)
	}
	val := tlv.Serialize(f.Interface())
	if val == nil {
		val = tlv.EmptyFrame()
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
