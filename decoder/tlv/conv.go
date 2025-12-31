package tlv

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

// BytesToInt converts a byte slice to a 64-bit integer.
func bytes_to_int(data []byte) int {
	l := len(data)
	switch l {
	case 1:
		return int(data[0])
	case 2:
		return int(binary.BigEndian.Uint16(data))
	case 4:
		return int(binary.BigEndian.Uint32(data))
	case 8:
		return int(binary.BigEndian.Uint64(data))
	default:
		return 0
	}
}

// bytes_to_int16 converts a byte slice to a 16-bit integer.
func bytes_to_int16(data []byte) int16 {
	l := len(data)
	switch l {
	case 1:
		return int16(data[0])
	case 2:
		return int16(binary.BigEndian.Uint16(data))
	case 4:
		return int16(binary.BigEndian.Uint32(data))
	case 8:
		return int16(binary.BigEndian.Uint64(data))
	default:
		return 0
	}
}

// bytes_to_int32 converts a byte slice to a 32-bit integer.
func bytes_to_int32(data []byte) int32 {
	l := len(data)
	switch l {
	case 1:
		return int32(int8(data[0]))
	case 2:
		return int32(binary.BigEndian.Uint16(data))
	case 4:
		return int32(binary.BigEndian.Uint32(data))
	default:
		return 0
	}
}

// bytes_to_int64 converts a byte slice to a 64-bit integer.
func bytes_to_int64(data []byte) int64 {
	l := len(data)
	switch l {
	case 1:
		return int64(int8(data[0]))
	case 2:
		return int64(binary.BigEndian.Uint16(data))
	case 4:
		return int64(binary.BigEndian.Uint32(data))
	case 8:
		return int64(binary.BigEndian.Uint64(data))
	default:
		return 0
	}
}

// bytes_to_uint converts a byte slice to a 64-bit unsigned integer.
func bytes_to_uint(data []byte) uint {
	l := len(data)
	switch l {
	case 1:
		return uint(data[0])
	case 2:
		return uint(binary.BigEndian.Uint16(data))
	case 4:
		return uint(binary.BigEndian.Uint32(data))
	case 8:
		return uint(binary.BigEndian.Uint64(data))
	default:
		return 0
	}
}

// bytes_to_uint16 converts a byte slice to a 16-bit unsigned integer.
func bytes_to_uint16(data []byte) uint16 {
	l := len(data)
	switch l {
	case 1:
		return uint16(data[0])
	case 2:
		return uint16(binary.BigEndian.Uint16(data))
	case 4:
		return uint16(binary.BigEndian.Uint32(data))
	case 8:
		return uint16(binary.BigEndian.Uint64(data))
	default:
		return 0
	}
}

// bytes_to_uint32 converts a byte slice to a 32-bit unsigned integer.
func bytes_to_uint32(data []byte) uint32 {
	l := len(data)
	switch l {
	case 1:
		return uint32(data[0])
	case 2:
		return uint32(binary.BigEndian.Uint16(data))
	case 4:
		return uint32(binary.BigEndian.Uint32(data))
	case 8:
		return uint32(binary.BigEndian.Uint64(data))
	default:
		return 0
	}
}

// bytes_to_uint64 converts a byte slice to a 64-bit unsigned integer.
func bytes_to_uint64(data []byte) uint64 {
	l := len(data)
	switch l {
	case 1:
		return uint64(data[0])
	case 2:
		return uint64(binary.BigEndian.Uint16(data))
	case 4:
		return uint64(binary.BigEndian.Uint32(data))
	case 8:
		return uint64(binary.BigEndian.Uint64(data))
	default:
		return 0
	}
}

// bytes_to_float32 converts a byte slice to a 32-bit floating-point number.
func bytes_to_float32(data []byte) float32 {
	l := len(data)
	switch l {
	case 1:
		return math.Float32frombits(uint32(data[0]))
	case 2:
		return math.Float32frombits(uint32(binary.BigEndian.Uint16(data)))
	case 4:
		return math.Float32frombits(binary.BigEndian.Uint32(data))
	case 8:
		return math.Float32frombits(uint32(binary.BigEndian.Uint64(data)))
	default:
		return 0
	}
}

