package main

import (
	"fmt"
	"reflect"
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
}

func main() {
	t := A{
		Name: "Leo",
		Type: "裹紧同在",
		Time: uint32(time.Now().Unix()),
	}
	v := reflect.ValueOf(t)
	ty := v.Type()
	fs := []byte{}
	for num := 0; num < v.NumField(); num++ {
		f := v.Field(num)
		tyf := ty.Field(num)
		val := tlv.Serialize(f.Interface())
		nam, err := tlv.Encode(0x3E, []byte(tyf.Name))
		if err != nil {
			fmt.Println(err)
		}
		feild := get_tlv_struct_feild(nam, val)
		fs = append(fs, feild...)
	}
	fs, err := tlv.Encode(0x3F, fs)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(fs)
}

var ma = []string{"uint8", "[]uint8"}
