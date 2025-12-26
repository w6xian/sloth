package tlv

import (
	"bytes"
	"encoding/binary"
)

// Uint2Bytes converts an uint64 to a byte slice.
func Int16ToBytes(i int16) []byte {
	data := int16(i)
	buffer := bytes.NewBuffer([]byte{})
	binary.Write(buffer, binary.BigEndian, data) // Big-endian encoding
	return buffer.Bytes()
}

// Uint2Bytes converts an uint64 to a byte slice.
func Uint16ToBytes(i uint16) []byte {
	// Convert int to int32 for consistent size
	data := uint16(i)
	buffer := bytes.NewBuffer([]byte{})
	binary.Write(buffer, binary.BigEndian, data) // Big-endian encoding
	return buffer.Bytes()
}

func IntToBytes(n int64) []byte {
	// Convert int to int32 for consistent size
	data := int64(n)
	buffer := bytes.NewBuffer([]byte{})
	binary.Write(buffer, binary.BigEndian, data) // Big-endian encoding
	return buffer.Bytes()
}
func UintToBytes(n uint64) []byte {
	// Convert int to int32 for consistent size
	data := uint64(n)
	buffer := bytes.NewBuffer([]byte{})
	binary.Write(buffer, binary.BigEndian, data) // Big-endian encoding
	return buffer.Bytes()
}

// Uint32ToBytes converts an uint32 to a byte slice.
func Uint32ToBytes(i uint32) []byte {
	// Convert int to int32 for consistent size
	data := uint64(i)
	buffer := bytes.NewBuffer([]byte{})
	binary.Write(buffer, binary.BigEndian, data) // Big-endian encoding
	return buffer.Bytes()
}

// Int32ToBytes converts an int32 to a byte slice.
func Int32ToBytes(i int32) []byte {
	// Convert int to int32 for consistent size
	data := int32(i)
	buffer := bytes.NewBuffer([]byte{})
	binary.Write(buffer, binary.BigEndian, data) // Big-endian encoding
	return buffer.Bytes()
}