// BytesToFloat64 converts a byte slice to a 64-bit floating-point number.
func bytes_to_float64(data []byte) float64 {
	bits := 0
	l := len(data)
	switch l {
	case 1:
		bits = int(data[0])
	case 2:
		bits = int(binary.BigEndian.Uint16(data))
	case 4:
		bits = int(binary.BigEndian.Uint32(data))
	case 8:
		bits = int(binary.BigEndian.Uint64(data))
	default:
		return 0
	}
	return math.Float64frombits(uint64(bits))
}

// BytesToBool converts a byte slice to a boolean value.
func bytes_to_bool(data []byte) bool {
	return data[0] != 0
}

// bytes_to_byte converts a byte slice to a byte value.
func bytes_to_byte(data []byte) byte {
	return data[0]
}

// bytes_to_int8 converts a byte slice to a 8-bit integer.
func bytes_to_int8(data []byte) int8 {
	return int8(data[0])
}

// bytes_to_uint8 converts a byte slice to a 8-bit unsigned integer.
func bytes_to_uint8(data []byte) uint8 {
	return uint8(data[0])
}

// bytes_to_complex64 converts a byte slice to a 64-bit complex number.
func bytes_to_complex64(data []byte) complex64 {
	return complex(bytes_to_float32(data[:4]), bytes_to_float32(data[4:]))
}

// bytes_to_complex128 converts a byte slice to a 128-bit complex number.
func bytes_to_complex128(data []byte) complex128 {
	return complex(bytes_to_float64(data[:8]), bytes_to_float64(data[8:]))
}

// bytes_to_uintptr converts a byte slice to a uintptr value.
func bytes_to_uintptr(data []byte) uintptr {
	l := len(data)
	switch l {
	case 1:
		return uintptr(data[0])
	case 2:
		return uintptr(binary.BigEndian.Uint16(data))
	case 4:
		return uintptr(binary.BigEndian.Uint32(data))
	case 8:
		return uintptr(binary.BigEndian.Uint64(data))
	default:
		return 0
	}
}

// bytes_to_rune converts a byte slice to a rune value.
func bytes_to_rune(data []byte) string {
	l := len(data)
	switch l {
	case 1:
		return string(int32(data[0]))
	case 2:
		return string(int32(binary.BigEndian.Uint16(data)))
	case 4:
		return string(int32(binary.BigEndian.Uint32(data)))
	case 8:
		return string(int32(binary.BigEndian.Uint64(data)))
	default:
		return ""
	}
}

// slice_byte_to_string converts a byte slice to a slice of byte values.
func slice_byte_to_string(data []byte) string {
	s := []string{}
	for _, v := range data {
		s = append(s, fmt.Sprintf("%d", v))
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}

func slice_to_string(data []byte) string {
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
		strs = append(strs, slice_to_string(vv))
		pos += vl
	}
	return strs
}

