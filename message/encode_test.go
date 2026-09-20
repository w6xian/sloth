package message

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
)

// 手写编码器必须与 encoding/json 输出等价：
// 编码端已经换成 AppendJSON/MarshalJSONFast，而解码端（服务端/客户端）仍用 json.Unmarshal，
// 两边一旦不一致就是线上必然出现、但本地编译期发现不了的串包问题，故用测试固化。

func TestJsonCallObjectEncodeEqualsStdlib(t *testing.T) {
	cases := []*JsonCallObject{
		{}, // 全空
		{Method: "Ping"},
		{Method: "Ping", Header: Header{"trace": "abc"}},
		{Method: "Login", Header: Header{"a": "1", "b": "2"}}, // 多 key：map 顺序随机，仅比对语义
		{Method: "Echo", Args: [][]byte{[]byte("hello"), nil, {}}},
		{Method: "Echo", Args: [][]byte{[]byte("x")}, Data: []byte("payload")},
		{Method: "Fail", Error: "boom"},
		{Method: `特殊"字符\与
换行`, Header: Header{"k": "值\ttab"}},
		{Method: "Full", Header: Header{"h": "v"}, Args: [][]byte{[]byte("a")}, Data: []byte("d"), Error: "e"},
		// 边界：nil 与空 slice 的 JSON 语义不同（null vs ""）
		{Method: "Nil", Args: nil, Data: nil},
		{Method: "Empty", Args: [][]byte{}, Data: []byte{}},
	}
	for i, c := range cases {
		want, err := json.Marshal(c)
		if err != nil {
			t.Fatalf("case %d: stdlib marshal: %v", i, err)
		}
		got := c.MarshalJSONFast()

		// 字段顺序与 stdlib 一致；Header 只有一个 key 时 map 顺序无歧义，可直接比字节
		if len(c.Header) <= 1 && !bytes.Equal(want, got) {
			t.Errorf("case %d: bytes mismatch\n want: %s\n  got: %s", i, want, got)
		}

		// 语义等价：解回结构体后必须完全一致
		var a, b JsonCallObject
		if err := json.Unmarshal(got, &a); err != nil {
			t.Fatalf("case %d: unmarshal fast: %v (%s)", i, err, got)
		}
		if err := json.Unmarshal(want, &b); err != nil {
			t.Fatalf("case %d: unmarshal stdlib: %v", i, err)
		}
		if !reflect.DeepEqual(normCall(a), normCall(b)) {
			t.Errorf("case %d: semantic mismatch\n want: %+v\n  got: %+v", i, b, a)
		}
	}
}

// 大 payload（触发 base64 与长度预估的堆分配分支）仍须与标准库字节一致。
func TestJsonCallObjectEncodeLarge(t *testing.T) {
	big := bytes.Repeat([]byte{0x5A}, 64*1024)
	c := &JsonCallObject{
		Method: "v1.Big",
		Args:   [][]byte{big, big[:1]},
		Data:   big[:1024],
	}
	want, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.MarshalJSONFast(); !bytes.Equal(want, got) {
		t.Fatal("large payload bytes mismatch")
	}
	// 解码回环
	var back JsonCallObject
	if err := json.Unmarshal(c.MarshalJSONFast(), &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !bytes.Equal(back.Args[0], big) || !bytes.Equal(back.Data, big[:1024]) {
		t.Fatal("large payload roundtrip mismatch")
	}
}

// 并发编码（配合 -race 验证无共享可变状态）。
func TestJsonCallObjectEncodeConcurrent(t *testing.T) {
	c := &JsonCallObject{
		Method: "v1.Echo",
		Header: Header{"trace": "abc", "uid": "1"},
		Args:   [][]byte{[]byte("payload")},
		Data:   []byte("data"),
	}
	want := c.MarshalJSONFast()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				if got := c.MarshalJSONFast(); len(got) != len(want) {
					t.Errorf("length mismatch: %d vs %d", len(got), len(want))
					return
				}
			}
		}()
	}
	wg.Wait()
}

// AppendJSON 追加到已有缓冲：不得破坏前缀，且可连续追加。
func TestJsonCallObjectAppendJSON(t *testing.T) {
	c := &JsonCallObject{Method: "m", Args: [][]byte{[]byte("a")}}
	buf := []byte("head:")
	buf = c.AppendJSON(buf)
	buf = append(buf, ' ') // 顶层两个 JSON 值之间用空白分隔（json.Decoder 不接受逗号）
	buf = (&Msg{Type: 1, Body: []byte("b")}).AppendJSON(buf)
	var v1 JsonCallObject
	var v2 Msg
	dec := json.NewDecoder(bytes.NewReader(buf[5:]))
	if err := dec.Decode(&v1); err != nil {
		t.Fatalf("decode call: %v", err)
	}
	if err := dec.Decode(&v2); err != nil {
		t.Fatalf("decode msg: %v", err)
	}
	if v1.Method != "m" || len(v1.Args) != 1 || string(v1.Args[0]) != "a" {
		t.Fatalf("unexpected call: %+v", v1)
	}
	if v2.Type != 1 || string(v2.Body) != "b" {
		t.Fatalf("unexpected msg: %+v", v2)
	}
}

