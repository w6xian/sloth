package fn

import (
	"bytes"
	"errors"
	"testing"
)

func TestFnHeader(t *testing.T) {
	h := FnHeader()
	if len(h) != 2 {
		t.Fatalf("FnHeader len = %d, want 2", len(h))
	}
	if h[0] != FnMagic1 || h[1] != FnMagic2 {
		t.Fatalf("FnHeader = 0x%02X%02X, want 0x%02X%02X", h[0], h[1], FnMagic1, FnMagic2)
	}
}

func TestIsFn(t *testing.T) {
	cases := []struct {
		name string
		b    []byte
		want bool
	}{
		{"empty", nil, false},
		{"one byte", []byte{FnMagic1}, false},
		{"good header", []byte{FnMagic1, FnMagic2, 0x00}, true},
		{"bad first", []byte{0x00, FnMagic2}, false},
		{"bad second", []byte{FnMagic1, 0x00}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsFn(c.b); got != c.want {
				t.Errorf("IsFn(%v) = %v, want %v", c.b, got, c.want)
			}
		})
	}
}

func TestEncodeFn_NilFrame(t *testing.T) {
	b, err := EncodeFn(nil)
	if !errors.Is(err, ErrFnNilFrame) {
		t.Fatalf("EncodeFn(nil) err = %v, want ErrFnNilFrame", err)
	}
	if b != nil {
		t.Fatalf("EncodeFn(nil) bytes = %v, want nil", b)
	}
}

func TestEncodeFn_DataTooLarge(t *testing.T) {
	f := &FnFrame{Action: 1, ID: 1, Data: make([]byte, FnMaxDataSize+1)}
	_, err := EncodeFn(f)
	if !errors.Is(err, ErrFnDataTooLarge) {
		t.Fatalf("EncodeFn oversized err = %v, want ErrFnDataTooLarge", err)
	}
}

func TestEncodeDecode_EmptyData(t *testing.T) {
	orig := &FnFrame{Action: 0x01, ID: 0, Data: nil}
	b, err := EncodeFn(orig)
	if err != nil {
		t.Fatalf("EncodeFn err: %v", err)
	}
	if len(b) != FnHeaderSize {
		t.Fatalf("encoded len = %d, want %d", len(b), FnHeaderSize)
	}
	if !IsFn(b) {
		t.Fatal("IsFn(encoded) = false")
	}
	dec, err := DecodeFn(b)
	if err != nil {
		t.Fatalf("DecodeFn err: %v", err)
	}
	if dec.Action != orig.Action {
		t.Errorf("Action = 0x%02X, want 0x%02X", dec.Action, orig.Action)
	}
	if dec.ID != orig.ID {
		t.Errorf("ID = %d, want %d", dec.ID, orig.ID)
	}
	if len(dec.Data) != 0 {
		t.Errorf("Data len = %d, want 0", len(dec.Data))
	}
}

func TestEncodeDecode_WithData(t *testing.T) {
	payload := []byte("hello fn protocol test payload \x00\x01\xff binary safe")
	orig := &FnFrame{Action: 0xFF, ID: 0x0123456789ABCDEF, Data: payload}
	b, err := EncodeFn(orig)
	if err != nil {
		t.Fatalf("EncodeFn err: %v", err)
	}
	wantLen := FnHeaderSize + len(payload)
	if len(b) != wantLen {
		t.Fatalf("encoded len = %d, want %d", len(b), wantLen)
	}
	dec, err := DecodeFn(b)
	if err != nil {
		t.Fatalf("DecodeFn err: %v", err)
	}
	if dec.Action != orig.Action {
		t.Errorf("Action = 0x%02X, want 0x%02X", dec.Action, orig.Action)
	}
	if dec.ID != orig.ID {
		t.Errorf("ID = 0x%X, want 0x%X", dec.ID, orig.ID)
	}
	if !bytes.Equal(dec.Data, payload) {
		t.Errorf("Data mismatch:\n got  %v\n want %v", dec.Data, payload)
	}
	if &dec.Data[0] == &payload[0] {
		t.Error("DecodeFn should copy data, not share underlying array")
	}
}

