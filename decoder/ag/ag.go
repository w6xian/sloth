package ag

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync/atomic"

	"github.com/w6xian/sloth/v4/utils"
)

/**
 * @brief AG 协议 (Argument Grid) 参数帧格式
 *
 * 短帧（Value ≤ 65534）——与扩展帧出现之前逐字节一致：
 *   MAGIC  :p   2 byte   0x3A 0x70  (ASCII ":p")
 *   TYPE   t    1 byte   ArgumentType* 枚举
 *   LEN    l    2 byte   big endian，Value 字节数 (0~65534)
 *   VALUE  d    l byte   payload，长度 = l
 *
 * 扩展帧（Value ≥ 65535）——LEN 写满值作转义位，真实长度跟在后面：
 *   MAGIC  :p   2 byte
 *   TYPE   t    1 byte
 *   FLAG       2 byte   固定 0xFFFF
 *   LEN32  n   4 byte   big endian，Value 字节数 (65535 ~ MaxDataSize())
 *   VALUE  d   n byte   payload，长度 = n
 *
 * 为什么占用 LEN 满值、而不是新增 type 标签：type 是既有语义（Bytes/String…），
 * 老端碰到未知 tag 的行为不确定；让出 0xFFFF 则 type 一字不动，短帧格式也一字
 * 不动——未升级的对端照样能收发小包。代价是短帧上限从 65535 降到 65534。
 *
 * 兼容性边界（必须知道）：
 *   - 短帧双向兼容，随便混版本；
 *   - 扩展帧**只有两端都升级过才通**。老端收到扩展帧解不开，会按"非 AG 帧"把
 *     带头的原始字节透传上去，属于静默错数据——所以大包链路要两端一起抬上限。
 */

const (
	ArgumentMagic1      byte = 0x3A // ':'
	ArgumentMagic2      byte = 0x70 // 'p'
	ArgumentHeaderSize       = 2 + 1 + 2
	// ArgumentExtHeaderSize 扩展帧头 = 短帧头 + 4 字节长度
	ArgumentExtHeaderSize = ArgumentHeaderSize + 4
	// ArgumentExtLenFlag 短帧 LEN 的转义位：命中即表示后面跟 4 字节真实长度。
	ArgumentExtLenFlag = 1<<16 - 1
	// ArgumentMaxShortData 短帧能表达的 Value 上限（0xFFFF 已被转义位占用，
	// 所以是 65534 而不是 65535）。
	ArgumentMaxShortData = 1<<16 - 2
	// ArgumentMaxDataSize 默认上限。不调 SetMaxDataSize 时行为与扩展帧出现前
	// 一致：Value > 65535 一律拒绝。
	//
	// 上限是 65535 而非 65536：原先写成 1<<16，encode_ag 会接受 65536 字节的
	// Value，再被 PutUint16 截断成 0 → 编出长度 0 的帧，数据静默丢失。
	ArgumentMaxDataSize = 1<<16 - 1
	// MaxAgDataSize SetMaxDataSize 能抬到的硬顶（8MB）。
	//
	// 传输层装得下更多（fn 帧上限 1GB），这里压到 8MB 是内存考量：长度字段由
	// 对端给出，解码侧照它分配，抬太高等于把内存控制权交给对端。
	MaxAgDataSize = 8 << 20
)

// maxDataSize 当前生效的 Value 上限，默认 ArgumentMaxDataSize。
//
// 用原子量：允许启动后再调整，与收发两侧的读不打架。
var maxDataSize atomic.Int64

func init() {
	maxDataSize.Store(ArgumentMaxDataSize)
}

// MaxDataSize 返回当前生效的 Value 上限。
func MaxDataSize() int { return int(maxDataSize.Load()) }

// SetMaxDataSize 调整 Value 上限，超出 [ArgumentMaxDataSize, MaxAgDataSize] 会被夹住。
//
// 收发两端要一起抬：一端没抬，大包会在它那侧按超长拒绝，小包不受影响。
// 建议只在启动时设一次——运行期调高/调低会让在途调用的判定前后不一致。
func SetMaxDataSize(n int) {
	switch {
	case n < ArgumentMaxDataSize:
		n = ArgumentMaxDataSize
	case n > MaxAgDataSize:
		n = MaxAgDataSize
	}
	maxDataSize.Store(int64(n))
}

