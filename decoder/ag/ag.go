package ag

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"

	"github.com/w6xian/sloth/v3/internal/utils"
)

/**
 * @brief AG 协议 (Argument Grid) 参数帧格式
 *
 * MAGIC  :p   2 byte   0x3A 0x70  (ASCII ":p")
 * TYPE   t    1 byte   ArgumentType* 枚举
 * LEN    l    2 byte   big endian，Value 字节数 (0~65535)
 * VALUE  d    l byte   payload，长度 = l
 *
 * 总帧最小 5 字节，最大 5 + 65535 = 65540 字节。
 */

const (
	ArgumentMagic1      byte = 0x3A // ':'
	ArgumentMagic2      byte = 0x70 // 'p'
	ArgumentHeaderSize       = 2 + 1 + 2
	ArgumentMaxDataSize      = 1 << 16
)

// 基本类型穷举（与 Go 原语一一对应，0x01~0x1F 为基础标量；0x20~0x3F 为复合/扩展）
const (
	ArgumentTypeNil uint8 = iota + 1
	ArgumentTypeBool

	ArgumentTypeInt
	ArgumentTypeInt8
	ArgumentTypeInt16
	ArgumentTypeInt32
	ArgumentTypeInt64

	ArgumentTypeUint
	ArgumentTypeUint8
	ArgumentTypeUint16
	ArgumentTypeUint32
	ArgumentTypeUint64
	ArgumentTypeUintptr

	ArgumentTypeFloat32
	ArgumentTypeFloat64

	ArgumentTypeComplex64
	ArgumentTypeComplex128

	ArgumentTypeString
	ArgumentTypeBytes

	ArgumentTypeSlice
	ArgumentTypeMap
	ArgumentTypeStruct
	ArgumentTypeCustom
)

var (
	ErrAgTooShort       = errors.New("ag: payload too short for header")
	ErrAgBadMagic       = errors.New("ag: bad magic header, expect :p")
	ErrAgLengthMismatch = errors.New("ag: payload length mismatch")
	ErrAgDataTooLarge   = fmt.Errorf("ag: data length exceeds %d", ArgumentMaxDataSize)
	ErrAgUnknownType    = errors.New("ag: unknown type tag")
	ErrAgInvalidHeader  = errors.New("ag: invalid header")
)

// IsArgument O(1) 校验帧完整性（magic + length 匹配）
func IsArgument(b []byte) bool {
	if len(b) < ArgumentHeaderSize || b[0] != ArgumentMagic1 || b[1] != ArgumentMagic2 {
		return false
	}
	t := b[2]
	_ = t
	length := binary.BigEndian.Uint16(b[3:5])
	return len(b) == ArgumentHeaderSize+int(length)
}

// Data 取 Value 段；非 AG 帧或不合法返回源切片（兼容旧调用方直接透传）
func Data(b []byte) []byte {
	if !IsArgument(b) {
		return b
	}
	return get_data(b)
}

func Value(b []byte) []byte {
	return Data(b)
}

func Json(v any) []byte {
	d, err := Encode(v)
	if err != nil {
		d, err = json.Marshal(v)
		if err != nil {
			return []byte{}
		}
	}
	return d
}

func get_data(b []byte) []byte {
	length := binary.BigEndian.Uint16(b[3:5])
	if length == 0 {
		return nil
	}
	out := make([]byte, length)
	copy(out, b[ArgumentHeaderSize:ArgumentHeaderSize+int(length)])
	t := b[2]
	switch t {
	case ArgumentTypeUint8, ArgumentTypeInt8:
		return zeroExtendN(out, 1)
	case ArgumentTypeUint16, ArgumentTypeInt16:
		return zeroExtend2byte(out)
	case ArgumentTypeUint32, ArgumentTypeInt32:
		return zeroExtend4byte(out)
	case ArgumentTypeUint64, ArgumentTypeInt64, ArgumentTypeInt, ArgumentTypeUint, ArgumentTypeUintptr:
		return zeroExtend8byte(out)
	}
	return out
}

// Validate 纯验证；全部通过返回 nil
func Validate(b []byte) error {
	if len(b) < ArgumentHeaderSize {
		return ErrAgTooShort
	}
	if b[0] != ArgumentMagic1 || b[1] != ArgumentMagic2 {
		return ErrAgBadMagic
	}
	length := binary.BigEndian.Uint16(b[3:5])
	if int(length) > ArgumentMaxDataSize {
		return ErrAgDataTooLarge
	}
	if len(b) != ArgumentHeaderSize+int(length) {
		return ErrAgLengthMismatch
	}
	return nil
}

