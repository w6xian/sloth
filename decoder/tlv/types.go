package tlv

import (
	"encoding/binary"
	"encoding/json"
	"math"
)

type Bin []byte
type TLVFrame []byte

func FrameFromString(v string) TLVFrame {
	r, err := tlv_encode(TLV_TYPE_STRING, []byte(v))
	if err != nil {
		return []byte{}
	}
	return r
}

func FrameFromJson(v any) TLVFrame {
	jsonData, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	r, err := tlv_encode(TLV_TYPE_JSON, jsonData)
	if err != nil {
		return []byte{}
	}
	return r
}

func FrameFromBinary(v Bin) TLVFrame {
	r, err := tlv_encode(TLV_TYPE_BINARY, v)
	if err != nil {
		return []byte{}
	}
	return r
}

// Float64 从float64编码为tlv
func FrameFromFloat64(v float64) TLVFrame {
	bits := math.Float64bits(v)
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, bits)
	r, err := tlv_encode(TLV_TYPE_FLOAT64, bytes)
	if err != nil {
		return []byte{}
	}
	return r
}

// Int64 从int64编码为tlv
func FrameFromInt64(v int64) TLVFrame {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, uint64(v))
	r, err := tlv_encode(TLV_TYPE_INT64, bytes)
	if err != nil {
		return []byte{}
	}
	return r
}

// Uint64 从uint64编码为tlv
func FrameFromUint64(v uint64) TLVFrame {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, v)
	r, err := tlv_encode(TLV_TYPE_UINT64, bytes)
	if err != nil {
		return []byte{}
	}
	return r
}

func Bytes2Float64(v []byte) float64 {
	bits := binary.BigEndian.Uint64(v)
	return math.Float64frombits(bits)
}

func FrameToFloat64(v TLVFrame) (float64, error) {
	if len(v) != 8+TLVX_HEADDER_SIZE {
		return 0, ErrInvalidFloat64
	}
	if v[0] != TLV_TYPE_FLOAT64 {
		return 0, ErrInvalidFloat64Type
	}
	fv := Bytes2Float64(v[TLVX_HEADDER_SIZE:])
	return fv, nil
}

func Bytes2Int64(v []byte) int64 {
	return int64(binary.BigEndian.Uint64(v))
}

func FrameToInt64(v TLVFrame) (int64, error) {
	if len(v) != 8+TLVX_HEADDER_SIZE {
		return 0, ErrInvalidInt64
	}
	if v[0] != TLV_TYPE_INT64 {
		return 0, ErrInvalidInt64Type
	}
	bits := Bytes2Uint64(v[TLVX_HEADDER_SIZE:])
	return int64(bits), nil
}

func Bytes2Uint64(v []byte) uint64 {
	return binary.BigEndian.Uint64(v)
}

// Uint64 从tlv解码为uint64
func FrameToUint64(v TLVFrame) (uint64, error) {
	if len(v) != 8+TLVX_HEADDER_SIZE {
		return 0, ErrInvalidUint64
	}
	if v[0] != TLV_TYPE_UINT64 {
		return 0, ErrInvalidUint64Type
	}
	bits := Bytes2Uint64(v[TLVX_HEADDER_SIZE:])
	return bits, nil
}

// Int64 从int64编码为tlv
func FrameToStruct(v TLVFrame, t any) error {
	if v == nil {
		return ErrInvalidValueLength
	}
	if len(v) < TLVX_HEADDER_SIZE {
		return ErrInvalidValueLength
	}
	if v[0] != TLV_TYPE_JSON {
		return ErrInvalidStructType
	}
	_, data, err := tlv_decode(v)
	if err != nil {
		return err
	}
	err = json.Unmarshal(data, t)
	if err != nil {
		return err
	}
	return nil
}

func FrameToBin(v TLVFrame) (Bin, error) {
	if v == nil {
		return nil, ErrInvalidValueLength
	}
	if len(v) < TLVX_HEADDER_SIZE {
		return nil, ErrInvalidValueLength
	}
	if v[0] != TLV_TYPE_BINARY {
		return nil, ErrInvalidBinType
	}
	_, data, err := tlv_decode(v)
	if err != nil {
		return nil, err
	}
	return data, nil
}
