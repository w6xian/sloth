package utils

import (
	"encoding/json"
	"fmt"
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
	fmt.Printf("%v%v", m, j[col])
	return m
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

func JsonString(v any) string {
	t := reflect.TypeOf(v)
	b, err := json.Marshal(v)
	if err != nil {
		switch t.Kind() {
		case reflect.Slice:
			return "[]"
		case reflect.Map:
			return "{}"
		case reflect.Bool:
			return strconv.FormatBool(v.(bool))
		case reflect.String:
			return v.(string)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return strconv.FormatInt(v.(int64), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return strconv.FormatUint(v.(uint64), 10)
		case reflect.Float32, reflect.Float64:
			return strconv.FormatFloat(v.(float64), 'f', -1, 64)
		default:
			return ""
		}

	}
	return string(b)
}
