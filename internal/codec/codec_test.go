package codec

import (
	"bytes"
	"testing"

	"github.com/w6xian/sloth/v3/actions"
)

// TestGetCodecerTooShort 覆盖空包/短包：此前 raw[0]/raw[1] 直接越界 panic。
func TestGetCodecerTooShort(t *testing.T) {
	cases := [][]byte{nil, {}, {0x40}, {0x40, 0x01}}
	for i, raw := range cases {
		co, err := GetCodecer(raw)
		if err == nil {
			t.Fatalf("case %d: GetCodecer(%v) expected error, got codec %v", i, raw, co)
		}
	}
}

// TestFnCodecRoundTrip FN 帧编解码往返。
func TestFnCodecRoundTrip(t *testing.T) {
	co := UseCodec(CODEC_CODER_FN)
	if co == nil {
		t.Fatal("UseCodec(CODEC_CODER_FN) returned nil")
	}
	const id = uint64(12345)
	body := []byte(`{"method":"v1.Hello","args":["d29ybGQ="]}`)

	actions_ := []byte{actions.ACTION_CALL, actions.ACTION_REPLY_SUCCESS, actions.ACTION_REPLY_ERROR}
	for _, action := range actions_ {
		raw, err := co.Encode(action, id, body)
		if err != nil {
			t.Fatalf("Encode(action=%d) err: %v", action, err)
		}
		// 通过 GetCodecer 自动识别（而不是直接复用 co）
		co2, err := GetCodecer(raw)
		if err != nil {
			t.Fatalf("GetCodecer err: %v", err)
		}
		a, i, b, err := co2.Decode(raw)
		if err != nil {
			t.Fatalf("Decode err: %v", err)
		}
		if a != action {
			t.Errorf("action mismatch: got %d, want %d", a, action)
		}
		if i != id {
			t.Errorf("id mismatch: got %d, want %d", i, id)
		}
		if !bytes.Equal(b, body) {
			t.Errorf("body mismatch: got %q, want %q", b, body)
		}
	}
}

// TestUseCodecUnknownFallback 未知 codec 名应回落到 fnCodec 而不是 panic/nil。
func TestUseCodecUnknownFallback(t *testing.T) {
	co := UseCodec("not-exist-codec")
	if co == nil {
		t.Fatal("UseCodec(unknown) returned nil")
	}
	raw, err := co.Encode(actions.ACTION_CALL, 1, []byte("x"))
	if err != nil {
		t.Fatalf("Encode err: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("encoded frame is empty")
	}
}
