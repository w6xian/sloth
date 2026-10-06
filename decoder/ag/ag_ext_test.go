package ag

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

// 扩展帧（LEN=0xFFFF 转义 + 4 字节真实长度）的边界用例。
//
// 两条主线：
//  1. 短帧格式一字未改，老端编出来的帧照样能解；
//  2. 大包在抬过上限后能往返，没抬时按超长明确拒绝（而不是静默错数据）。

// extFrame 手工拼一个扩展帧，模拟"对端抬了上限"发过来的字节。
func extFrame(t uint8, payload []byte) []byte {
	out := make([]byte, ArgumentExtHeaderSize+len(payload))
	out[0], out[1], out[2] = ArgumentMagic1, ArgumentMagic2, t
	binary.BigEndian.PutUint16(out[3:5], ArgumentExtLenFlag)
	binary.BigEndian.PutUint32(out[5:9], uint32(len(payload)))
	copy(out[ArgumentExtHeaderSize:], payload)
	return out
}

// TestShortVsExtBoundary 短帧/扩展帧的分界：65534 用短帧，65535 起用扩展帧。
func TestShortVsExtBoundary(t *testing.T) {
	// 65534：短帧，与老格式逐字节一致
	short := bytes.Repeat([]byte("a"), ArgumentMaxShortData)
	raw, err := Encode(short)
	if err != nil {
		t.Fatalf("encode %d bytes: %v", len(short), err)
	}
	if len(raw) != ArgumentHeaderSize+len(short) {
		t.Fatalf("short frame len=%d, want %d", len(raw), ArgumentHeaderSize+len(short))
	}
	if l := int(binary.BigEndian.Uint16(raw[3:5])); l != ArgumentMaxShortData {
		t.Fatalf("short LEN=%d, want %d", l, ArgumentMaxShortData)
	}
	if !IsArgument(raw) {
		t.Fatal("short frame should be valid")
	}
	if got, err := Decoder(raw); err != nil || !bytes.Equal(got, short) {
		t.Fatalf("short round trip: got=%d bytes err=%v", len(got), err)
	}

	// 65535：转义位被占用，必须走扩展帧
	ext := bytes.Repeat([]byte("b"), ArgumentMaxShortData+1)
	raw, err = Encode(ext)
	if err != nil {
		t.Fatalf("encode %d bytes: %v", len(ext), err)
	}
	if len(raw) != ArgumentExtHeaderSize+len(ext) {
		t.Fatalf("ext frame len=%d, want %d", len(raw), ArgumentExtHeaderSize+len(ext))
	}
	if l := int(binary.BigEndian.Uint16(raw[3:5])); l != ArgumentExtLenFlag {
		t.Fatalf("ext FLAG=%#x, want %#x", l, ArgumentExtLenFlag)
	}
	if n := int(binary.BigEndian.Uint32(raw[5:9])); n != len(ext) {
		t.Fatalf("ext LEN32=%d, want %d", n, len(ext))
	}
	if !IsArgument(raw) {
		t.Fatal("ext frame should be valid")
	}
	if got, err := Decoder(raw); err != nil || !bytes.Equal(got, ext) {
		t.Fatalf("ext round trip: got=%d bytes err=%v", len(got), err)
	}
}

// TestDefaultLimitUnchanged 不抬上限时行为与扩展帧出现前一致：65535 能过，65536 拒绝。
func TestDefaultLimitUnchanged(t *testing.T) {
	if MaxDataSize() != ArgumentMaxDataSize {
		t.Fatalf("default limit=%d, want %d", MaxDataSize(), ArgumentMaxDataSize)
	}
	if _, err := Encode(bytes.Repeat([]byte("x"), ArgumentMaxDataSize)); err != nil {
		t.Fatalf("65535 bytes should encode: %v", err)
	}
	if _, err := Encode(bytes.Repeat([]byte("x"), ArgumentMaxDataSize+1)); !errors.Is(err, ErrAgDataTooLarge) {
		t.Fatalf("65536 bytes err=%v, want ErrAgDataTooLarge", err)
	}
}

