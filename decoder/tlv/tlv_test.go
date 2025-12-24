package tlv

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestStr(t *testing.T) {
	frame := "eyJyZXEiOiJzZXJ2ZXIgMSIsInRpbWUiOiIyMDI1LTEyLTA1IDEyOjM0OjA4In0="
	decoded, err := base64.StdEncoding.DecodeString(frame)
	if err != nil {
		fmt.Println("Error decoding:", err)
		return
	}
	fmt.Println("Decoded:", string(decoded))
}

func TestAll(t *testing.T) {

	tests := []struct {
		tag  byte
		data any
	}{
		{
			tag:  TLV_TYPE_STRING,
			data: strings.Repeat("ACDE中国", 0x100),
		},
		{
			tag: TLV_TYPE_JSON,
			data: map[string]any{
				"a": 1,
				"b": "2",
				"c": "中国",
			},
		},
		{
			tag:  TLV_TYPE_INT64,
			data: int64(1234567890),
		},
		{
			tag:  TLV_TYPE_FLOAT64,
			data: float64(1234567890.123456),
		},
		{
			tag:  TLV_TYPE_UINT64,
			data: uint64(1234567890),
		},
	}
	for _, tt := range tests {
		switch tt.tag {
		case TLV_TYPE_STRING:
			from := FrameFromString(tt.data.(string), CheckCRC())
			tag, data, err := tlv_decode(from)
			if err != nil {
				t.Errorf("tlv_decode() = %v, want %v", err, nil)
			}
			if tag != TLV_TYPE_STRING {
				t.Errorf("tlv_decode() = %v, want %v", tag, TLV_TYPE_STRING)
			}
			if !bytes.Equal(data, []byte(tt.data.(string))) {
				t.Errorf("tlv_decode() = %v, want %v", data, []byte(tt.data.(string)))
			}
			t255 := strings.Repeat("ABC", 0x100)
			from = FrameFromString(t255)
			tag, data, err = tlv_decode(from)
			if err != nil {
				t.Errorf("tlv_decode() = %v, want %v", err, nil)
			}
			if tag != TLV_TYPE_STRING {
				t.Errorf("tlv_decode() = %v, want %v", tag, TLV_TYPE_STRING)
			}
			if !bytes.Equal(data, []byte(t255)) {
				t.Errorf("tlv_decode() = %v, want %v", data, []byte(tt.data.(string)))
			}
			return
		case TLV_TYPE_JSON:
			from := FrameFromJson(tt.data)
			tag, data, err := tlv_decode(from)
			if err != nil {
				t.Errorf("tlv_decode() = %v, want %v", err, nil)
			}
			if tag != TLV_TYPE_JSON {
				t.Errorf("tlv_decode_def() = %v, want %v", tag, TLV_TYPE_JSON)
			}

			// 需要比较json字符串是否相等
			jsonData, err := json.Marshal(tt.data)
			if err != nil {
				t.Errorf("json.Unmarshal() = %v, want %v", err, nil)
			}

			if !bytes.Equal(data, jsonData) {
				t.Errorf("tlv_decode_def() = %v, want %v", data, jsonData)
			}
		case TLV_TYPE_INT64:
			from := FrameFromInt64(tt.data.(int64))

			tag, data, err := tlv_decode(from)
			if err != nil {
				t.Errorf("tlv_decode() = %v, want %v", err, nil)
			}
			if tag != TLV_TYPE_INT64 {
				t.Errorf("tlv_decode() = %v, want %v", tag, TLV_TYPE_INT64)
			}
			int64Val := Bytes2Int64(data)

			if int64Val != tt.data.(int64) {
				t.Errorf("tlv_decode() = %v, want %v", int64Val, tt.data.(int64))
			}
		case TLV_TYPE_FLOAT64:
			from := FrameFromFloat64(tt.data.(float64))

			tag, data, err := tlv_decode(from)
			if err != nil {
				t.Errorf("tlv_decode() = %v, want %v", err, nil)
			}
			if tag != TLV_TYPE_FLOAT64 {
				t.Errorf("tlv_decode() = %v, want %v", tag, TLV_TYPE_FLOAT64)
			}
			float64Val := Bytes2Float64(data)
			if float64Val != tt.data.(float64) {
				t.Errorf("tlv_decode() = %v, want %v", float64Val, tt.data.(float64))
			}
		case TLV_TYPE_UINT64:
			from := FrameFromUint64(tt.data.(uint64))

			tag, data, err := tlv_decode(from)
			if err != nil {
				t.Errorf("tlv_decode() = %v, want %v", err, nil)
			}
			if tag != TLV_TYPE_UINT64 {
				t.Errorf("tlv_decode() = %v, want %v", tag, TLV_TYPE_UINT64)
			}
			uint64Val := Bytes2Uint64(data)
			if uint64Val != tt.data.(uint64) {
				t.Errorf("tlv_decode() = %v, want %v", uint64Val, tt.data.(uint64))
			}
		}
	}
}
