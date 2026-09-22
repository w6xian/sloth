// Package codec 定义 sloth 的帧编解码抽象：在"一段字节"与
// "action + id + payload"之间相互转换。
//
// 默认实现是 FN 协议（decoder/fn）。要换协议就实现 Codec 接口，
// 再用 option.WithCodec 注入——这也是本包必须公开的原因：
// 放在 internal 下的话，使用方连接口类型都拿不到，根本无法实现自定义编解码。
//
// 实现要点：
//   - Detect 只按 magic 认领帧，格式错误留给 Decode 报错，
//     避免每帧被完整解析两次；
//   - 实现要轻量：Encode/Decode 在每帧的必经路径上，不能有额外分配。
package codec

import (
	"errors"
)

// Codec is an interface for pluggable frame codecs (eg. FN).
// It must be lightweight and suitable for hot paths.
type Codec interface {
	// Detect returns true if the raw bytes belong to this codec's frame type.
	Detect(raw []byte) bool
	// Decode parses the raw frame and returns action, id, payload (data) and error.
	Decode(raw []byte) (action uint8, id uint64, data []byte, err error)
	// Encode encodes a frame for this codec (optional helper).
	Encode(action uint8, id uint64, data []byte) ([]byte, error)
}

// DefaultFnCodecName is the default codec id for legacy FN protocol.
const DefaultFnCodecName = "fn"
const CODEC_PRE = 0x40
const CODEC_FN = 0x46
const CODEC_CODER_FN = "@F"

// registry 已注册的 codec。
//
// "这段字节是什么帧"只由各 codec 的 Detect 回答，识别规则只有一份：
// 以前路由环节（FrameRouter）和解码环节（HandleFn）各判一次、规则还不一样
// （一个按 magic、一个按 IsFn 的完整校验），同一个畸形帧会在两个环节被判成
// 两种不同的类型。现在两处都调 Select。
var registry = []Codec{fnCodec{}}

// Select 选出能处理该帧的 codec，是**唯一的帧识别入口**。
// 返回 false 表示该帧不属于任何已注册协议（例如裸业务数据、TLV 帧）。
func Select(raw []byte) (Codec, bool) {
	for _, c := range registry {
		if c.Detect(raw) {
			return c, true
		}
	}
	return nil, false
}

// GetCodecer 兼容旧调用：识别帧并返回 codec。
// 新代码请用 Select —— "不匹配"是正常结果，用 bool 比 error 更贴切。
func GetCodecer(raw []byte) (Codec, error) {
	if c, ok := Select(raw); ok {
		return c, nil
	}
	// 网络字节流不可信：短帧/未知 magic 都属于"不认识"，不该 panic
	return nil, errors.New("not support")
}

func UseCodec(corder string) Codec {
	switch corder {
	case CODEC_CODER_FN:
		return &fnCodec{}
	}
	return &fnCodec{}
}