func TestDecodeFn_TooShort(t *testing.T) {
	cases := []struct {
		name string
		b    []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"14 bytes", make([]byte, FnHeaderSize-1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DecodeFn(c.b)
			if !errors.Is(err, ErrFnTooShort) {
				t.Fatalf("DecodeFn err = %v, want ErrFnTooShort", err)
			}
		})
	}
}

func TestDecodeFn_BadMagic(t *testing.T) {
	b := make([]byte, FnHeaderSize+4)
	b[0] = 0x00
	b[1] = 0x00
	_, err := DecodeFn(b)
	if !errors.Is(err, ErrFnBadMagic) {
		t.Fatalf("DecodeFn err = %v, want ErrFnBadMagic", err)
	}
}

func TestDecodeFn_LengthMismatch(t *testing.T) {
	f := &FnFrame{Action: 1, ID: 42, Data: make([]byte, 100)}
	b, _ := EncodeFn(f)
	truncated := b[:len(b)-1]
	_, err := DecodeFn(truncated)
	if !errors.Is(err, ErrFnLengthMismatch) {
		t.Fatalf("DecodeFn truncated err = %v, want ErrFnLengthMismatch", err)
	}
}

func TestValidateFn_AllOk(t *testing.T) {
	f := &FnFrame{Action: 1, ID: 1, Data: []byte{1, 2, 3}}
	b, _ := EncodeFn(f)
	if err := ValidateFn(b); err != nil {
		t.Fatalf("ValidateFn err: %v", err)
	}
}

func TestValidateFn_InvalidAction(t *testing.T) {
	b := make([]byte, FnHeaderSize)
	b[0] = FnMagic1
	b[1] = FnMagic2
	b[2] = 0
	if err := ValidateFn(b); !errors.Is(err, ErrFnInvalidAction) {
		t.Fatalf("ValidateFn action=0 err = %v, want ErrFnInvalidAction", err)
	}
}

func TestValidateFn_TooShort(t *testing.T) {
	err := ValidateFn([]byte{FnMagic1})
	if !errors.Is(err, ErrFnTooShort) {
		t.Fatalf("ValidateFn short err = %v, want ErrFnTooShort", err)
	}
}

func TestValidateFn_BadMagic(t *testing.T) {
	b := make([]byte, FnHeaderSize)
	err := ValidateFn(b)
	if !errors.Is(err, ErrFnBadMagic) {
		t.Fatalf("ValidateFn bad magic err = %v, want ErrFnBadMagic", err)
	}
}

func TestValidateFn_DataTooLarge(t *testing.T) {
	b := make([]byte, FnHeaderSize)
	b[0] = FnMagic1
	b[1] = FnMagic2
	b[2] = 1
	b[11] = 0xFF
	b[12] = 0xFF
	b[13] = 0xFF
	b[14] = 0xFF
	err := ValidateFn(b)
	if !errors.Is(err, ErrFnDataTooLarge) {
		t.Fatalf("ValidateFn oversized err = %v, want ErrFnDataTooLarge", err)
	}
}

func TestValidateFn_LengthMismatch(t *testing.T) {
	b := make([]byte, FnHeaderSize)
	b[0] = FnMagic1
	b[1] = FnMagic2
	b[2] = 1
	b[14] = 4
	err := ValidateFn(b)
	if !errors.Is(err, ErrFnLengthMismatch) {
		t.Fatalf("ValidateFn length mismatch err = %v, want ErrFnLengthMismatch", err)
	}
}

