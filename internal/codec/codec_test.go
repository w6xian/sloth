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

// TestGetCodecerRejectNonFNFrame 只接受 @F(0x40 0x46) 开头的帧。
func TestGetCodecerRejectNonFNFrame(t *testing.T) {
	cases := [][]byte{
		{0x40, 0x41}, // @A
		{0x46, 0x40}, // 反向 magic
		{0x46, 0x46}, // FF
		{0x00, 0x01},
	}
	for i, raw := range cases {
		if co, err := GetCodecer(raw); err == nil {
			t.Fatalf("case %d: GetCodecer(%v) should error, got codec %v", i, raw, co)
		}
	}
}

// TestFnCodecDecodeTruncated 任意截断的 FN 帧不得 panic（网络字节流不可信）。
func TestFnCodecDecodeTruncated(t *testing.T) {
	co := UseCodec(CODEC_CODER_FN)
	raw, err := co.Encode(actions.ACTION_CALL, 0x1122334455667788, []byte("the quick brown fox jumps over the lazy dog"))
	if err != nil {
		t.Fatalf("Encode err: %v", err)
	}
	for i := 0; i < len(raw); i++ {
		// 截断后只要仍能被识别为 FN（>=2 字节）就必须返回错误而不是 panic
		trunc := raw[:i]
		if _, gerr := GetCodecer(trunc); gerr != nil {
			continue // 2 字节以下直接拒绝
		}
		if _, _, _, derr := co.Decode(trunc); derr == nil {
			t.Fatalf("truncated frame len=%d should fail Decode", i)
		}
	}
}

// TestFnCodecDecodeForgeData 篡改 payload 长度字段（超出实际数据）不得 panic。
func TestFnCodecDecodeForgeData(t *testing.T) {
	co := UseCodec(CODEC_CODER_FN)
	raw, err := co.Encode(actions.ACTION_CALL, 7, []byte("abc"))
	if err != nil {
		t.Fatalf("Encode err: %v", err)
	}
	// 头布局: [0:2]=magic [2]=action [3:11]=id [11:15]=length
	raw = append([]byte(nil), raw...)
	raw[11] = 0xFF // 篡改为极大长度
	if _, _, _, derr := co.Decode(raw); derr == nil {
		t.Fatal("forged length should fail Decode")
	}
}

// TestFnCodecEncodeOversize 超出 1GB 上限的数据应被拒绝。
func TestFnCodecEncodeOversize(t *testing.T) {
	co := UseCodec(CODEC_CODER_FN)
	big := make([]byte, (1<<30)+1) // 超过 FnMaxDataSize
	if _, err := co.Encode(actions.ACTION_CALL, 1, big); err == nil {
		t.Fatal("encode of oversized data should error")
	}
}
