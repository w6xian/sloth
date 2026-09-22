package utils

import "encoding/base64"

// 本文件提供与 encoding/json **字节级等价**的手写编码器。
//
// 背景：JsonCallObject / Msg / DataSlice 位于每条 RPC 调用与每次消息下发的必经路径，
// json.Marshal 走反射 + 额外缓冲（约 1µs 级、多次分配）。
// 而解码端仍使用 json.Unmarshal，因此编码器必须与标准库输出完全一致
// （字段顺序、转义规则、[]byte 的 base64），否则会出现编译期无法发现的串包问题。
// 这里集中实现一份，供 message / decoder/frame 共用，避免多处实现漂移。

const hexDigits = "0123456789abcdef"

// JSONStrLen 返回 s 编码为 JSON 字符串（含两端引号）后的字节数。
// 转义规则与 encoding/json 一致：
//   - " \ \b \f \n \r \t 使用 2 字节短转义；
//   - 其余控制字符与 < > &（标准库默认开启 HTML 转义）编码为 \u00XX（6 字节）。
func JSONStrLen(s string) int {
	n := len(s) + 2 // 两端引号
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\\' || c == '\b' || c == '\f' || c == '\n' || c == '\r' || c == '\t':
			n++
		case c < 0x20 || c == '<' || c == '>' || c == '&':
			n += 5 // \u00XX 共 6 字节，原 1 字节
		}
	}
	return n
}

// AppendJSONStr 将 s 以 JSON 字符串形式（含两端引号）追加到 dst。
func AppendJSONStr(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			dst = append(dst, '\\', '"')
		case c == '\\':
			dst = append(dst, '\\', '\\')
		case c == '\b':
			dst = append(dst, '\\', 'b')
		case c == '\f':
			dst = append(dst, '\\', 'f')
		case c == '\n':
			dst = append(dst, '\\', 'n')
		case c == '\r':
			dst = append(dst, '\\', 'r')
		case c == '\t':
			dst = append(dst, '\\', 't')
		case c < 0x20 || c == '<' || c == '>' || c == '&':
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0x0f])
		default:
			dst = append(dst, c)
		}
	}
	dst = append(dst, '"')
	return dst
}

// AppendJSONBytes 将 []byte 按 encoding/json 对 []byte 的语义编码后追加到 dst：
// nil -> null，空 slice -> ""，其余为标准 base64（带 padding）。
//
// 容量不足时一次性分配到"恰好"大小并原地编码：
// 早期版本先 append('"') 再 append(base64)，对大 payload 会多一次全量拷贝
// （64KB payload 实测 180KB/3 allocs，反而慢于标准库）。
func AppendJSONBytes(dst []byte, b []byte) []byte {
	if b == nil {
		return append(dst, 'n', 'u', 'l', 'l')
	}
	enc := base64.StdEncoding.EncodedLen(len(b))
	need := enc + 2 // 两端引号
	off := len(dst)
	if cap(dst)-off >= need {
		dst = dst[:off+need]
	} else {
		buf := make([]byte, off+need)
		copy(buf, dst)
		dst = buf
	}
	dst[off] = '"'
	base64.StdEncoding.Encode(dst[off+1:off+1+enc], b)
	dst[off+need-1] = '"'
	return dst
}