func TestParseFnHeader(t *testing.T) {
	action := uint8(0x7F)
	id := uint64(1234567890)
	dataLen := uint32(256)
	b := make([]byte, FnHeaderSize)
	b[0] = FnMagic1
	b[1] = FnMagic2
	b[2] = action
	b[3] = 0x00
	b[4] = 0x00
	b[5] = 0x00
	b[6] = 0x00
	b[7] = 0x49
	b[8] = 0x96
	b[9] = 0x02
	b[10] = 0xD2
	b[11] = 0x00
	b[12] = 0x00
	b[13] = 0x01
	b[14] = 0x00

	a, i, l, err := ParseFnHeader(b)
	if err != nil {
		t.Fatalf("ParseFnHeader err: %v", err)
	}
	if a != action {
		t.Errorf("action = 0x%02X, want 0x%02X", a, action)
	}
	if i != id {
		t.Errorf("id = %d, want %d", i, id)
	}
	if l != dataLen {
		t.Errorf("length = %d, want %d", l, dataLen)
	}
}

func TestParseFnHeader_Error(t *testing.T) {
	_, _, _, err := ParseFnHeader([]byte{0x00, 0x00})
	if !errors.Is(err, ErrFnTooShort) {
		t.Fatalf("ParseFnHeader short err = %v, want ErrFnTooShort", err)
	}

	b := make([]byte, FnHeaderSize)
	_, _, _, err = ParseFnHeader(b)
	if !errors.Is(err, ErrFnBadMagic) {
		t.Fatalf("ParseFnHeader bad magic err = %v, want ErrFnBadMagic", err)
	}
}

func TestEncodeDecode_ActionRange(t *testing.T) {
	actions := []uint8{0x01, 0x7F, 0x80, 0xFE, 0xFF}
	for _, a := range actions {
		t.Run("action_0x%02X", func(t *testing.T) {
			orig := &FnFrame{Action: a, ID: uint64(a) * 1000, Data: []byte{byte(a)}}
			b, err := EncodeFn(orig)
			if err != nil {
				t.Fatalf("EncodeFn action=0x%02X err: %v", a, err)
			}
			dec, err := DecodeFn(b)
			if err != nil {
				t.Fatalf("DecodeFn action=0x%02X err: %v", a, err)
			}
			if dec.Action != a {
				t.Errorf("Action = 0x%02X, want 0x%02X", dec.Action, a)
			}
		})
	}
}

func TestEncodeDecode_IDBoundary(t *testing.T) {
	ids := []uint64{0, 1, ^uint64(0) - 1, ^uint64(0)}
	for _, id := range ids {
		orig := &FnFrame{Action: 1, ID: id, Data: []byte("x")}
		b, err := EncodeFn(orig)
		if err != nil {
			t.Fatalf("EncodeFn id=%d err: %v", id, err)
		}
		dec, err := DecodeFn(b)
		if err != nil {
			t.Fatalf("DecodeFn id=%d err: %v", id, err)
		}
		if dec.ID != id {
			t.Errorf("ID = %d (0x%X), want %d (0x%X)", dec.ID, dec.ID, id, id)
		}
	}
}

func TestDecodeFn_IgnoresTrailingBytes(t *testing.T) {
	orig := &FnFrame{Action: 2, ID: 99, Data: []byte("body")}
	b, _ := EncodeFn(orig)
	extra := append(b, 0xDE, 0xAD, 0xBE, 0xEF)
	dec, err := DecodeFn(extra)
	if err != nil {
		t.Fatalf("DecodeFn with trailing bytes err: %v", err)
	}
	if dec.Action != orig.Action || dec.ID != orig.ID || !bytes.Equal(dec.Data, orig.Data) {
		t.Errorf("DecodeFn with trailing bytes mismatch: got %+v, want %+v", dec, orig)
	}
}

func BenchmarkEncodeFn(b *testing.B) {
	f := &FnFrame{Action: 1, ID: 12345, Data: make([]byte, 1024)}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EncodeFn(f)
	}
}

func BenchmarkDecodeFn(b *testing.B) {
	f := &FnFrame{Action: 1, ID: 12345, Data: make([]byte, 1024)}
	buf, _ := EncodeFn(f)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeFn(buf)
	}
}

func BenchmarkValidateFn(b *testing.B) {
	f := &FnFrame{Action: 1, ID: 12345, Data: make([]byte, 1024)}
	buf, _ := EncodeFn(f)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateFn(buf)
	}
}
