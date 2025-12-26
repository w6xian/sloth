package utils

import (
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