// TestSetMaxDataSize 抬上限后的往返、夹取与超限。
func TestSetMaxDataSize(t *testing.T) {
	defer SetMaxDataSize(ArgumentMaxDataSize) // 还原，别影响其它用例

	// 夹取：低于默认值的按默认值，高于硬顶的按硬顶
	SetMaxDataSize(0)
	if MaxDataSize() != ArgumentMaxDataSize {
		t.Fatalf("clamp low: got %d, want %d", MaxDataSize(), ArgumentMaxDataSize)
	}
	SetMaxDataSize(MaxAgDataSize * 1024)
	if MaxDataSize() != MaxAgDataSize {
		t.Fatalf("clamp high: got %d, want %d", MaxDataSize(), MaxAgDataSize)
	}

	SetMaxDataSize(MaxAgDataSize)
	big := bytes.Repeat([]byte("x"), 4<<20) // 4MB
	raw, err := Encode(big)
	if err != nil {
		t.Fatalf("encode 4MB: %v", err)
	}
	if !IsArgument(raw) {
		t.Fatal("4MB frame should be valid")
	}
	got, err := Decoder(raw)
	if err != nil {
		t.Fatalf("decode 4MB: %v", err)
	}
	if !bytes.Equal(got, big) {
		t.Fatalf("4MB round trip mismatch: got %d bytes, want %d", len(got), len(big))
	}

	// 抬了上限也照旧拒绝更大的
	_, err = Encode(bytes.Repeat([]byte("x"), MaxAgDataSize+1))
	if !errors.Is(err, ErrAgDataTooLarge) {
		t.Fatalf("over limit err=%v, want ErrAgDataTooLarge", err)
	}
	// 抬过上限后错误仍能被 errors.Is 认出，且带上实际值便于排查
	if errors.Is(err, ErrAgDataTooLarge) && !bytes.Contains([]byte(err.Error()), []byte("got")) {
		t.Fatalf("raised-limit error should carry actual size: %q", err.Error())
	}
}

// TestDecodeExtFrame 解析对端发来的扩展帧，以及各类坏帧。
func TestDecodeExtFrame(t *testing.T) {
	defer SetMaxDataSize(ArgumentMaxDataSize)

	payload := bytes.Repeat([]byte{0xAB}, 100000)
	raw := extFrame(ArgumentTypeBytes, payload)

	// 本端没抬上限：按超长拒绝，且不能当成合法帧透传
	if err := Validate(raw); !errors.Is(err, ErrAgDataTooLarge) {
		t.Fatalf("oversized err=%v, want ErrAgDataTooLarge", err)
	}
	if IsArgument(raw) {
		t.Fatal("oversized frame must not be treated as valid")
	}
	if _, err := Decoder(raw); !errors.Is(err, ErrAgDataTooLarge) {
		t.Fatalf("Decoder should surface the error, got %v", err)
	}

	SetMaxDataSize(MaxAgDataSize)
	if err := Validate(raw); err != nil {
		t.Fatalf("validate ext frame: %v", err)
	}
	v, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode ext frame: %v", err)
	}
	if b, ok := v.([]byte); !ok || !bytes.Equal(b, payload) {
		t.Fatalf("ext payload mismatch: %T %d bytes", v, len(b))
	}

	// 截断：声明长度与实收不符
	if err := Validate(raw[:len(raw)-1]); !errors.Is(err, ErrAgLengthMismatch) {
		t.Fatalf("truncated err=%v, want ErrAgLengthMismatch", err)
	}
	// 有转义位但帧头不足 9 字节
	if err := Validate(raw[:6]); !errors.Is(err, ErrAgTooShort) {
		t.Fatalf("short ext header err=%v, want ErrAgTooShort", err)
	}
	// 声明 4GB（uint32 上限）：必须被当前上限挡住，不能照着分配内存
	huge := make([]byte, ArgumentExtHeaderSize)
	huge[0], huge[1], huge[2] = ArgumentMagic1, ArgumentMagic2, ArgumentTypeBytes
	binary.BigEndian.PutUint16(huge[3:5], ArgumentExtLenFlag)
	binary.BigEndian.PutUint32(huge[5:9], math.MaxUint32)
	if err := Validate(huge); !errors.Is(err, ErrAgDataTooLarge) {
		t.Fatalf("4GB claim err=%v, want ErrAgDataTooLarge", err)
	}
}

// TestLegacyShortFrameStillDecodes 老端编的短帧（5 字节头）新端必须照样能解。
func TestLegacyShortFrameStillDecodes(t *testing.T) {
	old := []byte{ArgumentMagic1, ArgumentMagic2, ArgumentTypeString, 0, 5, 'h', 'e', 'l', 'l', 'o'}
	if !IsArgument(old) {
		t.Fatal("legacy short frame should be valid")
	}
	v, err := Decode(old)
	if err != nil {
		t.Fatalf("decode legacy: %v", err)
	}
	if s, ok := v.(string); !ok || s != "hello" {
		t.Fatalf("legacy value=%v", v)
	}
	if got := string(Data(old)); got != "hello" {
		t.Fatalf("Data()=%q", got)
	}
	// 非 AG 数据（不带 magic）仍按原样透传
	plain := []byte(`{"a":1}`)
	got, err := Decoder(plain)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("passthrough: got=%q err=%v", got, err)
	}
}
