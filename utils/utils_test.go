package utils

import (
	"errors"
	"reflect"
	"testing"
)

// ================== Max/Min ==================

func TestMax(t *testing.T) {
	cases := []struct {
		a, b, want int
	}{
		{1, 2, 2},
		{-1, -2, -1},
		{0, 0, 0},
	}
	for _, c := range cases {
		if got := Max(c.a, c.b); got != c.want {
			t.Fatalf("Max(%d,%d)=%d, want %d", c.a, c.b, got, c.want)
		}
	}
	if got := Max(uint16(3), uint16(7)); got != 7 {
		t.Fatalf("Max(uint16)=%d", got)
	}
	if got := Max(float32(1.1), float32(1.2)); got != float32(1.2) {
		t.Fatalf("Max(float32)=%v", got)
	}
}

func TestMin(t *testing.T) {
	if got := Min(int8(-3), int8(7)); got != -3 {
		t.Fatalf("Min(int8)=%d", got)
	}
	if got := Min(uintptr(10), uintptr(2)); got != 2 {
		t.Fatalf("Min(uintptr)=%d", got)
	}
	if got := Min(3.5, 2.5); got != 2.5 {
		t.Fatalf("Min(float64)=%v", got)
	}
}

// ================== AnyToBytes ==================

func TestAnyToBytes_Nil(t *testing.T) {
	b, err := AnyToBytes(nil)
	if err != nil || b != nil {
		t.Fatalf("nil => %v %v", b, err)
	}
	var p *int
	b, err = AnyToBytes(p)
	if err != nil || b != nil {
		t.Fatalf("nil ptr => %v %v", b, err)
	}
}

func TestAnyToBytes_PtrDeref(t *testing.T) {
	n := 42
	b, err := AnyToBytes(&n)
	if err != nil || string(b) != "42" {
		t.Fatalf("*int => %q %v", b, err)
	}
	s := "hi"
	b, err = AnyToBytes(&s)
	if err != nil || string(b) != "hi" {
		t.Fatalf("*string => %q %v", b, err)
	}
}

func TestAnyToBytes_CopySlice(t *testing.T) {
	src := []byte("abc")
	b, err := AnyToBytes(src)
	if err != nil {
		t.Fatal(err)
	}
	b[0] = 'X'
	if string(src) != "abc" {
		t.Fatalf("AnyToBytes leaked the input slice: src=%q", src)
	}
}

func TestAnyToBytes_Primitives(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{"abc", "abc"},
		{int(-7), "-7"},
		{int8(-8), "-8"},
		{int16(16), "16"},
		{int32(32), "32"},
		{int64(-64), "-64"},
		{uint(1), "1"},
		{uint8(8), "8"},
		{uint16(160), "160"},
		{uint32(320), "320"},
		{uint64(640), "640"},
		{uintptr(9), "9"},
		{float32(1.5), "1.5"},
		{float64(-2.25), "-2.25"},
		{complex64(1 + 2i), "(1+2i)"},
		{complex128(3 + 4i), "(3+4i)"},
		{true, "true"},
		{false, "false"},
	}
	for _, c := range cases {
		b, err := AnyToBytes(c.v)
		if err != nil {
			t.Fatalf("%T %v err: %v", c.v, c.v, err)
		}
		if string(b) != c.want {
			t.Fatalf("%T %v => %q, want %q", c.v, c.v, b, c.want)
		}
	}
}

func TestAnyToBytes_Error(t *testing.T) {
	b, err := AnyToBytes(errors.New("boom"))
	if err != nil || string(b) != "boom" {
		t.Fatalf("error => %q %v", b, err)
	}
}