func TestMsgEncodeEqualsStdlib(t *testing.T) {
	cases := []*Msg{
		{},
		{Type: 1},
		{Type: 2, Body: []byte("hello")},
		{Type: -7, Body: []byte{0x00, 0xff, 0x10}},
		{Type: 99, Body: []byte{}}, // 空 Body：stdlib 输出 ""，非 null
		{Type: 1, Body: nil},
	}
	for i, m := range cases {
		want, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("case %d: stdlib marshal: %v", i, err)
		}
		got := m.MarshalJSONFast()
		if !bytes.Equal(want, got) {
			t.Errorf("case %d: bytes mismatch\n want: %s\n  got: %s", i, want, got)
		}
		// 解码回环
		var back Msg
		if err := json.Unmarshal(got, &back); err != nil {
			t.Fatalf("case %d: unmarshal: %v", i, err)
		}
		if back.Type != m.Type {
			t.Errorf("case %d: type %d != %d", i, back.Type, m.Type)
		}
		if !bytes.Equal(nilToEmpty(back.Body), nilToEmpty(m.Body)) {
			t.Errorf("case %d: body mismatch", i)
		}
	}
}

func nilToEmpty(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}

// 对象池：归还后字段必须清零，避免下一次取出时读到脏数据（Header 残留会串到别的调用）。
func TestCallJCOPool(t *testing.T) {
	fx := GetCallJCO()
	if fx == nil {
		t.Fatal("GetCallJCO returned nil")
	}
	fx.Method = "v1.Echo"
	fx.Header = Header{"uid": "1"}
	fx.Args = [][]byte{[]byte("x")}
	fx.Data = []byte("d")
	fx.Error = "e"
	PutCallJCO(fx)

	got := GetCallJCO()
	if got.Method != "" || got.Error != "" || got.Header != nil || got.Args != nil || got.Data != nil {
		t.Fatalf("pooled object not reset: %+v", got)
	}
	PutCallJCO(got)
	PutCallJCO(nil) // 不得 panic
}

// 并发 Get/Put（配合 -race）
func TestPoolConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				fx := GetCallJCO()
				fx.Method = "m"
				PutCallJCO(fx)
			}
		}()
	}
	wg.Wait()
}

// normCall 归一化 []byte 的 nil/空差异（JSON 无法区分），便于 DeepEqual
func normCall(c JsonCallObject) JsonCallObject {
	if len(c.Args) == 0 {
		c.Args = nil
	} else {
		for i := range c.Args {
			if len(c.Args[i]) == 0 {
				c.Args[i] = []byte{}
			}
		}
	}
	if len(c.Data) == 0 {
		c.Data = nil
	}
	if len(c.Header) == 0 {
		c.Header = nil
	}
	return c
}

// ---------------------------------------------------------------------------
// Benchmark
// ---------------------------------------------------------------------------

// Benchmark 对照：手写编码 vs encoding/json
func BenchmarkMarshalJSONFast(b *testing.B) {
	c := &JsonCallObject{Method: "Echo", Header: Header{"trace": "abc"}, Args: [][]byte{[]byte("payload")}}
	b.Run("fast", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = c.MarshalJSONFast()
		}
	})
	b.Run("stdlib", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = json.Marshal(c)
		}
	})
}

func BenchmarkMsgMarshal(b *testing.B) {
	m := &Msg{Type: 1, Body: bytes.Repeat([]byte("x"), 1024)}
	b.Run("fast", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = m.MarshalJSONFast()
		}
	})
	b.Run("stdlib", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = json.Marshal(m)
		}
	})
}

// 编码 + 解码整链路（贴近单次 RPC 的真实开销）
func BenchmarkCallObjectRoundTrip(b *testing.B) {
	c := &JsonCallObject{Method: "v1.Echo", Header: Header{"trace": "abc"}, Args: [][]byte{[]byte("payload")}}
	b.Run("fast", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var back JsonCallObject
			if err := json.Unmarshal(c.MarshalJSONFast(), &back); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("stdlib", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			raw, _ := json.Marshal(c)
			var back JsonCallObject
			if err := json.Unmarshal(raw, &back); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkCallJCOPool(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		fx := GetCallJCO()
		fx.Method = "v1.Echo"
		PutCallJCO(fx)
	}
}

// 对照：池化 vs 直接 new（每次 RPC 一个 JsonCallObject）
// sink 防止编译器把未使用的分配优化掉。
var sinkJsonCallObject *JsonCallObject

func BenchmarkCallJCOAlloc(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkJsonCallObject = &JsonCallObject{Method: "v1.Echo"}
	}
}
