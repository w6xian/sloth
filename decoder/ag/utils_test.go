package ag

import (
	"fmt"
	"testing"
)

// ============== int_to_byte 单测（L18-23 简易压缩） ==============

func TestIntToByte_CompressedLen(t *testing.T) {
	cases := []struct {
		in   int64
		want int
	}{
		{0, 1},
		{1, 1},
		{255, 1},
		{256, 2},
		{1 << 15, 2},
		{1 << 16, 3},
		{1 << 23, 3},
		{1 << 24, 4},
		{1 << 31, 4},
		{1 << 32, 5},
		{1 << 39, 5},
		{1 << 40, 6},
		{1 << 47, 6},
		{1 << 48, 7},
		{1 << 55, 7},
		{1 << 56, 8},
		{-1, 8},
		{9223372036854775807, 8},
		{-9223372036854775808, 8},
	}
	for _, c := range cases {
		b := int_to_byte(c.in)
		if len(b) != c.want {
			t.Fatalf("int_to_byte(%d) len = %d (% x), want %d", c.in, len(b), b, c.want)
		}
	}
}

func TestIntToByte_Zero(t *testing.T) {
	b := int_to_byte(0)
	if len(b) != 1 || b[0] != 0 {
		t.Fatalf("zero encode = % x (len=%d), want [00] len=1", b, len(b))
	}
}

// 注释示例的三个显式字节级断言
func TestIntToByte_CommentExamples(t *testing.T) {
	// [0 0 0 0 0 0 128 0] -> [128,0]
	if b := int_to_byte(int64(128) << 8); len(b) != 2 || b[0] != 128 || b[1] != 0 {
		t.Fatalf("32768 encode = % x, want [80 00]", b)
	}
	// [0 0 0 0 0 0 1 1] -> [1,1]
	if b := int_to_byte(int64(1)<<8 | 1); len(b) != 2 || b[0] != 1 || b[1] != 1 {
		t.Fatalf("257 encode = % x, want [01 01]", b)
	}
	// [0 0 0 0 0 0 0 0] -> [0]
	if b := int_to_byte(0); len(b) != 1 || b[0] != 0 {
		t.Fatalf("0 encode = % x, want [00]", b)
	}
}

func TestIntToByte_Serialization(t *testing.T) {
	cases := []struct {
		name string
		in   int64
		want uint64
	}{
		{"max int64", 9223372036854775807, 9223372036854775807},
		{"min int64", -9223372036854775808, 0x8000000000000000},
		{"-1", -1, 0xFFFFFFFFFFFFFFFF},
		{"1", 1, 1},
		{"positive small", 256, 256},
		{"bit 32 boundary", 1 << 32, 1 << 32},
		{"negative small", -128, 0xFFFFFFFFFFFFFF80},
	}
	for _, c := range cases {
		b := int_to_byte(c.in)
		got := uint64(to_int64(b))
		if got != c.want {
			t.Fatalf("%s in=%d -> bytes=% x => uint64 = %d (0x%x), want %d (0x%x)",
				c.name, c.in, b, got, got, c.want, c.want)
		}
	}
}

// int_to_byte 产生的 bytes 字节序必须是 BigEndian：高位在前
func TestIntToByte_ByteOrder(t *testing.T) {
	// 0x0102030405060708 BigEndian 下最高字节 0x01 != 0，不触发压缩，输出 8B
	b := int_to_byte(0x0102030405060708)
	expected := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	if len(b) != 8 {
		t.Fatalf("byte order test len = %d, want 8  bytes=% x", len(b), b)
	}
	for i := range b {
		if b[i] != expected[i] {
			t.Fatalf("byte order wrong at %d: got 0x%02x, want 0x%02x  bytes=% x", i, b[i], expected[i], b)
		}
	}
}

// ============== to_int* 解码配套单测（含短字节输入） ==============

func TestToInt8(t *testing.T) {
	if got := to_int8([]byte{0x7F}); got != 127 {
		t.Fatalf("to_int8(0x7F) = %d", got)
	}
	if got := to_int8([]byte{0x80}); got != -128 {
		t.Fatalf("to_int8(0x80) = %d", got)
	}
	if got := to_int8([]byte{0xFF}); got != -1 {
		t.Fatalf("to_int8(0xFF) = %d", got)
	}
	// 短输入压缩：前面补 0（正数）
	if got := to_int8([]byte{0x00, 0x00, 0x7F}); got != 127 {
		t.Fatalf("to_int8([00 00 7F]) = %d, want 127", got)
	}
	// 短输入压缩：前面补 FF（负数符号扩展）
	if got := to_int8([]byte{0xFF, 0xFF}); got != -1 {
		t.Fatalf("to_int8([FF FF]) = %d, want -1", got)
	}
}

