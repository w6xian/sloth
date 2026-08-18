package message

import (
	"encoding/json"
	"fmt"
	"sync"

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

const hexDigits = "0123456789abcdef"

// jsonStrLen 返回 s 作为 JSON 字符串（含两端引号）编码后的字节数。
func jsonStrLen(s string) int {
	n := len(s) + 2 // 引号
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\\':
			n++
		case c < 0x20:
			n += 5 // \u00XX 共 6 字节，原 1 字节
		}
	}
	return n
}

// appendJSONStr 将 s 以 JSON 字符串形式（含引号）追加到 dst。
func appendJSONStr(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			dst = append(dst, '\\', '"')
		case c == '\\':
			dst = append(dst, '\\', '\\')
		case c < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0x0f])
		default:
			dst = append(dst, c)
		}
	}
	dst = append(dst, '"')
	return dst
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
