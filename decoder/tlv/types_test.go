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
			tag:  TLV_TYPE_UINT8,
			data: byte(0x01),
		},
		{
			tag:  TLV_TYPE_NIL,
			data: nil,
		},
		{
			tag:  TLV_TYPE_STRING,
			data: "hello",
		},
		{
			tag:  TLV_TYPE_FLOAT32,
			data: float32(1.0),
		},
		{
			tag:  TLV_TYPE_FLOAT64,
			data: float64(1.0),
		},
		{
			tag:  TLV_TYPE_INT8,
			data: int8(1),
		},
		{
			tag:  TLV_TYPE_INT16,
			data: int16(1),
		},
		{
			tag:  TLV_TYPE_INT32,
			data: int32(1),
		},
		{
			tag:  TLV_TYPE_INT64,
			data: int64(1),
		},
		{
			tag:  TLV_TYPE_UINT8,
			data: uint8(1),
		},
		{
			tag:  TLV_TYPE_UINT16,
			data: uint16(1),
		},
		{
			tag:  TLV_TYPE_UINT32,
			data: uint32(1),
		},
		{
			tag:  TLV_TYPE_UINT64,
			data: uint64(1),
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.tag), func(t *testing.T) {
			newOpt := NewOption(LengthSize(1, 4))
			frame := Serialize(tt.data)
			tlv, err := tlv_new_from_frame(frame, newOpt)
			if err != nil {
				t.Errorf("NewTLVFromFrame() error = %v", err)
				return
			}
			// fmt.Println(frame)
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
