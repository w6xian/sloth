package message

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/w6xian/tlv"
)

// decodeByTlv 用 tlv 标准解码器解析 Header.Bytes() 输出，验证协议兼容性。
func decodeByTlv(t *testing.T, buf []byte) Header {
	t.Helper()
	jsonData, err := tlv.JsonUnpack(buf)
	if err != nil {
		t.Fatalf("tlv.JsonUnpack failed: %v", err)
	}
	var h Header
	if err := json.Unmarshal(jsonData, &h); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	return h
}

func TestHeaderBytesRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		header Header
	}{
		{"empty", Header{}},
		{"single", Header{"uid": "10086"}},
		{"multi", Header{"uid": "10086", "token": "abc123", "seq": "1"}},
		{"chinese", Header{"name": "你好世界", "nick": "张三"}},
		{"quote", Header{`key"1`: `va"lue`}},
		{"backslash", Header{`a\b`: `c\d`}},
		{"control", Header{"k": "a\nb\tc\x01"}},
		{"html", Header{"k": "<script>&</script>"}},
		{"emoji", Header{"emoji": "🎉🚀"}},
		{"empty_value", Header{"uid": "", "token": "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf, err := tt.header.Bytes()
			if err != nil {
				t.Fatalf("Bytes() error: %v", err)
			}
			got := decodeByTlv(t, buf)
			if !reflect.DeepEqual(got, tt.header) {
				t.Fatalf("round trip mismatch:\n got: %#v\nwant: %#v", got, tt.header)
			}
		})
	}
}

// 大 Header 触发 2 字节长度字段（>255 字节）。
func TestHeaderBytesLarge(t *testing.T) {
	big := strings.Repeat("x", 300)
	h := Header{"big": big, "k2": "v2"}
	buf, err := h.Bytes()
	if err != nil {
		t.Fatalf("Bytes() error: %v", err)
	}
	got := decodeByTlv(t, buf)
	if !reflect.DeepEqual(got, h) {
		t.Fatalf("large round trip mismatch: len(got.big)=%d want=%d", len(got["big"]), len(big))
	}
}

// 与 tlv.JsonEnpack 的输出互解：我方编码 tlv 能解，tlv 编码我方 NewHeaderFromBV 能解。
func TestHeaderCompatibleWithTlvEnpack(t *testing.T) {
	h := Header{"uid": "10086", "token": "abc", "lang": "zh-CN"}

	// 1) 我方编码 → tlv 标准解码
	buf, err := h.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeByTlv(t, buf); !reflect.DeepEqual(got, h) {
		t.Fatalf("our encode -> tlv decode mismatch: %#v", got)
	}

	// 2) tlv 标准编码 → 我方 NewHeaderFromBV 解码
	tlvBuf, err := tlv.JsonEnpack(h)
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewHeaderFromBV(tlvBuf)
	if err != nil {
		t.Fatalf("NewHeaderFromBV(tlv bytes) failed: %v", err)
	}
	if !reflect.DeepEqual(got, h) {
		t.Fatalf("tlv encode -> our decode mismatch: %#v", got)
	}
}

func TestNewHeaderFromBV(t *testing.T) {
	h := Header{"uid": "1", "token": "t"}
	buf, err := h.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewHeaderFromBV(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, h) {
		t.Fatalf("NewHeaderFromBV mismatch: %#v", got)
	}

	if _, err := NewHeaderFromBV([]byte{0x00, 0x21, 0x01}); err == nil {
		t.Fatal("expected error for malformed bytes, got nil")
	}
}

func TestHeaderOps(t *testing.T) {
	h := Header{}
	h.Set("a", "1")
	h.Set("b", "") // 空值删除
	if h.Get("a") != "1" {
		t.Fatalf("Get(a)=%q", h.Get("a"))
	}
	if _, ok := h["b"]; ok {
		t.Fatal("b should be deleted")
	}
	h.Set("a", "")
	if _, ok := h["a"]; ok {
		t.Fatal("a should be deleted after empty set")
	}

	h = Header{"k1": "v1", "k2": "v2", "k3": "v3"}
	keys := h.Keys("k1", "k3", "kX")
	if !reflect.DeepEqual(keys, Header{"k1": "v1", "k3": "v3"}) {
		t.Fatalf("Keys mismatch: %#v", keys)
	}

	clone := h.Clone()
	clone["k1"] = "changed"
	if h["k1"] != "v1" {
		t.Fatal("Clone should not affect original")
	}
}

// 并发只读 Bytes 不应 panic（无共享可变状态）。
func TestHeaderBytesConcurrent(t *testing.T) {
	h := Header{"uid": "10086", "token": "abc123", "seq": "1"}
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 1000; j++ {
				if _, err := h.Bytes(); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}

// 验证 Header 编码结果可以被 json 直接解析（内联进 CallMsg 等结构时行为一致）。
func TestHeaderJSONEmbed(t *testing.T) {
	type wrap struct {
		Header Header `json:"header"`
		Method string `json:"method"`
	}
	w := wrap{Header: Header{"uid": "1"}, Method: "m"}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	var back wrap
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Header, w.Header) {
		t.Fatalf("embed mismatch: %#v", back.Header)
	}
}
