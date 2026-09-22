package utils

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"testing"
)

// 本文件固化一条硬约束：手写编码器（AppendJSONStr / AppendJSONBytes）
// 必须与 encoding/json 的输出**字节级等价**。
//
// 编码端已替换为手写实现（热路径去反射），而解码端仍用 json.Unmarshal，
// 两边一旦漂移就是线上必然出现、但编译期发现不了的串包问题。

func TestAppendJSONStrEqualsStdlib(t *testing.T) {
	cases := []string{
		"",
		"plain",
		"你好世界",
		"🎉🚀 emoji",
		`quote"inside`,
		`back\slash`,
		"tab\there",
		"nl\nhere",
		"cr\rhere",
		"ctrl\x00\x01\x1f",
		"<script>alert('x')</script>",
		"a&b<c>d", // 标准库默认开启 HTML 转义
		`mixed "\" <>&` + "\n\t\r\x0b\x0c",
		string(bytes.Repeat([]byte("x"), 4096)),
	}
	for i, s := range cases {
		want, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("case %d: stdlib marshal: %v", i, err)
		}
		got := AppendJSONStr(nil, s)
		if !bytes.Equal(want, got) {
			t.Errorf("case %d (%q): mismatch\n want: %s\n  got: %s", i, s, want, got)
		}
		// 长度预估必须与实际编码长度一致（编码端用它做一次性分配）
		if n := JSONStrLen(s); n != len(got) {
			t.Errorf("case %d: JSONStrLen=%d, actual=%d", i, n, len(got))
		}
	}
}

// 逐个比对 0x00-0x7F 单字节字符串的编码结果，覆盖全部转义分支。
func TestAppendJSONStrAllSingleBytes(t *testing.T) {
	for c := 0; c < 0x80; c++ {
		s := string([]byte{byte(c)})
		want, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("byte %#02x: stdlib marshal: %v", c, err)
		}
		if got := AppendJSONStr(nil, s); !bytes.Equal(want, got) {
			t.Errorf("byte %#02x: want %s got %s", c, want, got)
		}
	}
}

// 随机串对照标准库（固定种子，失败可复现）。
func TestAppendJSONStrRandom(t *testing.T) {
	alphabet := []rune("ab\"\\\n\r\t\x00\x1f<>& /;你🎉Z")
	rng := rand.New(rand.NewSource(20240612))
	for i := 0; i < 3000; i++ {
		n := rng.Intn(24)
		sb := make([]rune, n)
		for j := range sb {
			sb[j] = alphabet[rng.Intn(len(alphabet))]
		}
		s := string(sb)
		want, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("stdlib marshal: %v", err)
		}
		if got := AppendJSONStr(nil, s); !bytes.Equal(want, got) {
			t.Fatalf("seed case %d (%q):\n want: %s\n  got: %s", i, s, want, got)
		}
	}
}

// 非法 UTF-8：标准库会替换成 U+FFFD，手写实现按原字节透传。
// 字节不同，但 json.Unmarshal 对两者解码结果一致，故只校验语义等价。
func TestAppendJSONStrInvalidUTF8Semantics(t *testing.T) {
	s := "\xff\xfe\x80"
	want, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	got := AppendJSONStr(nil, s)
	var a, b string
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatalf("decode fast: %v", err)
	}
	if err := json.Unmarshal(want, &b); err != nil {
		t.Fatalf("decode stdlib: %v", err)
	}
	if a != b {
		t.Fatalf("semantic mismatch: %q vs %q", a, b)
	}
}

// 追加语义：不能破坏已有内容，且可连续追加。
func TestAppendJSONStrAppendSemantics(t *testing.T) {
	buf := []byte("head:")
	buf = AppendJSONStr(buf, "a")
	buf = append(buf, ',')
	buf = AppendJSONStr(buf, "b")
	if string(buf) != `head:"a","b"` {
		t.Fatalf("unexpected: %s", buf)
	}
}

func TestAppendJSONBytesEqualsStdlib(t *testing.T) {
	cases := [][]byte{
		nil,
		{},
		[]byte("a"),
		[]byte("ab"),
		[]byte("abc"),
		{0x00, 0xff, 0x10, 0x7f},
		bytes.Repeat([]byte{0xAB}, 1024),
	}
	for i, b := range cases {
		want, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("case %d: stdlib marshal: %v", i, err)
		}
		got := AppendJSONBytes(nil, b)
		if !bytes.Equal(want, got) {
			t.Errorf("case %d: mismatch\n want: %s\n  got: %s", i, want, got)
		}
	}
}

// nil -> null、空 slice -> ""：与 json.Marshal 对 []byte 的语义一致
func TestAppendJSONBytesNilVsEmpty(t *testing.T) {
	if got := string(AppendJSONBytes(nil, nil)); got != "null" {
		t.Fatalf("nil should encode to null, got %s", got)
	}
	if got := string(AppendJSONBytes(nil, []byte{})); got != `""` {
		t.Fatalf("empty slice should encode to \"\", got %s", got)
	}
}

// 大 payload 走堆分配分支（超过栈缓冲 512B），结果仍须与标准库一致
func TestAppendJSONBytesLarge(t *testing.T) {
	b := make([]byte, 4096)
	for i := range b {
		b[i] = byte(i)
	}
	want, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := AppendJSONBytes(nil, b); !bytes.Equal(want, got) {
		t.Fatal("large payload mismatch")
	}
}

// ---------------------------------------------------------------------------
// Benchmark
// ---------------------------------------------------------------------------

func BenchmarkAppendJSONStr(b *testing.B) {
	s := `user"name\<>&` + "\n\t中文"
	b.Run("fast", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = AppendJSONStr(nil, s)
		}
	})
	b.Run("stdlib", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = json.Marshal(s)
		}
	})
}

func BenchmarkAppendJSONBytes(b *testing.B) {
	for _, size := range []int{16, 1024, 65536} {
		payload := bytes.Repeat([]byte{0x5A}, size)
		b.Run(byteSizeName(size), func(b *testing.B) {
			b.Run("fast", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					_ = AppendJSONBytes(nil, payload)
				}
			})
			b.Run("stdlib", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					_, _ = json.Marshal(payload)
				}
			})
		})
	}
}

func byteSizeName(n int) string {
	switch {
	case n >= 1024:
		return "KB" + itoa(n/1024)
	default:
		return "B" + itoa(n)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
