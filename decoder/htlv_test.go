package decoder

import (
	"bytes"
	"fmt"
	"testing"
)

func TestEncodeATLVCrc(t *testing.T) {

	tests := []struct {
		name string
		l    string
	}{
		{
			name: "rand-10",
			l:    "ACDE中国人",
		},
	}
	for _, tt := range tests {
		d := []byte(tt.l)
		atlv := NewATLVCrcDecoder(0x01, 0x02, d)
		buf := EncodeATLVCrc(atlv)

		if buf[0] != atlv.Address {
			t.Errorf("EncodeATLVCrc() = %v, want %v", buf[0], atlv.Address)
		}
		if buf[1] != atlv.FunctionCode {
			t.Errorf("EncodeATLVCrc() = %v, want %v", buf[1], atlv.FunctionCode)
		}

		atlv2 := DecodeATLVCrc(buf)
		if atlv2.Address != atlv.Address {
			t.Errorf("DecodeATLVCrc() = %v, want %v", atlv2.Address, atlv.Address)
		}
		if atlv2.FunctionCode != atlv.FunctionCode {
			t.Errorf("DecodeATLVCrc() = %v, want %v", atlv2.FunctionCode, atlv.FunctionCode)
		}
		a := DecodeATLVCrc(buf)
		if !bytes.Equal(a.Body, atlv.Body) {
			t.Errorf("DecodeATLVCrc() = %v, want %v", a.Body, atlv.Body)
		}
		fmt.Println(string(a.Body))

	}
}
