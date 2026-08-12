package ag

import "encoding/binary"

func zeroExtendN(b []byte, n int) []byte {
	l := len(b)
	out := make([]byte, n)
	if l >= n {
		copy(out, b[l-n:])
		return out
	}
	copy(out[n-l:], b)
	return out
}

func zeroExtend8byte(b []byte) []byte { return zeroExtendN(b, 8) }
func zeroExtend4byte(b []byte) []byte { return zeroExtendN(b, 4) }
func zeroExtend2byte(b []byte) []byte { return zeroExtendN(b, 2) }

func to_int8(b []byte) int8 {
	return int8(zeroExtendN(b, 1)[0])
}
func to_uint8(b []byte) uint8 {
	return uint8(zeroExtendN(b, 1)[0])
}
func to_int16(b []byte) int16 {
	return int16(binary.BigEndian.Uint16(zeroExtend2byte(b)))
}
func to_int32(b []byte) int32 {
	return int32(binary.BigEndian.Uint32(zeroExtend4byte(b)))
}
func to_int64(b []byte) int64 {
	return int64(binary.BigEndian.Uint64(zeroExtend8byte(b)))
}

func to_uint16(b []byte) uint16 {
	return binary.BigEndian.Uint16(zeroExtend2byte(b))
}
func to_uint32(b []byte) uint32 {
	return binary.BigEndian.Uint32(zeroExtend4byte(b))
}
func to_uint64(b []byte) uint64 {
	return binary.BigEndian.Uint64(zeroExtend8byte(b))
}

func int_to_byte(i int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(i))
	// big endian  最高位在最前面 如果前面是0，则不编码（简易压缩）
	// [0 0 0 0 0 0 128 0] -> [128,0]
	// [0 0 0 0 0 0 1 1] -> [1,1]
	// [0 0 0 0 0 0 0 0] -> [0]
	pos := 0
	for pos < 7 && b[pos] == 0 {
		pos++
	}
	if pos == 0 {
		return b
	}
	out := make([]byte, 8-pos)
	copy(out, b[pos:])
	return out
}

func uint_to_byte(u uint64) []byte {
	return int_to_byte(int64(u))
}
