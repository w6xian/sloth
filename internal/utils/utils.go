package utils

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
)

func Serialize(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	return b
}

func Deserialize(b []byte, v any) error {
	err := json.Unmarshal(b, v)
	if err != nil {
		return err
	}
	return nil
}

type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

func Max[T Number](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func Min[T Number](a, b T) T {
	if a < b {
		return a
	}
	return b
}

func AnyToBytes(i any) ([]byte, error) {
	if i == nil {
		return nil, nil
	}
	if err, ok := i.(error); ok {
		return []byte(err.Error()), nil
	}
	v := reflect.ValueOf(i)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, nil
		}
		return AnyToBytes(v.Elem().Interface())
	}
	switch dt := i.(type) {
	case string:
		return []byte(dt), nil
	case []byte:
		out := make([]byte, len(dt))
		copy(out, dt)
		return out, nil
	case int:
		return []byte(strconv.FormatInt(int64(dt), 10)), nil
	case int8:
		return []byte(strconv.FormatInt(int64(dt), 10)), nil
	case int16:
		return []byte(strconv.FormatInt(int64(dt), 10)), nil
	case int32:
		return []byte(strconv.FormatInt(int64(dt), 10)), nil
	case int64:
		return []byte(strconv.FormatInt(int64(dt), 10)), nil
	case uint:
		return []byte(strconv.FormatUint(uint64(dt), 10)), nil
	case uint8:
		return []byte(strconv.FormatUint(uint64(dt), 10)), nil
	case uint16:
		return []byte(strconv.FormatUint(uint64(dt), 10)), nil
	case uint32:
		return []byte(strconv.FormatUint(uint64(dt), 10)), nil
	case uint64:
		return []byte(strconv.FormatUint(uint64(dt), 10)), nil
	case uintptr:
		return []byte(strconv.FormatUint(uint64(dt), 10)), nil
	case float32:
		return []byte(strconv.FormatFloat(float64(dt), 'f', -1, 32)), nil
	case float64:
		return []byte(strconv.FormatFloat(float64(dt), 'f', -1, 64)), nil
	case complex64:
		return fmt.Appendf(nil, "(%g+%gi)", real(dt), imag(dt)), nil
	case complex128:
		return fmt.Appendf(nil, "(%g+%gi)", real(dt), imag(dt)), nil
	case bool:
		return fmt.Appendf(nil, "%v", dt), nil
	}
	rst, err := json.Marshal(i)
	if err != nil {
		return nil, err
	}
	return rst, nil
}

func AnyToStr(i any) (string, error) {
	if i == nil {
		return "", nil
	}
	if err, ok := i.(error); ok {
		return err.Error(), nil
	}
	if bs, ok := i.([]byte); ok {
		return string(bs), nil
	}
	if s, ok := i.(string); ok {
		return s, nil
	}
	v := reflect.ValueOf(i)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return "", nil
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.String:
		return v.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.Float32:
		return strconv.FormatFloat(v.Float(), 'f', -1, 32), nil
	case reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64), nil
	case reflect.Complex64:
		return fmt.Sprintf("(%g+%gi)", real(v.Complex()), imag(v.Complex())), nil
	case reflect.Complex128:
		return fmt.Sprintf("(%g+%gi)", real(v.Complex()), imag(v.Complex())), nil
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return string(v.Bytes()), nil
		}
		str, err := json.Marshal(i)
		if err != nil {
			return "", err
		}
		return string(str), nil
	case reflect.Map, reflect.Struct, reflect.Array:
		str, err := json.Marshal(i)
		if err != nil {
			return "", err
		}
		return string(str), nil
	default:
		return "", fmt.Errorf("unable to cast %#v (type=%T kind=%s) to string", i, i, v.Kind())
	}
}

func MustAnyToBytes(i any) []byte {
	b, err := AnyToBytes(i)
	if err != nil {
		return nil
	}
	return b
}

func MustAnyToStr(i any) string {
	s, err := AnyToStr(i)
	if err != nil {
		return ""
	}
	return s
}
