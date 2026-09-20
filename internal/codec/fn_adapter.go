package codec

import (
	"github.com/w6xian/sloth/v3/decoder/fn"
)

type fnCodec struct{}

// Detect 只按 magic 认领帧：格式错误留给 Decode 报错，
// 且避免每帧被完整解析两次（判定一次 + 解码一次）。
func (fnCodec) Detect(raw []byte) bool {
	return fn.HasMagic(raw)
}

func (fnCodec) Decode(raw []byte) (uint8, uint64, []byte, error) {
	return fn.Decode(raw)
}

func (fnCodec) Encode(action uint8, id uint64, data []byte) ([]byte, error) {
	return fn.Encode(action, id, data)
}

// DefaultFnCodec returns a codec implementing the FN protocol.
func DefaultFnCodec() Codec { return fnCodec{} }