func get_frame(b []byte) (byte, []byte, error) {
	if err := Validate(b); err != nil {
		return 0, nil, err
	}
	t := b[2]
	v := get_data(b)
	return t, v, nil
}

// Bytes 把任意值按类型编码为一帧 AG；标量走原语编码，复合走 json fallback 映射成 String 帧。
func Encode(arg any) ([]byte, error) {
	if arg == nil {
		return encode_ag(ArgumentTypeNil, nil)
	}
	t := typeof(arg)
	switch t {
	case ArgumentTypeBool:
		if arg.(bool) {
			return encode_ag(t, []byte{1})
		}
		return encode_ag(t, []byte{0})

	case ArgumentTypeInt:
		return encode_ag(t, int_to_byte(int64(arg.(int))))
	case ArgumentTypeInt8:
		return encode_ag(t, int_to_byte(int64(arg.(int8))))
	case ArgumentTypeInt16:
		return encode_ag(t, int_to_byte(int64(arg.(int16))))
	case ArgumentTypeInt32:
		return encode_ag(t, int_to_byte(int64(arg.(int32))))
	case ArgumentTypeInt64:
		return encode_ag(t, int_to_byte(arg.(int64)))

	case ArgumentTypeUint:
		return encode_ag(t, uint_to_byte(uint64(arg.(uint))))
	case ArgumentTypeUint8:
		return encode_ag(t, uint_to_byte(uint64(arg.(uint8))))
	case ArgumentTypeUint16:
		return encode_ag(t, uint_to_byte(uint64(arg.(uint16))))
	case ArgumentTypeUint32:
		return encode_ag(t, uint_to_byte(uint64(arg.(uint32))))
	case ArgumentTypeUint64:
		return encode_ag(t, uint_to_byte(arg.(uint64)))
	case ArgumentTypeUintptr:
		return encode_ag(t, uint_to_byte(uint64(arg.(uintptr))))

	case ArgumentTypeFloat32:
		return encode_ag(t, binary.LittleEndian.AppendUint32(nil, math.Float32bits(arg.(float32))))
	case ArgumentTypeFloat64:
		return encode_ag(t, binary.LittleEndian.AppendUint64(nil, math.Float64bits(arg.(float64))))

	case ArgumentTypeComplex64:
		c := arg.(complex64)
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint32(buf[0:4], math.Float32bits(real(c)))
		binary.LittleEndian.PutUint32(buf[4:8], math.Float32bits(imag(c)))
		return encode_ag(t, buf)
	case ArgumentTypeComplex128:
		c := arg.(complex128)
		buf := make([]byte, 16)
		binary.LittleEndian.PutUint64(buf[0:8], math.Float64bits(real(c)))
		binary.LittleEndian.PutUint64(buf[8:16], math.Float64bits(imag(c)))
		return encode_ag(t, buf)

	case ArgumentTypeString:
		return encode_ag(t, []byte(arg.(string)))
	case ArgumentTypeBytes:
		b := arg.([]byte)
		out := make([]byte, len(b))
		copy(out, b)
		return encode_ag(t, out)

	case ArgumentTypeSlice, ArgumentTypeMap, ArgumentTypeStruct:
		s, err := jsonMarshalFallback(arg)
		if err != nil {
			return nil, err
		}
		return encode_ag(ArgumentTypeString, []byte(s))
	}
	// 兜底：json 字符串化
	return encode_ag(ArgumentTypeCustom, utils.Serialize(arg))
}

func Decode(b []byte) (any, error) {
	if !IsArgument(b) {
		return nil, ErrAgInvalidHeader
	}
	return get_value(b)
}

func Decoder(b []byte) ([]byte, error) {
	if !IsArgument(b) {
		return b, nil
	}
	return get_data(b), nil
}
func Encoder(arg any) ([]byte, error) {
	return Encode(arg)
}