// dataTooLarge 超长错误。
//
// 默认上限下直接给哨兵 ErrAgDataTooLarge，连错误文本都与原来一致；抬过上限后
// 附上实际值与当前上限，同时 wrap 哨兵——errors.Is 与按文本匹配的老调用方都认得。
func dataTooLarge(got, limit int) error {
	if limit == ArgumentMaxDataSize {
		return ErrAgDataTooLarge
	}
	return fmt.Errorf("ag: data length exceeds %d (got %d): %w", limit, got, ErrAgDataTooLarge)
}

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

// frameLayout 解析帧布局，返回 Value 长度与起始偏移（短帧 5，扩展帧 9）。
//
// 长度是对端给的，这里先按当前上限校验再交给调用方分配：不然声明个 4GB 就照着
// 分配，一个包打爆内存。
func frameLayout(b []byte) (length, offset int, err error) {
	if len(b) < ArgumentHeaderSize {
		return 0, 0, ErrAgTooShort
	}
	l := int(binary.BigEndian.Uint16(b[3:5]))
	if l != ArgumentExtLenFlag {
		// 短帧：l ≤ 65534，恒小于当前上限（下限就是 65535），无需再校验
		return l, ArgumentHeaderSize, nil
	}
	if len(b) < ArgumentExtHeaderSize {
		return 0, 0, ErrAgTooShort
	}
	// uint64 域比较：32 位平台上 int(uint32) 可能溢出成负数绕过校验
	n := uint64(binary.BigEndian.Uint32(b[5:9]))
	limit := uint64(MaxDataSize())
	if n > limit {
		return 0, 0, dataTooLarge(int(min(n, uint64(math.MaxInt))), int(limit))
	}
	return int(n), ArgumentExtHeaderSize, nil
}

// IsArgument O(1) 校验帧完整性（magic + length 匹配）
func IsArgument(b []byte) bool {
	if len(b) < ArgumentHeaderSize || b[0] != ArgumentMagic1 || b[1] != ArgumentMagic2 {
		return false
	}
	length, offset, err := frameLayout(b)
	if err != nil {
		return false
	}
	return len(b) == offset+length
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
	length, offset, err := frameLayout(b)
	if err != nil || length == 0 {
		return nil
	}
	// 帧被截断时不能越界读：IsArgument/Validate 之外还有直接调 get_data 的路径
	if offset+length > len(b) {
		return nil
	}
	out := make([]byte, length)
	copy(out, b[offset:offset+length])
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
	length, offset, err := frameLayout(b)
	if err != nil {
		return err
	}
	if len(b) != offset+length {
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
		// 有 magic 却解析不了，通常是对端抬了上限而本端没抬（或帧被截断）。
		// 这里明确报错，而不是把带头字节的原样透传上去——那只会变成静默错数据。
		if len(b) >= ArgumentHeaderSize && b[0] == ArgumentMagic1 && b[1] == ArgumentMagic2 {
			if _, _, err := frameLayout(b); err != nil {
				return nil, err
			}
		}
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

// Encode 写一帧；Value 超过当前上限返回 ErrAgDataTooLarge（抬过上限时是包装值）。
//
// Value ≤ 65534 走短帧，与老格式逐字节一致；再大走扩展帧。
func encode_ag(t uint8, data []byte) ([]byte, error) {
	limit := MaxDataSize()
	if len(data) > limit {
		return nil, dataTooLarge(len(data), limit)
	}
	if len(data) <= ArgumentMaxShortData {
		out := make([]byte, ArgumentHeaderSize+len(data))
		out[0] = ArgumentMagic1
		out[1] = ArgumentMagic2
		out[2] = t
		binary.BigEndian.PutUint16(out[3:5], uint16(len(data)))
		copy(out[ArgumentHeaderSize:], data)
		return out, nil
	}
	out := make([]byte, ArgumentExtHeaderSize+len(data))
	out[0] = ArgumentMagic1
	out[1] = ArgumentMagic2
	out[2] = t
	binary.BigEndian.PutUint16(out[3:5], ArgumentExtLenFlag)
	binary.BigEndian.PutUint32(out[5:9], uint32(len(data)))
	copy(out[ArgumentExtHeaderSize:], data)
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
