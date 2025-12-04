package decoder

import (
	"bytes"
	"fmt"
	"testing"
)

func TestEncodeHdC(t *testing.T) {

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
		id := NextId()
		hdc := NewHdC(id, 0x01, 0x02, d)
		buf := EncodeHdC(hdc)
		fmt.Println(buf)
		if buf[0+ID_SIZE] != hdc.Address() {
			t.Errorf("EncodeHdC() = %v, want %v", buf[0+ID_SIZE], hdc.Address())
		}
		if buf[1+ID_SIZE] != hdc.FunctionCode() {
			t.Errorf("EncodeHdC() = %v, want %v", buf[1+ID_SIZE], hdc.FunctionCode())
		}
		hdc, err := DecodeHdC(buf)
		if err != nil {
			t.Errorf("DecodeHdC() = %v, want %v", err, nil)
		}
		if !bytes.Equal(hdc.Data(), d) {
			t.Errorf("DecodeHdC() = %v, want %v", hdc.Data(), d)
		}
		fmt.Printf("%v\n", hdc.Length())
		fmt.Println("Header:", hdc.Header())
		fmt.Println(string(hdc.Data()))
		fmt.Println(hdc.Id(), id)
		fmt.Println(hdc.Address())
		fmt.Println(hdc.FunctionCode())

	}
}
