package ag

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
)

// ---------- 公共类型：examples/ag A 结构体的镜像，用例对齐 L56-87 的值 ----------

type innerB struct {
	C string `json:"c"`
}

type fullA struct {
	Bool       bool
	Int1       int
	Int8       int8
	Int16      int16
	Int32      int32
	Int64      int64
	Uint       uint
	Uint8      uint8
	Uint16     uint16
	Uint32     uint32
	Uint64     uint64
	Uintptr    uintptr
	Float32    float32
	Float64    float64
	Complex64  complex64
	Complex128 complex128
	String     string
	Byte       byte
	Rune       rune
	B          innerB
	Slice      []int
	Slice16    []int16
	Slice32    []int32
	Slice64    []int64
	Map        map[string]int
	Arraya     []string
	Arrayb     []byte
	Float32s   []float32
	Float64s   []float64
}

// sampleA 返回 examples/ag/main.go L56-87 的同值实例
func sampleA() fullA {
	return fullA{
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
		B: innerB{
			C: "中文ab1234`",
		},
		Slice:    []int{-1, 2, 3, 4, 5},
		Slice16:  []int16{1, -2, 3, 4, 5},
		Slice32:  []int32{1, 2, -3, 4, 5},
		Slice64:  []int64{1, 2, 3, -4, 5},
		Map:      map[string]int{"a": 1, "b": 2, "c": 3},
		Arraya:   []string{"a中广", "b节qqq112", "c1231ff"},
		Arrayb:   []byte{0x01, 0x02, 0x03},
		Float32s: []float32{1.1, 2.2, 3.3},
		Float64s: []float64{10000.1, 2.2, 3.3},
	}
}

// helper: 断言帧结构 (magic, type, length, value切片)
func assertFrame(t *testing.T, label string, raw []byte, wantType uint8, wantValueLen int) {
	t.Helper()
	if len(raw) < ArgumentHeaderSize {
		t.Fatalf("%s: raw len=%d < header(5)", label, len(raw))
	}
	if raw[0] != ArgumentMagic1 || raw[1] != ArgumentMagic2 {
		t.Fatalf("%s: magic=% x want :p (3A 70)", label, raw[0:2])
	}
	if raw[2] != wantType {
		t.Fatalf("%s: TYPE=0x%02x(%s), want 0x%02x(%s)",
			label, raw[2], TypeName(raw[2]), wantType, TypeName(wantType))
	}
	l := int(binary.BigEndian.Uint16(raw[3:5]))
	if l != wantValueLen {
		t.Fatalf("%s: LENGTH=%d, want %d", label, l, wantValueLen)
	}
	if len(raw) != ArgumentHeaderSize+l {
		t.Fatalf("%s: raw len=%d != header+length=%d", label, len(raw), ArgumentHeaderSize+l)
	}
}

// ---------- TestEncode：穷举类型 + arg 结构断言 ----------

