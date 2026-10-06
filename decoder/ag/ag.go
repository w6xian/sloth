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
 *     带头的原始字节透传上去，属于静默错数据——所以大包链路两端都得是新版。
 *
 * 协议上限 vs 部署限制（两者别混）：
 *   - 协议上限 MaxAgDataSize = 1GB，与 fn 帧（FnMaxDataSize）对齐，是"能表达多大"；
 *   - 部署限制用 sloth.WithMaxParamSize 按连接设，是"这一侧最多收多大"。
 *     协议不该替业务做这个决定：协议写小了，想传大包的合法场景就永远没门。
 *   - 进程级默认值（SetMaxDataSize）只在没按连接设时才生效，是一刀切的兜底。
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
	// LegacyMaxDataSize 扩展帧出现之前的单帧上限。限制恰好等于它时，超长错误
	// 直接返回哨兵——连文本都与老版本一致，按文本 grep 的规则不会失效。
	//
	// 取 65535 而非 65536：原先写成 1<<16，encode_ag 会接受 65536 字节的 Value，
	// 再被 PutUint16 截断成 0 → 编出长度 0 的帧，数据静默丢失。
	LegacyMaxDataSize = 1<<16 - 1
	// MaxAgDataSize 协议能表达的 Value 上限，同时是未设限制时的默认值。
	//
	// 与 fn 帧（FnMaxDataSize = 1GB）对齐：AG 帧是装在 fn 帧 payload 里的参数
	// 编码，不该比外层先撞墙。要收窄是部署策略的事——用 sloth.WithMaxParamSize
	// 按连接设，而不是把协议写小：协议写小了，想传大包的合法场景就永远没门。
	MaxAgDataSize = 1 << 30
)

// maxDataSize 进程级默认上限：只在没有按连接设限制（WithMaxParamSize）时生效。
//
// 用原子量：允许启动后再调整，与收发两侧的读不打架。
var maxDataSize atomic.Int64

func init() {
	maxDataSize.Store(MaxAgDataSize)
}

// MaxDataSize 返回进程级默认上限。
func MaxDataSize() int { return int(maxDataSize.Load()) }

// SetMaxDataSize 调整进程级默认上限，n ≤ 0 恢复默认，超过协议上限会被夹住。
//
// 这是一刀切的进程级开关。要按连接分别设（多租户、公网/内网分档）用
// sloth.WithMaxParamSize——限制是部署策略，不该由协议层替业务决定。
// 进程级值同样建议只在启动时设一次：运行期改会让在途调用的判定前后不一致。
func SetMaxDataSize(n int) {
	if n <= 0 || n > MaxAgDataSize {
		n = MaxAgDataSize
	}
	maxDataSize.Store(int64(n))
}

// ClampLimit 把限制夹进 [1, MaxAgDataSize]。
//
// 下限取 1 而不是 65535：限制的作用是"这一侧最多收多大"，想收得比 65535 更紧
// （公网入口只收几 KB 的配置）是合理诉求，协议不该拦。
func ClampLimit(n int) int {
	if n < 1 {
		return 1
	}
	if n > MaxAgDataSize {
		return MaxAgDataSize
	}
	return n
}

// NewEncoder 生成按 limit 编码的 Encoder，供 sloth.WithMaxParamSize 注入连接。
func NewEncoder(limit int) func(any) ([]byte, error) {
	l := ClampLimit(limit)
	return func(arg any) ([]byte, error) { return encodeWith(l, arg) }
}

// NewDecoder 生成按 limit 解码的 Decoder：声明长度超过 limit 的帧直接拒绝，
// 不照对端给的数字分配内存。
func NewDecoder(limit int) func([]byte) ([]byte, error) {
	l := ClampLimit(limit)
	return func(b []byte) ([]byte, error) { return decoderWith(l, b) }
}

