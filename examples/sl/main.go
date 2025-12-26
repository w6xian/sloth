package main

import (
	"fmt"

	"github.com/w6xian/sloth/decoder/tlv"
)

type T struct {
	Tag   byte
	Value []byte
	A     A
}

type B struct {
	C string `tlv:"c"`
}

// A 结构体 包含golang所有基础数据类型
type A struct {
	// 布尔类型
	Bool bool `tlv:"bool"`

	// 整数类型
	Int     int     `tlv:"int"`
	Int8    int8    `tlv:"int8"`
	Int16   int16   `tlv:"int16"`
	Int32   int32   `tlv:"int32"`
	Int64   int64   `tlv:"int64"`
	Uint    uint    `tlv:"uint"`
	Uint8   uint8   `tlv:"uint8"`
	Uint16  uint16  `tlv:"uint16"`
	Uint32  uint32  `tlv:"uint32"`
	Uint64  uint64  `tlv:"uint64"`
	Uintptr uintptr `tlv:"uintptr"`

	// 浮点类型
	Float32 float32 `tlv:"float32"`
	Float64 float64 `tlv:"float64"`

	// 复数类型
	Complex64  complex64  `tlv:"complex64"`
	Complex128 complex128 `tlv:"complex128"`

	// 字符串类型
	String string `tlv:"string"`

	// 字节和字符类型
	Byte byte `tlv:"byte"`
	Rune rune `tlv:"rune"`
	B    B    `tlv:"b"`
}

func main() {
	t := A{
		Bool:       true,
		Int:        -42,
		Int8:       -8,
		Int16:      -16,
		Int32:      -32,
		Int64:      -64,
		Uint:       42,
		Uint8:      8,
		Uint16:     16,
		Uint32:     32,
		Uint64:     64,
		Uintptr:    100,
		Float32:    3.14,
		Float64:    3.141592653589793,
		Complex64:  complex(1, 2),
		Complex128: complex(3, 4),
		String:     "Hello, Go!",
		Byte:       'A',
		Rune:       '中',
		B: B{
			C: "中文ab1234`",
		},
	}

	tlv.NewOption(tlv.LengthSize(1, 4))
	fs, err := tlv.FromStruct(t)
	if err != nil {
		fmt.Println(err)
	}
	s, err := tlv.ToString(fs)
	if err != nil {
		fmt.Println(s, err)
	}
	fmt.Println(s)
}