// typeof 穷举 Go 原语，返回 ArgumentType* 常量；标量之外走 AnyToBytes 的 JSON 路径映射成 String/Bytes。
func typeof(arg any) uint8 {
	if arg == nil {
		return ArgumentTypeNil
	}
	switch arg.(type) {
	case bool:
		return ArgumentTypeBool
	case int:
		return ArgumentTypeInt
	case int8:
		return ArgumentTypeInt8
	case int16:
		return ArgumentTypeInt16
	case int32:
		return ArgumentTypeInt32
	case int64:
		return ArgumentTypeInt64
	case uint:
		return ArgumentTypeUint
	case uint8:
		return ArgumentTypeUint8
	case uint16:
		return ArgumentTypeUint16
	case uint32:
		return ArgumentTypeUint32
	case uint64:
		return ArgumentTypeUint64
	case uintptr:
		return ArgumentTypeUintptr
	case float32:
		return ArgumentTypeFloat32
	case float64:
		return ArgumentTypeFloat64
	case complex64:
		return ArgumentTypeComplex64
	case complex128:
		return ArgumentTypeComplex128
	case string:
		return ArgumentTypeString
	case []byte:
		return ArgumentTypeBytes
	}
	rv := reflect.ValueOf(arg)
	switch rv.Kind() {
	case reflect.Slice:
		return ArgumentTypeSlice
	case reflect.Map:
		return ArgumentTypeMap
	case reflect.Struct:
		return ArgumentTypeStruct
	}
	return ArgumentTypeCustom
}

// Encode 写一帧；Length 超 65535 返回 ErrAgDataTooLarge
func encode_ag(t uint8, data []byte) ([]byte, error) {
	if len(data) > ArgumentMaxDataSize {
		return nil, ErrAgDataTooLarge
	}
	out := make([]byte, ArgumentHeaderSize+len(data))
	out[0] = ArgumentMagic1
	out[1] = ArgumentMagic2
	out[2] = t
	binary.BigEndian.PutUint16(out[3:5], uint16(len(data)))
	copy(out[ArgumentHeaderSize:], data)
	return out, nil
}

func get_value(b []byte) (any, error) {
	t, v, err := get_frame(b)
	if err != nil {
		return nil, err
	}
	switch t {
	case ArgumentTypeNil:
		return nil, nil
	case ArgumentTypeBool:
		if len(v) == 0 {
			return false, nil
		}
		return v[0] != 0, nil

	case ArgumentTypeInt, ArgumentTypeInt8, ArgumentTypeInt16, ArgumentTypeInt32, ArgumentTypeInt64:
		n := to_int64(v)
		switch t {
		case ArgumentTypeInt:
			return int(n), nil
		case ArgumentTypeInt8:
			return int8(n), nil
		case ArgumentTypeInt16:
			return int16(n), nil
		case ArgumentTypeInt32:
			return int32(n), nil
		case ArgumentTypeInt64:
			return n, nil
		}

	case ArgumentTypeUint, ArgumentTypeUint8, ArgumentTypeUint16, ArgumentTypeUint32, ArgumentTypeUint64, ArgumentTypeUintptr:
		u := to_uint64(v)
		switch t {
		case ArgumentTypeUint:
			return uint(u), nil
		case ArgumentTypeUint8:
			return uint8(u), nil
		case ArgumentTypeUint16:
			return uint16(u), nil
		case ArgumentTypeUint32:
			return uint32(u), nil
		case ArgumentTypeUint64:
			return u, nil
		case ArgumentTypeUintptr:
			return uintptr(u), nil
		}

	case ArgumentTypeFloat32:
		if len(v) != 4 {
			return nil, ErrAgLengthMismatch
		}
		return math.Float32frombits(binary.LittleEndian.Uint32(v)), nil
	case ArgumentTypeFloat64:
		if len(v) != 8 {
			return nil, ErrAgLengthMismatch
		}
		return math.Float64frombits(binary.LittleEndian.Uint64(v)), nil

	case ArgumentTypeComplex64:
		if len(v) != 8 {
			return nil, ErrAgLengthMismatch
		}
		re := math.Float32frombits(binary.LittleEndian.Uint32(v[0:4]))
		im := math.Float32frombits(binary.LittleEndian.Uint32(v[4:8]))
		return complex64(complex(float64(re), float64(im))), nil
	case ArgumentTypeComplex128:
		if len(v) != 16 {
			return nil, ErrAgLengthMismatch
		}
		re := math.Float64frombits(binary.LittleEndian.Uint64(v[0:8]))
		im := math.Float64frombits(binary.LittleEndian.Uint64(v[8:16]))
		return complex128(complex(re, im)), nil

	case ArgumentTypeString:
		return string(v), nil
	case ArgumentTypeBytes:
		out := make([]byte, len(v))
		copy(out, v)
		return out, nil
	}
	return nil, ErrAgUnknownType
}
