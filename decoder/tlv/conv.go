package tlv

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

// BytesToInt converts a byte slice to a 64-bit integer.
func BytesToInt(data []byte) int {
	return int(binary.BigEndian.Uint64(data))
}

// BytesToInt16 converts a byte slice to a 16-bit integer.
func BytesToInt16(data []byte) int16 {
	r := binary.BigEndian.Uint16(data)
	return int16(r)
}

// BytesToInt32 converts a byte slice to a 32-bit integer.
func BytesToInt32(data []byte) int32 {
	return int32(binary.BigEndian.Uint32(data))
}

// BytesToInt64 converts a byte slice to a 64-bit integer.
func BytesToInt64(data []byte) int64 {
	return int64(binary.BigEndian.Uint64(data))
}

// BytesToUint converts a byte slice to a 64-bit unsigned integer.
func BytesToUint(data []byte) uint {
	return uint(binary.BigEndian.Uint64(data))
}

// BytesToUint16 converts a byte slice to a 16-bit unsigned integer.
func BytesToUint16(data []byte) uint16 {
	return uint16(binary.BigEndian.Uint16(data))
}

// BytesToUint32 converts a byte slice to a 32-bit unsigned integer.
func BytesToUint32(data []byte) uint32 {
	return binary.BigEndian.Uint32(data)
}

// BytesToUint64 converts a byte slice to a 64-bit unsigned integer.
func BytesToUint64(data []byte) uint64 {
	return binary.BigEndian.Uint64(data)
}

func BytesToFloat32(data []byte) float32 {
	return math.Float32frombits(binary.BigEndian.Uint32(data))
}

// BytesToFloat64 converts a byte slice to a 64-bit floating-point number.
func BytesToFloat64(data []byte) float64 {
	return math.Float64frombits(binary.BigEndian.Uint64(data))
}

// BytesToBool converts a byte slice to a boolean value.
func BytesToBool(data []byte) bool {
	return data[0] != 0
}

// BytesToByte converts a byte slice to a byte value.
func BytesToByte(data []byte) byte {
	return data[0]
}

// BytesToComplex64 converts a byte slice to a 64-bit complex number.
func BytesToComplex64(data []byte) complex64 {
	return complex(BytesToFloat32(data[:4]), BytesToFloat32(data[4:]))
}

// BytesToComplex128 converts a byte slice to a 128-bit complex number.
func BytesToComplex128(data []byte) complex128 {
	return complex(BytesToFloat64(data[:8]), BytesToFloat64(data[8:]))
}

// BytesToUintptr converts a byte slice to a uintptr value.
func BytesToUintptr(data []byte) uintptr {
	return uintptr(binary.BigEndian.Uint64(data))
}

// BytesToRune converts a byte slice to a rune value.
func BytesToRune(data []byte) string {
	if len(data) < 4 {
		return ""
	}
	return string(int32(binary.BigEndian.Uint32(data)))
}

// SliceByteToString converts a byte slice to a slice of byte values.
func SliceByteToString(data []byte) string {
	s := []string{}
	for _, v := range data {
		s = append(s, fmt.Sprintf("%d", v))
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}

// SliceInt64ToString converts a byte slice to a slice of 64-bit integer values.
func SliceInt16ToString(data []byte) string {
	s := []string{}
	// 2字节为一个int16
	for i := 0; i < len(data); i += 2 {
		s = append(s, fmt.Sprintf("%d", BytesToInt16(data[i:i+2])))
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}

func SliceUint16ToString(data []byte) string {
	s := []string{}
	// 2字节为一个uint16
	for i := 0; i < len(data); i += 2 {
		s = append(s, fmt.Sprintf("%d", BytesToUint16(data[i:i+2])))
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}

// SliceInt64ToString converts a byte slice to a slice of 64-bit integer values.
func SliceInt32ToString(data []byte) string {
	s := []string{}
	// 4字节为一个int32
	for i := 0; i < len(data); i += 4 {
		s = append(s, fmt.Sprintf("%d", BytesToInt32(data[i:i+4])))
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}

func SliceUint32ToString(data []byte) string {
	s := []string{}
	// 4字节为一个uint32
	for i := 0; i < len(data); i += 4 {
		s = append(s, fmt.Sprintf("%d", BytesToUint32(data[i:i+4])))
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}

func SliceInt64ToString(data []byte) string {
	s := []string{}
	// 8字节为一个int64
	for i := 0; i < len(data); i += 8 {
		s = append(s, fmt.Sprintf("%d", BytesToInt64(data[i:i+8])))
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}
func SliceUint64ToString(data []byte) string {
	s := []string{}
	// 8字节为一个uint64
	for i := 0; i < len(data); i += 8 {
		s = append(s, fmt.Sprintf("%d", BytesToUint64(data[i:i+8])))
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}

// SliceStringToString converts a byte slice to a slice of string values.
func SliceStringToString(v []byte) string {
	pos := 0
	rst := []string{}
	total := len(v)
	for pos+2 < total {
		data := v[pos:]
		if len(data) < 2 {
			break
		}
		ft, fl, fv, ferr := read_tlv_field(data)
		if ferr != nil {
			rst = append(rst, "\"\"")
			break
		}
		if ft != TLV_TYPE_STRING {
			rst = append(rst, "\"\"")
			break
		}
		rst = append(rst, fmt.Sprintf("\"%s\"", string(fv)))
		pos += fl
	}
	return fmt.Sprintf("[%s]", strings.Join(rst, ","))
}
