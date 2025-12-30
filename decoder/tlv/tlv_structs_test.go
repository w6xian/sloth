package tlv

import (
	"encoding/json"
	"testing"
)

//go test -bench=. -benchmem -run=none

type B struct {
	C string `tlv:"c"`
}

type Strings struct {
	// Strs    []string `tlv:"strs"`
	// Byte    byte     `tlv:"byte"`
	// Slice16 []int16  `tlv:"slice16"`
	// Slice32 []int32  `tlv:"slice32"`
	// Slice64 []int64  `tlv:"slice64"`
	// Arraya  []string `tlv:"arraya"`
	// Arrayb  []byte   `tlv:"arrayb"`
	Slice []int `tlv:"slice"`
}

// func TestMarshalStrings(t *testing.T) {
// 	strs := Strings{
// 		// Strs:    []string{"abce罩", "bcde", "c北京时间"},
// 		// Byte:    0x41,
// 		// Slice16: []int16{1, -2, 3, 4, 5},
// 		Slice: []int{-1, 2, 3, 4, 5},
// 		// Slice32: []int32{1, 2, -3, 4, 5},
// 		// Slice64: []int64{1, 2, 3, -4, 5},
// 		// Arraya:  []string{"a", "b", "c"},
// 		// Arrayb:  []byte{0x01, 0x02, 0x03},
// 	}

// 	b, err := Marshal(strs)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	t.Log(b)
// 	str := Strings{}
// 	Unmarshal(b, &str)
// 	// fmt.Println(str.Strs)
// 	// fmt.Println(str.Byte)
// 	// fmt.Println(str.Slice16)
// 	fmt.Println("---")
// 	fmt.Println(str.Slice)
// 	// fmt.Println(str.Slice32)
// 	// fmt.Println(str.Slice64)
// 	// fmt.Println(str.Arraya)
// 	// fmt.Println(str.Arrayb)
// }

// A 结构体 包含golang所有基础数据类型
type A struct {
	// 布尔类型
	Bool bool `tlv:"bool"`

	// 整数类型
	Int1    int     `tlv:"int"`
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
	// Complex64  complex64  `tlv:"complex64"`
	// Complex128 complex128 `tlv:"complex128"`

	// 字符串类型
	String string `tlv:"string"`

	// 字节和字符类型
	Byte     byte           `tlv:"byte"`
	Rune     rune           `tlv:"rune"`
	B        B              `tlv:"b"`
	Slice    []int          `tlv:"slice"`
	Slice16  []int16        `tlv:"slice16"`
	Slice32  []int32        `tlv:"slice32"`
	Slice64  []int64        `tlv:"slice64"`
	Slicestr []string       `tlv:"slicestr"`
	Map      map[string]int `tlv:"map"`
	Arraya   []string       `tlv:"arraya"`
	Arrayb   []byte         `tlv:"arrayb"`
}

func NewMockA() *A {
	return &A{
		Bool:    true,
		Int1:    -42,
		Int8:    -8,
		Int16:   -16,
		Int32:   -32,
		Int64:   -64,
		Uint:    42,
		Uint8:   8,
		Uint16:  16,
		Uint32:  32,
		Uint64:  64,
		Uintptr: 100,
		Float32: 3.14,
		Float64: 3.141592653589793,
		// Complex64:  complex(1, 2),
		// Complex128: complex(3, 4),
		String: "Hello, Go!",
		Byte:   'A',
		Rune:   '中',
		B: B{
			C: "中文ab1234`",
		},
		Slice:    []int{-1, 2, 3, 4, 5},
		Slice16:  []int16{1, -2, 3, 4, 5},
		Slice32:  []int32{1, 2, -3, 4, 5},
		Slice64:  []int64{1, 2, 3, -4, 5},
		Slicestr: []string{"a", "b", "c"},
		Map:      map[string]int{"a": 1, "b": 2, "c": 3},
		Arraya:   []string{"a", "b", "c"},
		Arrayb:   []byte{0x01, 0x02, 0x03},
	}
}

func BenchmarkMarshal200(b *testing.B) {
	a := NewMockA()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame, err := Marshal(a)
		if err != nil {
			b.Fatal(err)
		}
		_ = frame
	}
	b.StopTimer()
}

func BenchmarkJson200(b *testing.B) {
	a := NewMockA()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		js, err := json.Marshal(a)
		if err != nil {
			b.Fatal(err)
		}
		_ = js
	}
	b.StopTimer()
}

// 极限
// BenchmarkMarshal200-6           18679048                64.35 ns/op            0 B/op          0 allocs/op
// go test -bench=. -benchmem -run=none
// 对比结果 2025/12/27(第一次尝试)
// goos: windows
// goarch: amd64
// pkg: github.com/w6xian/sloth/decoder/tlv
// cpu: Intel(R) Core(TM) i5-9400F CPU @ 2.90GHz
// BenchmarkMarshal200-6              94322             12607 ns/op            6778 B/op        308 allocs/op
// BenchmarkJson200-6                505194              2281 ns/op             648 B/op          8 allocs/op
//									执行总次数             每次执行耗时(ns)        内存分配次数        内存分配字节数
// 调优相关记录
// 第一次优化
// BenchmarkMarshal200-6              60546             20156 ns/op            1936 B/op        102 allocs/op
// BenchmarkJson200-6                515382              2293 ns/op             648 B/op          8 allocs/op
// 操作：加pool读取byte，
// 内存分配次数减少到1/3次, 内存分配字节数减少到1/3次.
// 执行耗时增加了1倍，总次数减少了1/3次.
// 时间换空间（失败的优化）
// BenchmarkMarshal200-6              91135             12639 ns/op            2161 B/op        110 allocs/op
// BenchmarkJson200-6                531483              2300 ns/op             648 B/op          8 allocs/op
//
// BenchmarkMarshal200-6              92630             12734 ns/op            4128 B/op        100 allocs/op
//
// BenchmarkMarshal200-6             108876             10809 ns/op            4259 B/op        117 allocs/op
// 用buffer优化后
// BenchmarkMarshal200-6             119476              9922 ns/op            4258 B/op        117 allocs/op
// 顺序写buffer后
// 对比结果 2025/12/27(第二次尝试)
// 执行性能提升了3/5（0.6倍）
// BenchmarkMarshal200-6             152913              7821 ns/op            2625 B/op         45 allocs/op
// BenchmarkJson200-6                527168              2286 ns/op             648 B/op          8 allocs/op
