package utils

import (
	"encoding/json"
	"reflect"
	"strconv"
)

type JsonValue map[string]*json.RawMessage

func (j JsonValue) String(col string) string {
	var str string
	if j[col] != nil {
		json.Unmarshal(*j[col], &str)
	}
	return str
}
func (j JsonValue) Bytes(col string) []byte {
	var b []byte
	if j[col] != nil {
		json.Unmarshal(*j[col], &b)
	}
	return b
}
func (j JsonValue) BytesArray(col string) [][]byte {
	var b [][]byte
	if j[col] != nil {
		json.Unmarshal(*j[col], &b)
	}
	return b
}

func (j JsonValue) MapString(col string) map[string]string {
	var m = map[string]string{}
	if j[col] != nil {
		json.Unmarshal(*j[col], &m)
	}
	return m
}

func (j JsonValue) Int64(col string) int64 {
	var i int64
	if j[col] != nil {
		err := json.Unmarshal(*j[col], &i)
		if err != nil {
			str := j.String(col)
			i, _ = strconv.ParseInt(str, 10, 64)
		}
	}
	return i
}

func (j JsonValue) Ints64(col string) []int64 {
	var i []int64
	if j[col] != nil {
		json.Unmarshal(*j[col], &i)

	}
	return i
}

func (j JsonValue) Uint64(col string) uint64 {
	var i uint64
	if j[col] != nil {
		err := json.Unmarshal(*j[col], &i)
		if err != nil {
			str := j.String(col)
			i, _ = strconv.ParseUint(str, 10, 64)
		}
	}
	return i
}

func (j JsonValue) Uints64(col string) []uint64 {
	var i []uint64
	if j[col] != nil {
		json.Unmarshal(*j[col], &i)

	}
	return i
}

func (j JsonValue) Ints(col string) []int {
	var i []int
	if j[col] != nil {
		json.Unmarshal(*j[col], &i)

	}
	return i
}

func (j JsonValue) Int(col string) int {
	return int(j.Int64(col))
}

func (j JsonValue) MapSI(col string) map[string]interface{} {
	var m = map[string]interface{}{}
	if j[col] != nil {
		json.Unmarshal(*j[col], &m)
	}
	return m
}
func (j JsonValue) MapSS(col string) map[string]string {
	var m = map[string]string{}
	if j[col] != nil {
		json.Unmarshal(*j[col], &m)
	}
	return m
}

func (j JsonValue) MapStringInto(col string, dst map[string]string) {
	if dst == nil {
		return
	}
	for k := range dst {
		delete(dst, k)
	}
	if j[col] != nil {
		json.Unmarshal(*j[col], &dst)
	}
}

func MapToStruct(s any, v any) error {
	if s == nil {
		return nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// JsonString 把任意值转成字符串；json.Marshal 失败时按 Kind 降级格式化。
//
// 降级分支原先是 v.(bool) / v.(int64) 这类直接断言：v 的实际类型是 int、int32、
// float32 时断言会 panic（Kind 相同但类型不同）。改用 reflect.Value 取值，
// 对任何 Kind 都成立，且 nil 不再 panic。
func JsonString(v any) string {
	b, err := json.Marshal(v)
	if err == nil {
		return string(b)
	}
	if v == nil {
		return ""
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice:
		return "[]"
	case reflect.Map:
		return "{}"
	case reflect.Bool:
		return strconv.FormatBool(rv.Bool())
	case reflect.String:
		return rv.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(rv.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(rv.Float(), 'f', -1, 64)
	default:
		return ""
	}
}
