package frame

import (
	"bytes"
	"encoding/json"
	"testing"
)

// 分片编码器必须与 encoding/json 等价：解码端 frame.FromType 仍走 json.Unmarshal。
func TestDataSliceBytesEqualsStdlib(t *testing.T) {
	cases := []DataSlice{
		{},
		{P: TextMessage, N: "01", T: 1, I: 0, S: 5, D: []byte("hello")},
		{P: BinaryMessage, N: "99", T: 3, I: 2, S: 4096, D: bytes.Repeat([]byte{0xAB}, 4096)},
		{P: TextMessage, N: "07", T: 1, I: 0, S: 0, D: []byte{}},       // 空数据 -> ""
		{P: TextMessage, N: `a"b`, T: 1, I: 0, S: 1, D: []byte("\n\t")}, // 需转义
		{P: TextMessage, N: "08", T: 1, I: 0, S: 1, D: nil},             // nil -> null
		{P: LongMessage | TextMessage, N: "09", T: 255, I: 254, S: 1 << 20, D: []byte("x")},
	}
	for i, c := range cases {
		want, err := json.Marshal(&c)
		if err != nil {
			t.Fatalf("case %d: stdlib marshal: %v", i, err)
		}
		got := c.Bytes()
		if !bytes.Equal(want, got) {
			t.Errorf("case %d: bytes mismatch\n want: %s\n  got: %s", i, want, got)
		}
		// 解码端回环
		back, err := FromType(got, TextMessage)
		if err != nil {
			t.Fatalf("case %d: FromType: %v", i, err)
		}
		if back.N != c.N || back.T != c.T || back.I != c.I || back.S != c.S {
			t.Errorf("case %d: roundtrip mismatch: %+v", i, back)
		}
		if !bytes.Equal(back.D, c.D) {
			t.Errorf("case %d: data mismatch", i)
		}
	}
}

// 手写编码器与 Bytes() 必须一致（slicesTextSend 单分片快路径用的是前者）
func TestAppendSliceJSONMatchesBytes(t *testing.T) {
	s := DataSlice{P: TextMessage, N: "11", T: 2, I: 1, S: 128, D: []byte("slice-payload")}
	if a, b := AppendSliceJSON(nil, s), s.Bytes(); !bytes.Equal(a, b) {
		t.Errorf("mismatch:\n append: %s\n bytes: %s", a, b)
	}
	// 追加到已有缓冲时不能破坏前缀
	buf := append([]byte("prefix:"), s.Bytes()...)
	if !bytes.HasPrefix(buf, []byte("prefix:{")) {
		t.Errorf("unexpected prefix: %s", buf)
	}
}

// 单分片快路径：栈上构造分片编码，结果须与堆上对象编码一致
func TestAppendSliceJSONStackValue(t *testing.T) {
	payload := []byte("single slice payload")
	stack := DataSlice{P: TextMessage, N: "01", T: 1, I: 0, S: uint32(len(payload)), D: payload}
	heapSlice := &DataSlice{P: TextMessage, N: "01", T: 1, I: 0, S: uint32(len(payload)), D: payload}
	if a, b := AppendSliceJSON(nil, stack), heapSlice.Bytes(); !bytes.Equal(a, b) {
		t.Errorf("mismatch:\n stack: %s\n heap: %s", a, b)
	}
}

// 分片 → 编码 → 解码 → 重组，必须还原原始数据。
func TestSplitRoundTrip(t *testing.T) {
	payloads := [][]byte{
		[]byte("short"),
		bytes.Repeat([]byte("a"), 1024),       // 恰好一片
		bytes.Repeat([]byte("b"), 1025),       // 多一片
		bytes.Repeat([]byte("c"), 64*1024),    // 64KB
		[]byte("中文分片测试\x00\xff\x01"), // 含多字节与非 ASCII 字节
	}
	for i, payload := range payloads {
		slices, err := Split("07", payload, 4096, TextMessage)
		if err != nil {
			t.Fatalf("case %d: Split: %v", i, err)
		}
		if len(slices) == 0 {
			t.Fatalf("case %d: no slices", i)
		}
		var (
			got  bytes.Buffer
			prev byte = 255
		)
		for j, s := range slices {
			if s.I != byte(j) {
				t.Fatalf("case %d: slice %d index=%d", i, j, s.I)
			}
			if s.I <= prev && j != 0 {
				t.Fatalf("case %d: index not increasing", i)
			}
			prev = s.I
			if s.T != byte(len(slices)) || s.S != uint32(len(payload)) || s.N != "07" {
				t.Fatalf("case %d: meta mismatch: %+v", i, s)
			}
			// 文本分片编码 → 解码
			back, err := FromType(s.Bytes(), TextMessage)
			if err != nil {
				t.Fatalf("case %d: FromType: %v", i, err)
			}
			got.Write(back.D)
		}
		if !bytes.Equal(got.Bytes(), payload) {
			t.Fatalf("case %d: reassembled %d bytes, want %d bytes", i, got.Len(), len(payload))
		}
	}
}

