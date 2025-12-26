package utils

import (
	"encoding/binary"
	"math"
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