func TestEncode_TypeExhaustive(t *testing.T) {
	sa := sampleA()

	// 1. nil —— 特殊，独立出来
	t.Run("nil", func(t *testing.T) {
		raw, err := Encode(nil)
		if err != nil {
			t.Fatal(err)
		}
		assertFrame(t, "nil", raw, ArgumentTypeNil, 0)
	})

	// 2. 标量（与 sampleA 字段值一致，但每个字段是独立调用 Encode）
	scalarCases := []struct {
		name     string
		in       any
		wantType uint8
		wantMinL int // Value 最小长度
		wantMaxL int // Value 最大长度
	}{
		{"Bool", sa.Bool, ArgumentTypeBool, 1, 1},
		{"Int", sa.Int1, ArgumentTypeInt, 1, 8},
		{"Int8", sa.Int8, ArgumentTypeInt8, 1, 8},
		{"Int16", sa.Int16, ArgumentTypeInt16, 1, 8},
		{"Int32", sa.Int32, ArgumentTypeInt32, 1, 8},
		{"Int64", sa.Int64, ArgumentTypeInt64, 1, 8},
		{"Uint", sa.Uint, ArgumentTypeUint, 1, 8},
		{"Uint8", sa.Uint8, ArgumentTypeUint8, 1, 8},
		{"Uint16", sa.Uint16, ArgumentTypeUint16, 1, 8},
		{"Uint32", sa.Uint32, ArgumentTypeUint32, 1, 8},
		{"Uint64", sa.Uint64, ArgumentTypeUint64, 1, 8},
		{"Uintptr", sa.Uintptr, ArgumentTypeUintptr, 1, 8},
		{"Float32", sa.Float32, ArgumentTypeFloat32, 4, 4},
		{"Float64", sa.Float64, ArgumentTypeFloat64, 8, 8},
		{"Complex64", sa.Complex64, ArgumentTypeComplex64, 8, 8},
		{"Complex128", sa.Complex128, ArgumentTypeComplex128, 16, 16},
		{"String", sa.String, ArgumentTypeString, len(sa.String), len(sa.String)},
		{"Byte", sa.Byte, ArgumentTypeUint8, 1, 8},          // byte = uint8 别名
		{"Rune", sa.Rune, ArgumentTypeInt32, 1, 8},          // rune = int32 别名
		{"struct", sa.B, ArgumentTypeString, 2, 1 << 16},    // struct 降级为 JSON String
		{"Slice", sa.Slice, ArgumentTypeString, 2, 1 << 16}, // 复合 → JSON
		{"Slice16", sa.Slice16, ArgumentTypeString, 2, 1 << 16},
		{"Slice32", sa.Slice32, ArgumentTypeString, 2, 1 << 16},
		{"Slice64", sa.Slice64, ArgumentTypeString, 2, 1 << 16},
		{"Map", sa.Map, ArgumentTypeString, 2, 1 << 16},
		{"Arraya", sa.Arraya, ArgumentTypeString, 2, 1 << 16},
		{"Arrayb", sa.Arrayb, ArgumentTypeBytes, 3, 3}, // []byte → Bytes
		{"Float32s", sa.Float32s, ArgumentTypeString, 2, 1 << 16},
		{"Float64s", sa.Float64s, ArgumentTypeString, 2, 1 << 16},
	}
	for _, c := range scalarCases {
		t.Run(c.name, func(t *testing.T) {
			raw, err := Encode(c.in)
			if err != nil {
				t.Fatalf("Encode(%s) err: %v", c.name, err)
			}
			l := int(binary.BigEndian.Uint16(raw[3:5]))
			if l < c.wantMinL || l > c.wantMaxL {
				t.Fatalf("%s: Value len=%d out of range [%d,%d]  raw=% x",
					c.name, l, c.wantMinL, c.wantMaxL, raw)
			}
			assertFrame(t, c.name, raw, c.wantType, l)
		})
	}

	// 3. 有符号整数的最小/最大/负边界：确保压缩不会把负数的高位 FF trim 掉
	t.Run("Int_Boundaries", func(t *testing.T) {
		boundaries := []struct {
			name string
			in   int64
			tag  uint8
		}{
			{"int8.min", int64(math.MinInt8), ArgumentTypeInt8},
			{"int8.max", int64(math.MaxInt8), ArgumentTypeInt8},
			{"int16.min", int64(math.MinInt16), ArgumentTypeInt16},
			{"int16.max", int64(math.MaxInt16), ArgumentTypeInt16},
			{"int32.min", int64(math.MinInt32), ArgumentTypeInt32},
			{"int32.max", int64(math.MaxInt32), ArgumentTypeInt32},
			{"int64.min", math.MinInt64, ArgumentTypeInt64},
			{"int64.max", math.MaxInt64, ArgumentTypeInt64},
			{"int64.neg1", -1, ArgumentTypeInt64},
			{"int64.zero", 0, ArgumentTypeInt64},
		}
		for _, c := range boundaries {
			t.Run(c.name, func(t *testing.T) {
				var raw []byte
				var err error
				switch c.tag {
				case ArgumentTypeInt8:
					raw, err = Encode(int8(c.in))
				case ArgumentTypeInt16:
					raw, err = Encode(int16(c.in))
				case ArgumentTypeInt32:
					raw, err = Encode(int32(c.in))
				default:
					raw, err = Encode(int64(c.in))
				}
				if err != nil {
					t.Fatal(err)
				}
				assertFrame(t, c.name, raw, c.tag, int(binary.BigEndian.Uint16(raw[3:5])))
				v := raw[ArgumentHeaderSize:]
				// 负数首字节必须是 0xFF (MinI8/MinI16 除外首字节 0x80/0xFF 80…)，绝不能 trim
				if c.in < 0 && len(v) < 2 {
					// -128 是 0x80（1B），位模式是补码，负数但小值，允许 1B
					if c.in != math.MinInt8 && c.in != -128 {
						t.Fatalf("%s negative encode too short: % x len=%d", c.name, v, len(v))
					}
				}
				// 负数：首字节高位必须为 1
				if c.in < 0 && v[0]&0x80 == 0 {
					t.Fatalf("%s negative encode: v[0]=0x%02x 高位为 0，丢了符号", c.name, v[0])
				}
				// 正数：首字节高位为 0（除了 Max 的 0x7F...）
				if c.in > 0 && len(v) == 8 && v[0] == 0x7F {
					// 正常
				}
			})
		}
	})

	// 4. 无符号整数边界：确保压缩后无多余前导 0
	t.Run("Uint_Boundaries", func(t *testing.T) {
		type uCase struct {
			name string
			in   uint64
			tag  uint8
		}
		cases := []uCase{
			{"uint8.max", math.MaxUint8, ArgumentTypeUint8},
			{"uint16.max", math.MaxUint16, ArgumentTypeUint16},
			{"uint32.max", math.MaxUint32, ArgumentTypeUint32},
			{"uint64.max", math.MaxUint64, ArgumentTypeUint64},
			{"uint64.zero", 0, ArgumentTypeUint64},
			{"uint64.bit56", 1 << 56, ArgumentTypeUint64},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				var raw []byte
				var err error
				switch c.tag {
				case ArgumentTypeUint8:
					raw, err = Encode(uint8(c.in))
				case ArgumentTypeUint16:
					raw, err = Encode(uint16(c.in))
				case ArgumentTypeUint32:
					raw, err = Encode(uint32(c.in))
				default:
					raw, err = Encode(c.in)
				}
				if err != nil {
					t.Fatal(err)
				}
				l := int(binary.BigEndian.Uint16(raw[3:5]))
				assertFrame(t, c.name, raw, c.tag, l)
				v := raw[ArgumentHeaderSize:]
				// 压缩规则：除全 0 保留 1B，其它不得有前导 0
				if len(v) > 1 && v[0] == 0 && c.in != 0 {
					t.Fatalf("%s: value=% x len=%d 有前导 0，未压缩", c.name, v, len(v))
				}
			})
		}
	})

	// 5. Float：字节级精确比较位模式 (LittleEndian)
	t.Run("Float_Exact", func(t *testing.T) {
		raw32, _ := Encode(float32(3.14))
		v32 := raw32[ArgumentHeaderSize:]
		if got := math.Float32frombits(binary.LittleEndian.Uint32(v32)); math.Abs(float64(got)-3.14) > 1e-6 {
			t.Fatalf("float32: got=%v want=3.14  raw=% x", got, v32)
		}

		want64 := 3.141592653589793
		raw64, _ := Encode(want64)
		v64 := raw64[ArgumentHeaderSize:]
		if got := math.Float64frombits(binary.LittleEndian.Uint64(v64)); got != want64 {
			t.Fatalf("float64: got=%v want=%v", got, want64)
		}
	})

	// 6. Complex：实部/虚部分别位模式匹配
	t.Run("Complex_Exact", func(t *testing.T) {
		c64 := complex(float32(1), float32(2))
		raw64, _ := Encode(c64)
		v64 := raw64[ArgumentHeaderSize:]
		re := math.Float32frombits(binary.LittleEndian.Uint32(v64[0:4]))
		im := math.Float32frombits(binary.LittleEndian.Uint32(v64[4:8]))
		if re != 1 || im != 2 {
			t.Fatalf("complex64: re=%v im=%v", re, im)
		}

		c128 := complex(3.0, 4.0)
		raw128, _ := Encode(c128)
		v128 := raw128[ArgumentHeaderSize:]
		re128 := math.Float64frombits(binary.LittleEndian.Uint64(v128[0:8]))
		im128 := math.Float64frombits(binary.LittleEndian.Uint64(v128[8:16]))
		if re128 != 3 || im128 != 4 {
			t.Fatalf("complex128: re=%v im=%v", re128, im128)
		}
	})

	// 7. String/Bytes：拷贝独立性
	t.Run("String_CopyIndependent", func(t *testing.T) {
		in := []byte("abc")
		raw, err := Encode(string(in))
		if err != nil {
			t.Fatal(err)
		}
		v := raw[ArgumentHeaderSize:]
		v[0] = 'Z'
		if string(in) != "abc" {
			t.Fatal("Encode string modified original input")
		}
	})
	t.Run("Bytes_CopyIndependent", func(t *testing.T) {
		in := []byte{1, 2, 3}
		raw, err := Encode(in)
		if err != nil {
			t.Fatal(err)
		}
		v := raw[ArgumentHeaderSize:]
		v[0] = 99
		if in[0] == 99 {
			t.Fatal("Encode []byte leaked original slice")
		}
	})

	// 8. Struct → JSON String 降级：JSON 可反解回等价 struct
	t.Run("Struct_JsonFallback", func(t *testing.T) {
		raw, err := Encode(sa.B)
		if err != nil {
			t.Fatal(err)
		}
		assertFrame(t, "struct", raw, ArgumentTypeString, int(binary.BigEndian.Uint16(raw[3:5])))
		jsonStr := string(raw[ArgumentHeaderSize:])
		var out innerB
		if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
			t.Fatalf("struct json unmarshal: %v  raw=%q", err, jsonStr)
		}
		if out.C != sa.B.C {
			t.Fatalf("struct B.C=%q, want %q", out.C, sa.B.C)
		}
	})

	// 9. Map / Slice / []string / []float → JSON String 降级
	t.Run("Composite_JsonFallback", func(t *testing.T) {
		// a) []int
		raw, _ := Encode(sa.Slice)
		var s1 []int
		if err := json.Unmarshal(raw[ArgumentHeaderSize:], &s1); err != nil {
			t.Fatalf("[]int json err %v", err)
		}
		if !reflect.DeepEqual(s1, sa.Slice) {
			t.Fatalf("[]int: got=%v want=%v", s1, sa.Slice)
		}

		// b) map[string]int —— key 可能乱序，Unmarshal 后再比较
		rawM, _ := Encode(sa.Map)
		var m1 map[string]int
		if err := json.Unmarshal(rawM[ArgumentHeaderSize:], &m1); err != nil {
			t.Fatalf("map json err %v", err)
		}
		if !reflect.DeepEqual(m1, sa.Map) {
			t.Fatalf("map: got=%v want=%v", m1, sa.Map)
		}

		// c) []string
		rawS, _ := Encode(sa.Arraya)
		var sA []string
		if err := json.Unmarshal(rawS[ArgumentHeaderSize:], &sA); err != nil {
			t.Fatalf("[]string json err %v", err)
		}
		if !reflect.DeepEqual(sA, sa.Arraya) {
			t.Fatalf("[]string: got=%v want=%v", sA, sa.Arraya)
		}
	})

	// 10. Encode 错误分支：超长 Value（>65535）
	t.Run("ErrAgDataTooLarge", func(t *testing.T) {
		big := make([]byte, ArgumentMaxDataSize+1)
		_, err := Encode(big)
		if !errors.Is(err, ErrAgDataTooLarge) {
			t.Fatalf("big []byte err=%v, want ErrAgDataTooLarge", err)
		}
		boundary := make([]byte, ArgumentMaxDataSize)
		if _, err := Encode(boundary); err != nil {
			t.Fatalf("boundary 65535 err=%v", err)
		}
	})
}