// slice_int16_to_string converts a byte slice to a slice of 16-bit integer values.
func slice_int16_to_string(data []byte) string {
	s := []string{}
	// 2字节为一个int16
	for i := 0; i < len(data); i += 2 {
		s = append(s, fmt.Sprintf("%d", bytes_to_int16(data[i:i+2])))
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}

func conv_to_slice_int16(data []byte) []int16 {
	s := []int16{}
	// 2字节为一个int16
	for i := 0; i < len(data); i += 2 {
		s = append(s, bytes_to_int16(data[i:i+2]))
	}
	return s
}

func slice_uint16_to_string(data []byte) string {
	s := []string{}
	// 2字节为一个uint16
	for i := 0; i < len(data); i += 2 {
		s = append(s, fmt.Sprintf("%d", bytes_to_uint16(data[i:i+2])))
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}

func slice_float32_to_string(data []byte) string {
	s := []string{}
	// 4字节为一个float32
	for i := 0; i < len(data); i += 4 {
		s = append(s, fmt.Sprintf("%f", bytes_to_float32(data[i:i+4])))
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}
func slice_float64_to_string(data []byte) string {
	s := []string{}
	// 8字节为一个float64
	for i := 0; i < len(data); i += 8 {
		s = append(s, fmt.Sprintf("%f", bytes_to_float64(data[i:i+8])))
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
		s = append(s, bytes_to_uint16(data[i:i+2]))
	}
	return s
}

func conv_to_slice_uint32(data []byte) []uint32 {
	s := []uint32{}
	// 4字节为一个uint32
	for i := 0; i < len(data); i += 4 {
		s = append(s, bytes_to_uint32(data[i:i+4]))
	}
	return s
}

func conv_to_slice_int32(data []byte) []int32 {
	s := []int32{}
	// 4字节为一个int32
	for i := 0; i < len(data); i += 4 {
		s = append(s, bytes_to_int32(data[i:i+4]))
	}
	return s
}

func conv_to_slice_float32(data []byte) []float32 {
	s := []float32{}
	// 4字节为一个float32
	for i := 0; i < len(data); i += 4 {
		s = append(s, bytes_to_float32(data[i:i+4]))
	}
	return s
}

func conv_to_slice_float64(data []byte) []float64 {
	s := []float64{}
	// 8字节为一个float64
	for i := 0; i < len(data); i += 8 {
		s = append(s, bytes_to_float64(data[i:i+8]))
	}
	return s
}

func conv_to_slice_int(data []byte) []int {
	s := []int{}
	// 8字节为一个int64
	for i := 0; i < len(data); i += 8 {
		s = append(s, int(bytes_to_int64(data[i:i+8])))
	}
	return s
}
func conv_to_slice_int64(data []byte) []int64 {
	s := []int64{}
	// 8字节为一个int64
	for i := 0; i < len(data); i += 8 {
		s = append(s, bytes_to_int64(data[i:i+8]))
	}
	return s
}
func conv_to_slice_uint(data []byte) []uint {
	s := []uint{}
	// 8字节为一个uint64
	for i := 0; i < len(data); i += 8 {
		s = append(s, bytes_to_uint(data[i:i+8]))
	}
	return s
}
func conv_to_slice_uint64(data []byte) []uint64 {
	s := []uint64{}
	// 8字节为一个uint64
	for i := 0; i < len(data); i += 8 {
		s = append(s, bytes_to_uint64(data[i:i+8]))
	}
	return s
}

// slice_int32_to_string converts a byte slice to a slice of 32-bit integer values.
func slice_int32_to_string(data []byte) string {
	s := []string{}
	// 4字节为一个int32
	for i := 0; i < len(data); i += 4 {
		s = append(s, fmt.Sprintf("%d", bytes_to_int32(data[i:i+4])))
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}

func slice_uint32_to_string(data []byte) string {
	s := []string{}
	// 4字节为一个uint32
	for i := 0; i < len(data); i += 4 {
		s = append(s, fmt.Sprintf("%d", bytes_to_uint32(data[i:i+4])))
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}

// slice_int64_to_string converts a byte slice to a slice of 64-bit integer values.
func slice_int64_to_string(data []byte) string {
	s := []string{}
	// 8字节为一个int64
	for i := 0; i < len(data); i += 8 {
		s = append(s, fmt.Sprintf("%d", bytes_to_int64(data[i:i+8])))
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}

// slice_uint64_to_string converts a byte slice to a slice of 64-bit unsigned integer values.
func slice_uint64_to_string(data []byte) string {
	s := []string{}
	// 8字节为一个uint64
	for i := 0; i < len(data); i += 8 {
		s = append(s, fmt.Sprintf("%d", bytes_to_uint64(data[i:i+8])))
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}

// SliceStringToString converts a byte slice to a slice of string values.
func slice_string_to_string(v []byte, opt *Option) string {
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
