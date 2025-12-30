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

func BytesToInt8(data []byte) int8 {
	return int8(data[0])
}

func BytesToUint8(data []byte) uint8 {
	return uint8(data[0])
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

func slice_so_String(data []byte) string {
	return fmt.Sprintf("\"%s\"", string(data))
}

func slice_bytes_to_slice_strings(data []byte, opt *Option) []string {
	pos := 0
	total := len(data)
	strs := []string{}
	for {
		if pos >= total {
			break
		}
		_, vl, vv, err := Next(data[pos:], opt)
		if err != nil {
			break
		}
		strs = append(strs, slice_so_String(vv))
		pos += vl
	}
	return strs
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

func conv_to_slice_int16(data []byte) []int16 {
	s := []int16{}
	// 2字节为一个int16
	for i := 0; i < len(data); i += 2 {
		s = append(s, BytesToInt16(data[i:i+2]))
	}
	return s
}

func SliceUint16ToString(data []byte) string {
	s := []string{}
	// 2字节为一个uint16
	for i := 0; i < len(data); i += 2 {
		s = append(s, fmt.Sprintf("%d", BytesToUint16(data[i:i+2])))
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}

func conv_to_slice_int8(data []byte) []int8 {
	s := []int8{}
	// 1字节为一个int8
	for i := 0; i < len(data); i += 1 {
		s = append(s, int8(data[i]))
	}
	return s
}

func conv_to_slice_uint16(data []byte) []uint16 {
	s := []uint16{}
	// 2字节为一个uint16
	for i := 0; i < len(data); i += 2 {
		s = append(s, BytesToUint16(data[i:i+2]))
	}
	return s
}

func conv_to_slice_uint32(data []byte) []uint32 {
	s := []uint32{}
	// 4字节为一个uint32
	for i := 0; i < len(data); i += 4 {
		s = append(s, BytesToUint32(data[i:i+4]))
	}
	return s
}

func conv_to_slice_int32(data []byte) []int32 {
	s := []int32{}
	// 4字节为一个int32
	for i := 0; i < len(data); i += 4 {
		s = append(s, BytesToInt32(data[i:i+4]))
	}
	return s
}

func conv_to_slice_float32(data []byte) []float32 {
	s := []float32{}
	// 4字节为一个float32
	for i := 0; i < len(data); i += 4 {
		s = append(s, BytesToFloat32(data[i:i+4]))
	}
	return s
}

func conv_to_slice_float64(data []byte) []float64 {
	s := []float64{}
	// 8字节为一个float64
	for i := 0; i < len(data); i += 8 {
		s = append(s, BytesToFloat64(data[i:i+8]))
	}
	return s
}

func conv_to_slice_int(data []byte) []int {
	s := []int{}
	// 8字节为一个int64
	for i := 0; i < len(data); i += 8 {
		s = append(s, BytesToInt(data[i:i+8]))
	}
	return s
}
func conv_to_slice_int64(data []byte) []int64 {
	s := []int64{}
	// 8字节为一个int64
	for i := 0; i < len(data); i += 8 {
		s = append(s, BytesToInt64(data[i:i+8]))
	}
	return s
}
func conv_to_slice_uint(data []byte) []uint {
	s := []uint{}
	// 8字节为一个uint64
	for i := 0; i < len(data); i += 8 {
		s = append(s, BytesToUint(data[i:i+8]))
	}
	return s
}
func conv_to_slice_uint64(data []byte) []uint64 {
	s := []uint64{}
	// 8字节为一个uint64
	for i := 0; i < len(data); i += 8 {
		s = append(s, BytesToUint64(data[i:i+8]))
	}
	return s
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
func SliceStringToString(v []byte, opt *Option) string {
	pos := 0
	rst := []string{}
	total := len(v)
	for pos+2 < total {
		data := v[pos:]
		if len(data) < 2 {
			break
		}
		ft, fl, fv, ferr := read_tlv_field(data, opt)
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