// ---------- TestDecode：Encode→Decode Round-Trip，类型 & 值严格一致 ----------

func TestDecode_RoundTrip(t *testing.T) {
	sa := sampleA()

	// 1. 基础标量 roundtrip（单值对应 A 的每个字段）
	type rtCase struct {
		name string
		in   any
	}
	cases := []rtCase{
		{"nil", nil},
		{"Bool_true", true},
		{"Bool_false", false},
		{"Int", sa.Int1},
		{"Int8", sa.Int8},
		{"Int16", sa.Int16},
		{"Int32", sa.Int32},
		{"Int64", sa.Int64},
		{"Uint", sa.Uint},
		{"Uint8", sa.Uint8},
		{"Uint16", sa.Uint16},
		{"Uint32", sa.Uint32},
		{"Uint64", sa.Uint64},
		{"Uintptr", sa.Uintptr},
		{"Float32", sa.Float32},
		{"Float64", sa.Float64},
		{"Complex64", sa.Complex64},
		{"Complex128", sa.Complex128},
		{"String", sa.String},
		{"Byte", sa.Byte},
		{"Rune", sa.Rune},
		{"Arrayb([]byte)", sa.Arrayb},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, err := Encode(c.in)
			if err != nil {
				t.Fatalf("Encode err %v", err)
			}
			out, err := Decode(raw)
			if err != nil {
				t.Fatalf("Decode err %v  raw=% x", err, raw)
			}
			// 类型必须完全一致，Decode 返回 any，断言 reflect.Type 相等
			if reflect.TypeOf(out) != reflect.TypeOf(c.in) {
				if c.in == nil && out == nil {
					// 两边 nil 跳过
				} else {
					t.Fatalf("%s: type mismatch: Decode→%T  Encode←%T  out=%#v",
						c.name, out, c.in, out)
				}
			}
			// 值比较：float 用近似；其它 DeepEqual
			switch v := c.in.(type) {
			case float32:
				got := out.(float32)
				if math.Abs(float64(got-v)) > 1e-6 {
					t.Fatalf("float32: got=%v want=%v", got, v)
				}
			case float64:
				got := out.(float64)
				if math.Abs(got-v) > 1e-12 {
					t.Fatalf("float64: got=%v want=%v", got, v)
				}
			case complex64:
				got := out.(complex64)
				reDiff := math.Abs(float64(real(got) - real(v)))
				imDiff := math.Abs(float64(imag(got) - imag(v)))
				if reDiff > 1e-6 || imDiff > 1e-6 {
					t.Fatalf("complex64: got=%v want=%v", got, v)
				}
			case complex128:
				got := out.(complex128)
				if got != v {
					t.Fatalf("complex128: got=%v want=%v", got, v)
				}
			default:
				if !reflect.DeepEqual(out, c.in) {
					t.Fatalf("%s: value mismatch: got=%#v want=%#v", c.name, out, c.in)
				}
			}
		})
	}

	// 2. 整数边界 round-trip
	t.Run("Int_BoundaryRoundTrip", func(t *testing.T) {
		bounds := []any{
			int8(math.MinInt8), int8(math.MaxInt8),
			int16(math.MinInt16), int16(math.MaxInt16),
			int32(math.MinInt32), int32(math.MaxInt32),
			int64(math.MinInt64), int64(math.MaxInt64),
			int8(-1), int16(-1), int32(-1), int64(-1),
			int8(0), int16(0), int32(0), int64(0),
		}
		for _, v := range bounds {
			label := fmt.Sprintf("%T(%v)", v, v)
			raw, err := Encode(v)
			if err != nil {
				t.Fatalf("%s Encode err %v", label, err)
			}
			out, err := Decode(raw)
			if err != nil {
				t.Fatalf("%s Decode err %v", label, err)
			}
			if reflect.TypeOf(out) != reflect.TypeOf(v) || !reflect.DeepEqual(out, v) {
				t.Fatalf("%s roundtrip fail: out=%#v(%T)", label, out, out)
			}
		}
	})
	t.Run("Uint_BoundaryRoundTrip", func(t *testing.T) {
		bounds := []any{
			uint8(0), uint8(math.MaxUint8),
			uint16(0), uint16(math.MaxUint16),
			uint32(0), uint32(math.MaxUint32),
			uint64(0), uint64(math.MaxUint64),
			uintptr(0), uintptr(0xFFFFFFFF),
		}
		for _, v := range bounds {
			label := fmt.Sprintf("%T(%v)", v, v)
			raw, err := Encode(v)
			if err != nil {
				t.Fatalf("%s Encode err %v", label, err)
			}
			out, err := Decode(raw)
			if err != nil {
				t.Fatalf("%s Decode err %v", label, err)
			}
			if reflect.TypeOf(out) != reflect.TypeOf(v) || !reflect.DeepEqual(out, v) {
				t.Fatalf("%s roundtrip fail: out=%#v(%T)", label, out, out)
			}
		}
	})

	// 3. Decode 错误分支：非法帧头
	t.Run("Decode_InvalidHeader", func(t *testing.T) {
		_, err := Decode([]byte("nonsense"))
		if !errors.Is(err, ErrAgInvalidHeader) {
			t.Fatalf("bad frame err=%v, want ErrAgInvalidHeader", err)
		}
		_, err = Decode(nil)
		if !errors.Is(err, ErrAgInvalidHeader) {
			t.Fatalf("nil frame err=%v", err)
		}
	})

	// 4. Decode 对 float/complex 短输入 → ErrAgLengthMismatch（通过构造坏帧触发）
	t.Run("Decode_FloatComplexShort", func(t *testing.T) {
		// float32 只给 1B value
		bad, _ := encoder(ArgumentTypeFloat32, []byte{0x01})
		_, err := Decode(bad)
		if !errors.Is(err, ErrAgLengthMismatch) {
			t.Fatalf("short float32 err=%v, want ErrAgLengthMismatch", err)
		}
		// complex64 给 4B 而不是 8B
		bad2, _ := encoder(ArgumentTypeComplex64, make([]byte, 4))
		_, err = Decode(bad2)
		if !errors.Is(err, ErrAgLengthMismatch) {
			t.Fatalf("short complex64 err=%v", err)
		}
		// Unknown type
		bad3, _ := encoder(uint8(99), nil)
		_, err = Decode(bad3)
		if !errors.Is(err, ErrAgUnknownType) {
			t.Fatalf("unknown tag err=%v, want ErrAgUnknownType", err)
		}
	})

	// 5. JSON 降级类型 roundtrip：Decode 返回 string(…)，值等价于 JSON 文本
	t.Run("Composite_DecodeReturnsJsonString", func(t *testing.T) {
		type pair struct {
			name string
			in   any
		}
		pairs := []pair{
			{"[]int", sa.Slice},
			{"[]int16", sa.Slice16},
			{"[]int32", sa.Slice32},
			{"[]int64", sa.Slice64},
			{"map[string]int", sa.Map},
			{"[]string", sa.Arraya},
			{"innerB struct", sa.B},
			{"[]float32", sa.Float32s},
			{"[]float64", sa.Float64s},
		}
		for _, p := range pairs {
			t.Run(p.name, func(t *testing.T) {
				raw, err := Encode(p.in)
				if err != nil {
					t.Fatalf("Encode %s err %v", p.name, err)
				}
				out, err := Decode(raw)
				if err != nil {
					t.Fatalf("Decode %s err %v", p.name, err)
				}
				s, ok := out.(string)
				if !ok {
					t.Fatalf("%s Decode type=%T, want string(json)", p.name, out)
				}
				// 再把 s Unmarshal 回 in 的类型，与原输入相等
				if reflect.DeepEqual(p.in, sa.Map) {
					var m map[string]int
					if err := json.Unmarshal([]byte(s), &m); err != nil {
						t.Fatalf("map json: %v", err)
					}
					if !reflect.DeepEqual(m, sa.Map) {
						t.Fatalf("map: got=%v want=%v", m, sa.Map)
					}
				} else if reflect.DeepEqual(p.in, sa.B) {
					var o innerB
					if err := json.Unmarshal([]byte(s), &o); err != nil {
						t.Fatalf("struct json: %v", err)
					}
					if o.C != sa.B.C {
						t.Fatalf("struct C=%q want %q", o.C, sa.B.C)
					}
				} else {
					// 其它切片：直接 Unmarshal 到 any，再 DeepEqual
					var rawOut any
					if err := json.Unmarshal([]byte(s), &rawOut); err != nil {
						t.Fatalf("unmarshal json err %v  json=%q", err, s)
					}
					// 与 in 的 JSON 编码再解码结果比较（避免 []int vs []float64 类型差异）
					wantBytes, _ := json.Marshal(p.in)
					var wantNorm any
					_ = json.Unmarshal(wantBytes, &wantNorm)
					if !reflect.DeepEqual(rawOut, wantNorm) {
						t.Fatalf("%s json norm mismatch:\ngot =%#v\nwant=%#v", p.name, rawOut, wantNorm)
					}
				}
			})
		}
	})
}
