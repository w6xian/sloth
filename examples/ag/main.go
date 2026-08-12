package main

import (
	"encoding/json"
	"fmt"

	"github.com/w6xian/sloth/v3/decoder/ag"
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
	BB         []byte         `tlv:"bb"`
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
	t2 := A{
		BB:         nil,
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
	u16, err := ag.Encode(t2.BB)
	fmt.Println(u16, err)
	v, err := ag.Decode(u16)
	fmt.Println("BB:", ag.Value(u16))
	fmt.Println(u16, ag.Data(u16), v, err)

}

func PrettyStruct(data interface{}) (string, error) {
	val, err := json.MarshalIndent(data, "", " ")
	if err != nil {
		return "", err
	}
	return string(val), nil
}