// sliceSize 会被夹到 [1024, 65535]
func TestSplitSizeClamp(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 4096)
	if slices, _ := Split("01", payload, 1, TextMessage); len(slices) != 4 {
		t.Fatalf("sliceSize=1 should clamp to 1024 -> 4 slices, got %d", len(slices))
	}
	if slices, _ := Split("01", payload, 1<<20, TextMessage); len(slices) != 1 {
		t.Fatalf("sliceSize=1MB should clamp to 65535 -> 1 slice, got %d", len(slices))
	}
	if slices, _ := Split("01", payload, 4096, TextMessage); len(slices) != 1 {
		t.Fatalf("sliceSize=4096 -> 1 slice, got %d", len(slices))
	}
}

// 二进制帧编解码回环（含 CRC 选项）
func TestEncodeDecodeRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		s    *DataSlice
		opts []FrameOption
	}{
		{"short", &DataSlice{P: TextMessage, N: "ab", T: 3, I: 1, S: 5, D: []byte("hello")}, nil},
		{"empty", &DataSlice{P: TextMessage, N: "ab", T: 1, I: 0, S: 0, D: []byte{}}, nil},
		{"crc_opt", &DataSlice{P: BinaryMessage, N: "ab", T: 1, I: 0, S: 5, D: []byte("hello")}, []FrameOption{CheckCRC()}},
		{"crc_flag", &DataSlice{P: BinaryMessage | CRC, N: "ab", T: 1, I: 0, S: 5, D: []byte("hello")}, nil},
		{"long", &DataSlice{P: BinaryMessage, N: "AB", T: 1, I: 0, S: 0x10000, D: bytes.Repeat([]byte{0x7E}, 0x10000)}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := Encode(c.s, c.opts...)
			back, err := Decode(raw)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if back.N != c.s.N || back.T != c.s.T || back.I != c.s.I {
				t.Fatalf("meta mismatch: %+v", back)
			}
			if !bytes.Equal(back.D, c.s.D) {
				t.Fatalf("data mismatch: got %d bytes want %d bytes", len(back.D), len(c.s.D))
			}
		})
	}
}

func TestFromTypeErrors(t *testing.T) {
	if _, err := FromType([]byte(`{"p":1}`), 0x7F); err == nil {
		t.Fatal("invalid message type should error")
	}
	if _, err := FromType([]byte(`not json`), TextMessage); err == nil {
		t.Fatal("malformed json should error")
	}
	if _, err := FromType([]byte{0x01, 0x02}, BinaryMessage); err == nil {
		t.Fatal("too short binary frame should error")
	}
}

// ---------------------------------------------------------------------------
// Benchmark
// ---------------------------------------------------------------------------

func BenchmarkDataSliceBytes(b *testing.B) {
	s := DataSlice{P: TextMessage, N: "01", T: 1, I: 0, S: 1024, D: bytes.Repeat([]byte("x"), 1024)}
	b.Run("fast", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = s.Bytes()
		}
	})
	b.Run("stdlib", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = json.Marshal(&s)
		}
	})
}

// 单分片快路径（append 到复用缓冲，无中间对象）
func BenchmarkSingleSliceAppend(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 1024)
	s := DataSlice{P: TextMessage, N: "01", T: 1, I: 0, S: uint32(len(payload)), D: payload}
	b.Run("fast", func(b *testing.B) {
		b.ReportAllocs()
		buf := make([]byte, 0, 2048)
		for i := 0; i < b.N; i++ {
			buf = AppendSliceJSON(buf[:0], s)
		}
	})
	b.Run("stdlib", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = json.Marshal(&s)
		}
	})
}

func BenchmarkSplit(b *testing.B) {
	payload := bytes.Repeat([]byte("0123456789abcdef"), 64*1024) // 1MB
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Split("01", payload, 4096, TextMessage); err != nil {
			b.Fatal(err)
		}
	}
}

// 分片 → 编码 → 解码 → 重组 全链路
func BenchmarkSplitEncodeDecodeReassemble(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 64*1024)
	b.ReportAllocs()
	var out bytes.Buffer
	for i := 0; i < b.N; i++ {
		slices, _ := Split("01", payload, 8192, TextMessage)
		out.Reset()
		for _, s := range slices {
			back, err := FromType(s.Bytes(), TextMessage)
			if err != nil {
				b.Fatal(err)
			}
			out.Write(back.D)
		}
		if out.Len() != len(payload) {
			b.Fatalf("reassembled %d want %d", out.Len(), len(payload))
		}
	}
}

func BenchmarkEncodeDecodeBinary(b *testing.B) {
	s := &DataSlice{P: BinaryMessage, N: "AB", T: 1, I: 0, S: 1024, D: bytes.Repeat([]byte{0x5A}, 1024)}
	b.Run("encode", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = Encode(s)
		}
	})
	raw := Encode(s)
	b.Run("decode", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := Decode(raw); err != nil {
				b.Fatal(err)
			}
		}
	})
}
