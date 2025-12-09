package utils

import (
	"encoding/binary"
	"math"
	"reflect"
)

func GetType(name string, data []byte) reflect.Value {
	// []string{"int", "int32", "int64", "uint", "uint32", "uint64", "float32", "float64", "string", "uint8", "bool"}
	switch name {
	case "int":
		return reflect.ValueOf(BytesToInt(data))
	case "int32":
		return reflect.ValueOf(BytesToInt32(data))
	case "int64":
		return reflect.ValueOf(BytesToInt64(data))
	case "uint":
		return reflect.ValueOf(BytesToUint(data))
	case "uint32":
		return reflect.ValueOf(BytesToUint32(data))
	case "uint64":
		return reflect.ValueOf(BytesToUint64(data))
	case "float32":
		return reflect.ValueOf(BytesToFloat32(data))
	case "float64":
		return reflect.ValueOf(BytesToFloat64(data))
	case "string":
		return reflect.ValueOf(string(data))
	case "uint8":
		return reflect.ValueOf(data[0])
	case "bool":
		return reflect.ValueOf(BytesToBool(data))
	default:
		return reflect.ValueOf(data)
	}
}

func BytesToInt(data []byte) int {
	return int(binary.BigEndian.Uint32(data))
}

func BytesToInt32(data []byte) int32 {
	return int32(binary.BigEndian.Uint32(data))
}

func BytesToInt64(data []byte) int64 {
	return int64(binary.BigEndian.Uint64(data))
}

func BytesToUint32(data []byte) uint32 {
	return binary.BigEndian.Uint32(data)
}
func BytesToUint(data []byte) uint {
	return uint(binary.BigEndian.Uint32(data))
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
