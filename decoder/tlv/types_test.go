package tlv

import (
	"fmt"
	"testing"
)

func TestTypes(t *testing.T) {

	tests := []struct {
		tag  byte
		data any
	}{
		{
			tag:  TLV_TYPE_BYTE,
			data: byte(0x01),
		},
		{
			tag:  TLV_TYPE_BYTE,
			data: nil,
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.tag), func(t *testing.T) {
			frame := Serialize(tt.data)
			tlv, err := NewTLVFromFrame(frame)
			if err != nil {
				t.Errorf("NewTLVFromFrame() error = %v", err)
				return
			}
			if tt.data == nil {
				if tlv.Type() != TLV_TYPE_NIL {
					t.Errorf("TLV.Type() = %v, want %v", tlv.Type(), TLV_TYPE_NIL)
				}
			} else if tlv.Type() != tt.tag {
				t.Errorf("TLV.Type() = %v, want %v", tlv.Type(), tt.tag)
			}
		})
	}
}
