package main

import (
	"fmt"
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
	Name string `tlv:"name"`
	Type string `tlv:"type"`
	Time uint32 `tlv:"time"`
	Long string `tlv:"long"`
	B    B      `tlv:"b"`
}
type B struct {
	Info string `tlv:"info"`
	C    C      `tlv:"c"`
}

type C struct {
	Id int `tlv:"id"`
}

func main() {
	t := A{
		Name: "Leo",
		Type: "裹紧同在",
		Time: uint32(time.Now().Unix()),
		Long: "12345678901234567890",
		B: B{
			Info: "hello",
			C: C{
				Id: 1,
			},
		},
	}
	tlv.NewOption(tlv.LengthSize(1, 4))
	fs, err := tlv.FromStruct(t)
	if err != nil {
		fmt.Println(err)
	}
	tt := &A{}
	err = tlv.ToStruct(fs, tt)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(tt.Type)
	fmt.Println(tt.Time)
	fmt.Println(tt.Long)
	fmt.Println(tt.Name)
	fmt.Println(tt.B.Info)
	fmt.Println(tt.B.C.Id)
}
