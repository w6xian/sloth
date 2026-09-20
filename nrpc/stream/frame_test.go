package stream

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/w6xian/sloth/v4/decoder/fn"
)

func TestReadFrame_OK(t *testing.T) {
	frame, err := fn.Encode(1, 7, []byte("hello"))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var h headBuf
	r := bufio.NewReader(bytes.NewReader(frame))
	got, err := ReadFrame(r, h[:])
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("got %v, want %v", got, frame)
	}
}

// TestReadFrame_StreamNotShifted 连续多帧：字节流必须逐帧对齐，不能错位。
func TestReadFrame_StreamNotShifted(t *testing.T) {
	var buf bytes.Buffer
	want := make([][]byte, 0, 3)
	for i := 0; i < 3; i++ {
		f, err := fn.Encode(uint8(i+1), uint64(i), bytes.Repeat([]byte{byte('a' + i)}, i*100))
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		buf.Write(f)
		want = append(want, f)
	}
	var h headBuf
	r := bufio.NewReader(&buf)
	for i, w := range want {
		got, err := ReadFrame(r, h[:])
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if !bytes.Equal(got, w) {
			t.Fatalf("frame %d mismatch: got %d bytes, want %d", i, len(got), len(w))
		}
	}
	if _, err := ReadFrame(r, h[:]); err != io.EOF {
		t.Fatalf("tail read err = %v, want io.EOF", err)
	}
}

// TestReadFrame_BadInput 畸形输入必须报错而不是 panic 或巨额分配。
func TestReadFrame_BadInput(t *testing.T) {
	// length 声明超过上限：不能照着它分配内存
	over := make([]byte, fn.FnHeaderSize)
	over[0], over[1], over[2] = fn.FnMagic1, fn.FnMagic2, 1
	binary.BigEndian.PutUint32(over[11:15], uint32(fn.FnMaxDataSize)+1)

	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{"bad magic", append([]byte{0x00, 0x00}, make([]byte, fn.FnHeaderSize-2)...), errBadMagic},
		{"length over limit", over, errFrameTooLarge},
		{"truncated header", []byte{fn.FnMagic1, fn.FnMagic2}, io.ErrUnexpectedEOF},
		{"empty", nil, io.EOF},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var h headBuf
			r := bufio.NewReader(bytes.NewReader(c.raw))
			if _, err := ReadFrame(r, h[:]); err == nil {
				t.Fatal("expected error")
			} else if c.want != nil && !isErr(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func isErr(got, want error) bool {
	for err := got; err != nil; err = unwrap(err) {
		if err == want {
			return true
		}
	}
	return false
}

func unwrap(err error) error {
	u, ok := err.(interface{ Unwrap() error })
	if !ok {
		return nil
	}
	return u.Unwrap()
}
