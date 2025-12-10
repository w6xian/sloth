package utils

import (
	"encoding/binary"
	"math"
	"reflect"
)

func GetType(needPtr bool, name string, data []byte) reflect.Value {
	// []string{"int", "int32", "int64", "uint", "uint32", "uint64", "float32", "float64", "string", "uint8", "bool"}

	switch name {
	case "int":
		by := BytesToInt(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "int32":
		by := BytesToInt32(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "int64":
		by := BytesToInt64(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uint":
		by := BytesToUint(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uint32":
		by := BytesToUint32(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "uint64":
		by := BytesToUint64(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "float32":
		by := BytesToFloat32(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	case "float64":
		by := BytesToFloat64(data)
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
	case "bool":
		by := BytesToBool(data)
		if needPtr {
			return reflect.ValueOf(&by)
		}
		return reflect.ValueOf(by)
	default:
		return reflect.ValueOf(data)
	}
}

func BytesToInt(data []byte) int {
	r := binary.BigEndian.Uint64(data)
	return int(r)
}

func BytesToInt32(data []byte) int32 {
	return int32(binary.BigEndian.Uint64(data))
}

func BytesToInt64(data []byte) int64 {
	return int64(binary.BigEndian.Uint64(data))
}

func BytesToUint32(data []byte) uint32 {
	return binary.BigEndian.Uint32(data)
}
func BytesToUint(data []byte) uint {
	return uint(binary.BigEndian.Uint64(data))
}

func BytesToUint64(data []byte) uint64 {
	return binary.BigEndian.Uint64(data)
}

func BytesToFloat32(data []byte) float32 {
	return math.Float32frombits(binary.BigEndian.Uint32(data))
}

func BytesToFloat64(data []byte) float64 {
	return math.Float64frombits(binary.BigEndian.Uint64(data))
}

func BytesToBool(data []byte) bool {
	return data[0] != 0
}
