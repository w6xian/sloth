package ref

import (
	"encoding/binary"
	"math"
	"reflect"
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

func get_param_type(needPtr bool, name string, data []byte) reflect.Value {
	// []string{"int", "int32", "int64", "uint", "uint32", "uint64", "float32", "float64", "string", "uint8", "bool"}
	switch name {
	case "int":
		by := bytes_to_int(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "int16":
		by := bytes_to_int16(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "int32":
		by := bytes_to_int32(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "int64":
		by := bytes_to_int64(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uint":
		by := bytes_to_uint(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uint16":
		by := bytes_to_uint16(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uint32":
		by := bytes_to_uint32(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uint64":
		by := bytes_to_uint64(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "float32":
		by := bytes_to_float32(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "float64":
		by := bytes_to_float64(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "string":
		str := string(data)
		if needPtr {
			return reflect.ValueOf(&str)
		}
		return reflect.ValueOf(str)
	case "uint8":
		by := data[0]
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "int8":
		by := int8(data[0])
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uintptr":
		by := bytes_to_uintptr(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "rune":
		by := bytes_to_int(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(rune(by))
	case "bool":
		by := bytes_to_bool(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	default:
		return reflect.ValueOf(data)
	}
}