// dataTooLarge 超长错误。
//
// 限制恰好等于老上限时直接给哨兵 ErrAgDataTooLarge，连错误文本都与原来一致；
// 其余情况附上实际值与当前限制，同时 wrap 哨兵——errors.Is 与按文本匹配的
// 老调用方都认得。
func dataTooLarge(got, limit int) error {
	if limit == LegacyMaxDataSize {
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
	ErrAgDataTooLarge   = fmt.Errorf("ag: data length exceeds %d", LegacyMaxDataSize)
	ErrAgUnknownType    = errors.New("ag: unknown type tag")
	ErrAgInvalidHeader  = errors.New("ag: invalid header")
)

// frameLayout 按进程级默认限制解析帧布局。
func frameLayout(b []byte) (length, offset int, err error) {
	return frameLayoutLimit(b, MaxDataSize())
}

// frameLayoutLimit 按 limit 解析帧布局，返回 Value 长度与起始偏移（短帧 5，扩展帧 9）。
//
// 长度是对端给的，这里先按 limit 校验再交给调用方分配：不然声明个 4GB 就照着
// 分配，一个包打爆内存。短帧也要比——限制设成 4KB 时，一条 60KB 的短帧同样得拒。
func frameLayoutLimit(b []byte, limit int) (length, offset int, err error) {
	if len(b) < ArgumentHeaderSize {
		return 0, 0, ErrAgTooShort
	}
	l := int(binary.BigEndian.Uint16(b[3:5]))
	if l != ArgumentExtLenFlag {
		if l > limit {
			return 0, 0, dataTooLarge(l, limit)
		}
		return l, ArgumentHeaderSize, nil
	}
	if len(b) < ArgumentExtHeaderSize {
		return 0, 0, ErrAgTooShort
	}
	// uint64 域比较：32 位平台上 int(uint32) 可能溢出成负数绕过校验
	n := uint64(binary.BigEndian.Uint32(b[5:9]))
	if n > uint64(limit) {
		return 0, 0, dataTooLarge(int(min(n, uint64(math.MaxInt))), limit)
	}
	return int(n), ArgumentExtHeaderSize, nil
}

// IsArgument O(1) 校验帧完整性（magic + length 匹配），按进程级默认限制。
func IsArgument(b []byte) bool {
	return isArgumentLimit(b, MaxDataSize())
}

func isArgumentLimit(b []byte, limit int) bool {
	if len(b) < ArgumentHeaderSize || b[0] != ArgumentMagic1 || b[1] != ArgumentMagic2 {
		return false
	}
	length, offset, err := frameLayoutLimit(b, limit)
	if err != nil {
		return false
	}
	return len(b) == offset+length
}

// Data 取 Value 段；非 AG 帧或不合法返回源切片（兼容旧调用方直接透传）
func Data(b []byte) []byte {
	return dataLimit(b, MaxDataSize())
}

func dataLimit(b []byte, limit int) []byte {
	if !isArgumentLimit(b, limit) {
		return b
	}
	return get_dataLimit(b, limit)
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
	return get_dataLimit(b, MaxDataSize())
}

func get_dataLimit(b []byte, limit int) []byte {
	length, offset, err := frameLayoutLimit(b, limit)
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

// Validate 纯验证；全部通过返回 nil（按进程级默认限制）
func Validate(b []byte) error {
	return validateLimit(b, MaxDataSize())
}

func validateLimit(b []byte, limit int) error {
	if len(b) < ArgumentHeaderSize {
		return ErrAgTooShort
	}
	if b[0] != ArgumentMagic1 || b[1] != ArgumentMagic2 {
		return ErrAgBadMagic
	}
	length, offset, err := frameLayoutLimit(b, limit)
	if err != nil {
		return err
	}
	if len(b) != offset+length {
		return ErrAgLengthMismatch
	}
	return nil
}

func get_frame(b []byte) (byte, []byte, error) {
	return getFrameLimit(b, MaxDataSize())
}

func getFrameLimit(b []byte, limit int) (byte, []byte, error) {
	if err := validateLimit(b, limit); err != nil {
		return 0, nil, err
	}
	t := b[2]
	v := get_dataLimit(b, limit)
	return t, v, nil
}

// argPayload 把任意值拆成"帧类型 + Value 字节"，组帧的动作留给调用方。
//
// 拆开是为了让带限制的编码器（NewEncoder）与默认编码器共用同一套类型映射：
// 限制只影响"多大算超长"，不影响"这个值该编成什么帧"。
func argPayload(arg any) (uint8, []byte, error) {
	if arg == nil {
		return ArgumentTypeNil, nil, nil
	}
	t := typeof(arg)
	switch t {
	case ArgumentTypeBool:
		if arg.(bool) {
			return t, []byte{1}, nil
		}
		return t, []byte{0}, nil

	case ArgumentTypeInt:
		return t, int_to_byte(int64(arg.(int))), nil
	case ArgumentTypeInt8:
		return t, int_to_byte(int64(arg.(int8))), nil
	case ArgumentTypeInt16:
		return t, int_to_byte(int64(arg.(int16))), nil
	case ArgumentTypeInt32:
		return t, int_to_byte(int64(arg.(int32))), nil
	case ArgumentTypeInt64:
		return t, int_to_byte(arg.(int64)), nil

	case ArgumentTypeUint:
		return t, uint_to_byte(uint64(arg.(uint))), nil
	case ArgumentTypeUint8:
		return t, uint_to_byte(uint64(arg.(uint8))), nil
	case ArgumentTypeUint16:
		return t, uint_to_byte(uint64(arg.(uint16))), nil
	case ArgumentTypeUint32:
		return t, uint_to_byte(uint64(arg.(uint32))), nil
	case ArgumentTypeUint64:
		return t, uint_to_byte(arg.(uint64)), nil
	case ArgumentTypeUintptr:
		return t, uint_to_byte(uint64(arg.(uintptr))), nil

	case ArgumentTypeFloat32:
		return t, binary.LittleEndian.AppendUint32(nil, math.Float32bits(arg.(float32))), nil
	case ArgumentTypeFloat64:
		return t, binary.LittleEndian.AppendUint64(nil, math.Float64bits(arg.(float64))), nil

	case ArgumentTypeComplex64:
		c := arg.(complex64)
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint32(buf[0:4], math.Float32bits(real(c)))
		binary.LittleEndian.PutUint32(buf[4:8], math.Float32bits(imag(c)))
		return t, buf, nil
	case ArgumentTypeComplex128:
		c := arg.(complex128)
		buf := make([]byte, 16)
		binary.LittleEndian.PutUint64(buf[0:8], math.Float64bits(real(c)))
		binary.LittleEndian.PutUint64(buf[8:16], math.Float64bits(imag(c)))
		return t, buf, nil

	case ArgumentTypeString:
		return t, []byte(arg.(string)), nil
	case ArgumentTypeBytes:
		b := arg.([]byte)
		out := make([]byte, len(b))
		copy(out, b)
		return t, out, nil

	case ArgumentTypeSlice, ArgumentTypeMap, ArgumentTypeStruct:
		s, err := jsonMarshalFallback(arg)
		if err != nil {
			return 0, nil, err
		}
		return ArgumentTypeString, []byte(s), nil
	}
	// 兜底：json 字符串化
	return ArgumentTypeCustom, utils.Serialize(arg), nil
}

// Encode 按进程级默认限制编码一帧 AG。
func Encode(arg any) ([]byte, error) {
	return encodeWith(MaxDataSize(), arg)
}

func encodeWith(limit int, arg any) ([]byte, error) {
	t, data, err := argPayload(arg)
	if err != nil {
		return nil, err
	}
	return encode_ag_with(limit, t, data)
}

func Decode(b []byte) (any, error) {
	return decodeWith(MaxDataSize(), b)
}

func decodeWith(limit int, b []byte) (any, error) {
	if !isArgumentLimit(b, limit) {
		return nil, ErrAgInvalidHeader
	}
	return get_valueLimit(b, limit)
}

// Decoder 按进程级默认限制解码；按连接设的限制走 NewDecoder。
func Decoder(b []byte) ([]byte, error) {
	return decoderWith(MaxDataSize(), b)
}

func decoderWith(limit int, b []byte) ([]byte, error) {
	if !isArgumentLimit(b, limit) {
		// 有 magic 却解析不了，通常是本侧限制小于对端发的帧（或帧被截断）。
		// 这里明确报错，而不是把带头字节的原样透传上去——那只会变成静默错数据。
		if len(b) >= ArgumentHeaderSize && b[0] == ArgumentMagic1 && b[1] == ArgumentMagic2 {
			if _, _, err := frameLayoutLimit(b, limit); err != nil {
				return nil, err
			}
		}
		return b, nil
	}
	return get_dataLimit(b, limit), nil
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

// encode_ag 写一帧（按进程级默认限制）。
func encode_ag(t uint8, data []byte) ([]byte, error) {
	return encode_ag_with(MaxDataSize(), t, data)
}

// encode_ag_with 写一帧；Value 超过 limit 返回 ErrAgDataTooLarge（非老上限时是包装值）。
//
// Value ≤ 65534 走短帧，与老格式逐字节一致；再大走扩展帧。
func encode_ag_with(limit int, t uint8, data []byte) ([]byte, error) {
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
	return get_valueLimit(b, MaxDataSize())
}

func get_valueLimit(b []byte, limit int) (any, error) {
	t, v, err := getFrameLimit(b, limit)
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
