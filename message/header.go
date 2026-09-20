package message

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/w6xian/sloth/v3/internal/utils"
	"github.com/w6xian/tlv"
)

type Header map[string]string

func (h Header) Get(key string) string {
	if _, ok := h[key]; !ok {
		return ""
	}
	return h[key]
}

func (h Header) Set(key, value string) {
	if value == "" {
		// 删除空值
		h.Delete(key)
		return
	}
	h[key] = value
}

// 删除头信息
func (h Header) Delete(key string) {
	delete(h, key)
}

// Bytes 将 Header 编码为 TLV JSON 帧（协议格式与 tlv.JsonEnpack 完全兼容）。
//
// 手写编码器的原因：
//   - tlv.JsonEnpack 内部是 json.Marshal(反射+key排序) + TLV 打包(bytes.Buffer)，
//     每次调用多次分配（benchmark: ~900ns / 464B / 11 allocs）；
//   - 此处针对 map[string]string 手写 JSON 编码，并预计算总长度单次分配，
//     输出协议字节与 tlv.JsonEnpack 一致，解码端（tlv.JsonUnpack/json.Unmarshal）无需改动。
func (h Header) Bytes() ([]byte, error) {
	return encodeHeader(h), nil
}

// tlv 协议默认选项常量（与 tlv 包 newOption 默认一致）
const (
	tlvTypeProtocol = 0x00
	tlvTypeJSON     = 0x14
	tlvMinLength    = 1
	tlvMaxLength    = 2
)

// jsonStrLen / appendJSONStr 复用 internal/utils 的实现：
// 与 encoding/json 字节级等价（此前未处理 \n \r \t 的短转义与 < > & 的 HTML 转义，
// 与 tlv.JsonEnpack / json.Marshal 的输出存在细微差异）。
func jsonStrLen(s string) int {
	return utils.JSONStrLen(s)
}

func appendJSONStr(dst []byte, s string) []byte {
	return utils.AppendJSONStr(dst, s)
}

// encodeHeader 编码 Header 为 tlv JSON 帧，单次分配。
func encodeHeader(h Header) []byte {
	// 1) 计算 JSON 部分长度
	jsonLen := 2 // {}
	if n := len(h); n > 0 {
		jsonLen += n - 1 // 键值对之间的逗号
		for k, v := range h {
			jsonLen += jsonStrLen(k) + 1 + jsonStrLen(v) // "k":"v"
		}
	}

	// 2) TLV 层长度与 tag（长度超过 MinLength 上限(255)时扩展为 2 字节并置高位）
	jsonLenSize := tlvMinLength
	tlvTag := byte(tlvTypeJSON)
	if jsonLen > 0xff {
		jsonLenSize = tlvMaxLength
		tlvTag |= 0x80
	}
	tlvDataLen := 1 + jsonLenSize + jsonLen

	// 3) 协议层长度与标记字节
	plenSize := tlvMinLength
	flag := byte(1)
	if tlvDataLen > 0xff {
		plenSize = tlvMaxLength
		flag |= 0x80
	}

	// 4) 单次分配整帧
	total := 3 + plenSize + tlvDataLen
	buf := make([]byte, 0, total)
	buf = append(buf, tlvTypeProtocol)
	buf = append(buf, (tlvMaxLength<<4)|tlvMinLength) // 0x21
	buf = append(buf, flag)
	if plenSize == 2 {
		buf = append(buf, byte(tlvDataLen>>8), byte(tlvDataLen))
	} else {
		buf = append(buf, byte(tlvDataLen))
	}
	buf = append(buf, tlvTag)
	if jsonLenSize == 2 {
		buf = append(buf, byte(jsonLen>>8), byte(jsonLen))
	} else {
		buf = append(buf, byte(jsonLen))
	}

	// 5) JSON 主体
	buf = append(buf, '{')
	first := true
	for k, v := range h {
		if !first {
			buf = append(buf, ',')
		}
		first = false
		buf = appendJSONStr(buf, k)
		buf = append(buf, ':')
		buf = appendJSONStr(buf, v)
	}
	buf = append(buf, '}')
	return buf
}

func (h Header) Keys(k ...string) Header {
	keys := Header{}
	for _, key := range k {
		if _, ok := h[key]; ok {
			keys[key] = h[key]
		}
	}
	return keys
}

// Copy 复制头信息
func (h Header) Clone() Header {
	clone := make(Header, len(h))
	for k, v := range h {
		clone[k] = v
	}
	return clone
}

// tlvMu 串行化对 tlv 库的调用，原因见 NewHeaderFromBV 内的注释。
var tlvMu sync.Mutex

func NewHeaderFromBV(bv []byte) (h Header, err error) {
	// tlv.JsonUnpack 对畸形输入会 panic（slice 越界），
	// 网络字节流不可信，这里兜底转成 error，避免服务端进程崩溃。
	defer func() {
		if r := recover(); r != nil {
			h = nil
			err = fmt.Errorf("header decode panic: %v", r)
		}
	}()
	h = Header{}
	// tlv 库内部复用全局共享的 option 对象并修改其字段，并发调用既有数据竞争
	// 也会互相污染状态（见 vendor/github.com/w6xian/tlv/option.go:32-36），
	// 依赖库改不了，只能在调用侧串行化。
	// 必须用 defer 解锁：tlv.JsonUnpack 对畸形输入会 panic，
	// 直接写的 Unlock 会被 panic 跳过，锁永久持有（后续调用全部死锁）。
	tlvMu.Lock()
	defer tlvMu.Unlock()
	bv, err = tlv.JsonUnpack(bv)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(bv, &h)
	if err != nil {
		return nil, err
	}
	return h, nil
}

var headerPool = sync.Pool{
	New: func() any {
		return make(Header, 8)
	},
}

func GetHeader() Header {
	h := headerPool.Get().(Header)
	for k := range h {
		delete(h, k)
	}
	return h
}

func PutHeader(h Header) {
	if h == nil {
		return
	}
	for k := range h {
		delete(h, k)
	}
	headerPool.Put(h)
}