func TestAnyToBytes_StructFallback(t *testing.T) {
	type A struct {
		X int `json:"x"`
	}
	b, err := AnyToBytes(A{X: 3})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"x":3}` {
		t.Fatalf("struct => %q", b)
	}
}

// ================== AnyToStr ==================

func TestAnyToStr_Nil(t *testing.T) {
	s, err := AnyToStr(nil)
	if err != nil || s != "" {
		t.Fatalf("nil => %q %v", s, err)
	}
	var p *string
	s, err = AnyToStr(p)
	if err != nil || s != "" {
		t.Fatalf("nil ptr => %q %v", s, err)
	}
}

func TestAnyToStr_Direct(t *testing.T) {
	s, err := AnyToStr("hello")
	if err != nil || s != "hello" {
		t.Fatalf("string => %q %v", s, err)
	}
	s, err = AnyToStr([]byte("bytes"))
	if err != nil || s != "bytes" {
		t.Fatalf("[]byte => %q %v", s, err)
	}
	s, err = AnyToStr(errors.New("x"))
	if err != nil || s != "x" {
		t.Fatalf("error => %q %v", s, err)
	}
}

func TestAnyToStr_SliceByteNoBase64(t *testing.T) {
	// 之前 AnyToStr 走 json.Marshal([]byte) 会转成 base64，
	// 这里验证新版直接 string(bytes)
	in := []byte{0x41, 0x42, 0x43} // "ABC"
	s, err := AnyToStr(in)
	if err != nil {
		t.Fatal(err)
	}
	if s != "ABC" {
		t.Fatalf("[]byte got %q, want ABC; if it is base64 the code path is wrong", s)
	}
	// reflect 路径也能命中：把它包装成 any 再转
	var wrap any = in
	s2, err := AnyToStr(wrap)
	if err != nil || s2 != "ABC" {
		t.Fatalf("wrap([]byte) => %q %v", s2, err)
	}
}

func TestAnyToStr_Primitives(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{int(-1), "-1"},
		{int32(32), "32"},
		{uint8(255), "255"},
		{uint64(1000), "1000"},
		{float32(0.5), "0.5"},
		{float64(3.14), "3.14"},
		{complex128(1 - 1i), "(1+-1i)"},
		{true, "true"},
	}
	for _, c := range cases {
		s, err := AnyToStr(c.v)
		if err != nil {
			t.Fatalf("%T %v err: %v", c.v, c.v, err)
		}
		if s != c.want {
			t.Fatalf("%T %v => %q, want %q", c.v, c.v, s, c.want)
		}
	}
}

func TestAnyToStr_PtrDeref(t *testing.T) {
	n := 99
	s, err := AnyToStr(&n)
	if err != nil || s != "99" {
		t.Fatalf("*int => %q %v", s, err)
	}
}

func TestAnyToStr_MapStructSliceJSON(t *testing.T) {
	s, err := AnyToStr(map[string]int{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	// json 顺序不稳定，反解比较
	var m map[string]int
	_ = Deserialize([]byte(s), &m)
	if !reflect.DeepEqual(m, map[string]int{"a": 1}) {
		t.Fatalf("map => %q", s)
	}
	type X struct{ N int }
	s, err = AnyToStr(X{N: 2})
	if err != nil || s != `{"N":2}` {
		t.Fatalf("struct => %q %v", s, err)
	}
	arr := [3]int{1, 2, 3}
	s, err = AnyToStr(arr)
	if err != nil || s != `[1,2,3]` {
		t.Fatalf("array => %q %v", s, err)
	}
}

func TestAnyToStr_UnknownKind(t *testing.T) {
	fn := func() {}
	_, err := AnyToStr(fn)
	if err == nil {
		t.Fatal("func should error")
	}
	// error msg 里应该带 Kind 信息（我们加的）
	if !containsAny(err.Error(), "kind=", "Func") {
		t.Fatalf("err should include kind info, got %q", err.Error())
	}
}

// ================== Must helpers ==================

func TestMustAnyToBytes(t *testing.T) {
	if string(MustAnyToBytes("x")) != "x" {
		t.FailNow()
	}
	if MustAnyToBytes(nil) != nil {
		t.FailNow()
	}
}

func TestMustAnyToStr(t *testing.T) {
	if MustAnyToStr(123) != "123" {
		t.FailNow()
	}
	if MustAnyToStr(nil) != "" {
		t.FailNow()
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