func TestToInt16(t *testing.T) {
	if got := to_int16([]byte{0x00, 0x01}); got != 1 {
		t.Fatalf("to_int16(BE 1) = %d", got)
	}
	if got := to_int16([]byte{0xFF, 0xFF}); got != -1 {
		t.Fatalf("to_int16(BE -1) = %d", got)
	}
	if got := to_int16([]byte{0x80, 0x00}); got != -32768 {
		t.Fatalf("to_int16(min int16) = %d", got)
	}
	// 压缩 1B：正数补 0，值 1
	if got := to_int16([]byte{0x01}); got != 1 {
		t.Fatalf("to_int16([01]) = %d, want 1", got)
	}
	// 压缩 1B：零扩展为 0x00FF = 255
	if got := to_int16([]byte{0xFF}); got != 255 {
		t.Fatalf("to_int16([FF]) = %d, want 255", got)
	}
	// 压缩 1B：0x80 → 零扩展 0x0080 = 128
	if got := to_int16([]byte{0x80}); got != 128 {
		t.Fatalf("to_int16([80]) = %d, want 128", got)
	}
}

func TestToInt32(t *testing.T) {
	if got := to_int32([]byte{0x00, 0x00, 0x00, 0x0A}); got != 10 {
		t.Fatalf("to_int32(10) = %d", got)
	}
	if got := to_int32([]byte{0xFF, 0xFF, 0xFF, 0xFF}); got != -1 {
		t.Fatalf("to_int32(-1) = %d", got)
	}
	if got := to_int32([]byte{0x80, 0x00, 0x00, 0x00}); got != -2147483648 {
		t.Fatalf("to_int32(min int32) = %d", got)
	}
	// 压缩 2B：[00 0A] → 10
	if got := to_int32([]byte{0x00, 0x0A}); got != 10 {
		t.Fatalf("to_int32([00 0A]) = %d, want 10", got)
	}
	// 压缩 1B：[FF] → 零扩展 0x000000FF = 255
	if got := to_int32([]byte{0xFF}); got != 255 {
		t.Fatalf("to_int32([FF]) = %d, want 255", got)
	}
}

func TestToInt64(t *testing.T) {
	if got := to_int64([]byte{0x7F, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}); got != 9223372036854775807 {
		t.Fatalf("to_int64(max int64) = %d", got)
	}
	if got := to_int64([]byte{0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}); got != -9223372036854775808 {
		t.Fatalf("to_int64(min int64) = %d", got)
	}
	if got := to_int64([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}); got != -1 {
		t.Fatalf("to_int64(-1) = %d", got)
	}
	// 压缩：正数 1，只输出 1B
	if got := to_int64([]byte{0x01}); got != 1 {
		t.Fatalf("to_int64([01]) = %d, want 1", got)
	}
	// 压缩：0x80 → 零扩展 0x0000000000000080 = 128
	if got := to_int64([]byte{0x80}); got != 128 {
		t.Fatalf("to_int64([80]) = %d, want 128", got)
	}
	// 压缩：[FF 01] → 零扩展 0x000000000000FF01 = 65281
	if got := to_int64([]byte{0xFF, 0x01}); got != 65281 {
		t.Fatalf("to_int64([FF 01]) = %d, want 65281", got)
	}
}

// ============== round-trip： int_to_byte -> to_int64 保持原值 ==============

func TestIntRoundTrip(t *testing.T) {
	values := []int64{
		0, 1, -1, 127, 128, -128,
		1 << 15, -1 << 15,
		1 << 31, -1 << 31,
		1 << 42, -1 << 42,
		9223372036854775807, -9223372036854775808,
		256, 257, 32768, // 注释示例覆盖
	}
	for _, v := range values {
		b := int_to_byte(v)
		fmt.Printf("v=%-22d encode=% x (len=%d)\n", v, b, len(b))
		if got := to_int64(b); got != v {
			t.Fatalf("roundtrip fail: in=%d  encode=% x  out=%d", v, b, got)
		}
	}
}
