package message

import (
	"encoding/base64"
	"strconv"

	"github.com/w6xian/sloth/v3/internal/utils"
)

// 本文件提供 JsonCallObject / Msg 的手写 JSON 编码器。
//
// 背景：它们是每条 RPC 调用、每次消息下发的必经结构，
// encoding/json 的 Marshal 走反射 + 额外缓冲（约 1µs 级、多次分配），
// 手写编码可把开销降到一次预估容量分配，且输出字节与 json.Marshal 完全等价
// （字段名一致、[]byte 均为标准 base64 带 padding），解码端无需任何改动。

// AppendJSONStr 将 s 以 JSON 字符串（含两端引号）追加到 dst。
func AppendJSONStr(dst []byte, s string) []byte {
	return utils.AppendJSONStr(dst, s)
}

// JSONStrLen 返回 s 编码为 JSON 字符串（含引号）后的字节数。
func JSONStrLen(s string) int {
	return utils.JSONStrLen(s)
}

// appendJSONBytes 将 []byte 以 base64 JSON 字符串追加到 dst，语义与 json.Marshal 对 []byte 一致。
func appendJSONBytes(dst []byte, b []byte) []byte {
	return utils.AppendJSONBytes(dst, b)
}

// appendHeaderJSON 编码 Header 为 JSON 对象（字段顺序不影响解码）。
func appendHeaderJSON(dst []byte, h Header) []byte {
	dst = append(dst, '{')
	first := true
	for k, v := range h {
		if !first {
			dst = append(dst, ',')
		}
		first = false
		dst = appendJSONStr(dst, k)
		dst = append(dst, ':')
		dst = appendJSONStr(dst, v)
	}
	dst = append(dst, '}')
	return dst
}

// AppendJSON 手写编码 JsonCallObject（字段顺序与 struct 声明一致、omitempty 语义与 tag 一致）。
func (m *JsonCallObject) AppendJSON(dst []byte) []byte {
	dst = append(dst, '{')
	first := true
	if len(m.Header) > 0 {
		dst = append(dst, '"', 'h', 'e', 'a', 'd', 'e', 'r', '"', ':')
		dst = appendHeaderJSON(dst, m.Header)
		first = false
	}
	if !first {
		dst = append(dst, ',')
	}
	dst = append(dst, '"', 'm', 'e', 't', 'h', 'o', 'd', '"', ':')
	dst = appendJSONStr(dst, m.Method)
	if len(m.Args) > 0 {
		dst = append(dst, ',', '"', 'a', 'r', 'g', 's', '"', ':', '[')
		for i, a := range m.Args {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendJSONBytes(dst, a)
		}
		dst = append(dst, ']')
	}
	if len(m.Data) > 0 {
		dst = append(dst, ',', '"', 'd', 'a', 't', 'a', '"', ':')
		dst = appendJSONBytes(dst, m.Data)
	}
	if m.Error != "" {
		dst = append(dst, ',', '"', 'e', 'r', 'r', 'o', 'r', '"', ':')
		dst = appendJSONStr(dst, m.Error)
	}
	dst = append(dst, '}')
	return dst
}

// jsonSize 预估编码后长度，用于一次性分配、避免 append 扩容拷贝。
func (m *JsonCallObject) jsonSize() int {
	n := 2 + 9 + jsonStrLen(m.Method) // {} + "method":
	if c := len(m.Header); c > 0 {
		n += 11 + 2 + c - 1 // ,"header": + {}
		for k, v := range m.Header {
			n += jsonStrLen(k) + 1 + jsonStrLen(v)
		}
	}
	if c := len(m.Args); c > 0 {
		n += 10 + c - 1 // ,"args":[]
		for _, a := range m.Args {
			n += base64.StdEncoding.EncodedLen(len(a)) + 2
		}
	}
	if len(m.Data) > 0 {
		n += 10 + base64.StdEncoding.EncodedLen(len(m.Data)) + 2 // ,"data":
	}
	if m.Error != "" {
		n += 10 + jsonStrLen(m.Error) // ,"error":
	}
	return n
}

// MarshalJSONFast 一次性编码为 JSON 字节（预估容量，通常只分配一次）。
func (m *JsonCallObject) MarshalJSONFast() []byte {
	buf := make([]byte, 0, m.jsonSize())
	return m.AppendJSON(buf)
}

// AppendJSON 手写编码 Msg（Type 无 omitempty，Body 为 nil 时输出 null，与 json.Marshal 一致）。
func (m *Msg) AppendJSON(dst []byte) []byte {
	dst = append(dst, '{', '"', 't', 'y', 'p', 'e', '"', ':')
	dst = strconv.AppendInt(dst, int64(m.Type), 10)
	dst = append(dst, ',', '"', 'b', 'o', 'd', 'y', '"', ':')
	dst = appendJSONBytes(dst, m.Body)
	dst = append(dst, '}')
	return dst
}

// MarshalJSONFast 一次性编码为 JSON 字节（容量按实际长度精确计算）。
func (m *Msg) MarshalJSONFast() []byte {
	// {"type": + N + ,"body": + "base64"|null + }
	size := 8 + intLen(int64(m.Type)) + 8 + 1
	if m.Body == nil {
		size += 4 // null
	} else {
		size += base64.StdEncoding.EncodedLen(len(m.Body)) + 2
	}
	buf := make([]byte, 0, size)
	return m.AppendJSON(buf)
}

// intLen 返回 v 十进制表示的字节数（不含符号位之外的开销）。
func intLen(v int64) int {
	if v < 0 {
		// 避免 math.MinInt64 取负溢出
		return 1 + uintLen(uint64(-(v+1))+1)
	}
	return uintLen(uint64(v))
}

func uintLen(u uint64) int {
	n := 1
	for u >= 10 {
		n++
		u /= 10
	}
	return n
}
