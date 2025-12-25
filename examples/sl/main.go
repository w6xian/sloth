package main

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/w6xian/sloth/decoder/tlv"
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
	Name string
	Type string
	Time uint32
	Long string
}

func main() {
	t := A{
		Name: "Leo",
		Type: "裹紧同在",
		Long: strings.Repeat("L刘贤", 100),
		Time: uint32(time.Now().Unix()),
	}
	tlv.NewOption(tlv.LengthSize(1, 3))
	v := reflect.ValueOf(t)
	ty := v.Type()
	fs := []byte{}
	for num := 0; num < v.NumField(); num++ {
		f := v.Field(num)
		tyf := ty.Field(num)
		frame := create_tlv_struct_feild(f, tyf)
		fs = append(fs, frame...)
	}
	fs, err := tlv.Encode(0x3F, fs)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(fs)
}

var ma = []string{"uint8", "[]uint8"}

func create_tlv_struct_feild(f reflect.Value, tyf reflect.StructField) []byte {
	val := tlv.Serialize(f.Interface())
	nam, err := tlv.Encode(0x3E, []byte(tyf.Name))
	if err != nil {
		fmt.Println(err)
	}
	return get_tlv_struct_feild(nam, val)
}
