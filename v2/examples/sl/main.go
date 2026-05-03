package main

import (
	"encoding/json"
	"fmt"

	"github.com/w6xian/tlv"
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
	Bool       bool           `tlv:"bool"`
	Int1       int            `tlv:"int"`
	Int8       int8           `tlv:"int8"`
	Int16      int16          `tlv:"int16"`
	Int32      int32          `tlv:"int32"`
	Int64      int64          `tlv:"int64"`
	Uint       uint           `tlv:"uint"`
	Uint8      uint8          `tlv:"uint8"`
	Uint16     uint16         `tlv:"uint16"`
	Uint32     uint32         `tlv:"uint32"`
	Uint64     uint64         `tlv:"uint64"`
	Uintptr    uintptr        `tlv:"uintptr"`
	Float32    float32        `tlv:"float32"`
	Float64    float64        `tlv:"float64"`
	Complex64  complex64      `tlv:"complex64"`
	Complex128 complex128     `tlv:"complex128"`
	String     string         `tlv:"string"`
	Byte       byte           `tlv:"byte"`
	Rune       rune           `tlv:"rune"`
	B          B              `tlv:"b"`
	Slice      []int          `tlv:"slice"`
	Slice16    []int16        `tlv:"slice16"`
	Slice32    []int32        `tlv:"slice32"`
	Slice64    []int64        `tlv:"slice64"`
	Map        map[string]int `tlv:"map"`
	Arraya     []string       `tlv:"arraya"`
	Arrayb     []byte         `tlv:"arrayb"`
	Float32s   []float32      `tlv:"float32s"`
	Float64s   []float64      `tlv:"float64s"`
}

func main() {
	t := A{
		Bool:       true,
		Int1:       -42,
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
		Slice:   []int{-1, 2, 3, 4, 5},
		Slice16: []int16{1, -2, 3, 4, 5},
		Slice32: []int32{1, 2, -3, 4, 5},
		Slice64: []int64{1, 2, 3, -4, 5},
		// Slicestr: []string{"a", "b", "c"},
		Map:      map[string]int{"a": 1, "b": 2, "c": 3},
		Arraya:   []string{"a中广", "b节qqq112", "c1231ff"},
		Arrayb:   []byte{0x01, 0x02, 0x03},
		Float32s: []float32{1.1, 2.2, 3.3},
		Float64s: []float64{10000.1, 2.2, 3.3},
	}

	// js, err := json.Marshal(t)
	// if err != nil {
	// 	fmt.Println(err)
	// }
	// fmt.Println(string(js))

	// tlv.NewOption(tlv.LengthSize(1, 4))
	fs, err := tlv.Marshal(t)
	if err != nil {
		fmt.Println(err)
	}

	s, err := tlv.ToString(fs)
	if err != nil {
		fmt.Println(s, err)
	}
	var t2 A
	err = tlv.Unmarshal(fs, &t2)
	if err != nil {
		fmt.Println("---", err)
	}
	fmt.Println("--------")
	fmt.Println("Bool", t2.Bool, false, t2.Bool == false)
	fmt.Println("Int1", t2.Int1, -42, t2.Int1 == -42)
	fmt.Println("Int8", t2.Int8, -8, t2.Int8 == -8)
	fmt.Println("Int16", t2.Int16, -16, t2.Int16 == -16)
	fmt.Println("Int32", t2.Int32, -32, t2.Int32 == -32)
	fmt.Println("Int64", t2.Int64, -64, t2.Int64 == -64)
	fmt.Println("Uint", t2.Uint, 42, t2.Uint == 42)
	fmt.Println("Uint8", t2.Uint8, 8, t2.Uint8 == 8)
	fmt.Println("Uint16", t2.Uint16, 16, t2.Uint16 == 16)
	fmt.Println("Uint32", t2.Uint32, 32, t2.Uint32 == 32)
	fmt.Println("Uint64", t2.Uint64, 64, t2.Uint64 == 64)
	fmt.Println("Uintptr", t2.Uintptr, 100, t2.Uintptr == 100)
	fmt.Println("Float32", t2.Float32, 3.14, t2.Float32 == 3.14)
	fmt.Println("Float64", t2.Float64, 3.141592653589793, t2.Float64 == 3.141592653589793)
	fmt.Println("String", t2.String, "Hello, Go!", t2.String == "Hello, Go!")
	fmt.Println("Byte", t2.Byte, 'A', t2.Byte == 'A')
	fmt.Println("Rune", t2.Rune, '中', t2.Rune == '中')
	fmt.Println("B", t2.B.C, "中文ab1234`", t2.B.C == "中文ab1234`")
	fmt.Println("Slice", t2.Slice, []int{-1, 2, 3, 4, 5})

	fmt.Println("Slice", t2.Slice, []int{-1, 2, 3, 4, 5})
	fmt.Println("Slice16", t2.Slice16, []int16{1, -2, 3, 4, 5})
	fmt.Println("Slice32", t2.Slice32, []int32{1, 2, -3, 4, 5})
	fmt.Println("Slice64", t2.Slice64, []int64{1, 2, 3, -4, 5})
	fmt.Println("Map", t2.Map, map[string]int{"a": 1, "b": 2, "c": 3})
	fmt.Println("Arraya", t2.Arraya, []string{"a中广", "b节qqq112", "c1231ff"})
	fmt.Println("Arrayb", t2.Arrayb, []byte{0x01, 0x02, 0x03})
	// fmt.Println("Float32s", t2.Float32s, []float32{1.1, 2.2, 3.3})
	fmt.Println("Float64s", t2.Float64s, []float64{10000.1, 2.2, 3.3})
	// fmt.Println(s)
	fmt.Println("--------")
}

func PrettyStruct(data interface{}) (string, error) {
	val, err := json.MarshalIndent(data, "", " ")
	if err != nil {
		return "", err
	}
	return string(val), nil
}
