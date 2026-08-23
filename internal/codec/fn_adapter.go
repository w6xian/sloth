package codec

import (
	"github.com/w6xian/sloth/v3/decoder/fn"
)

type fnCodec struct{}

func (fnCodec) Detect(raw []byte) bool {
	return fn.IsFn(raw)
}

func (fnCodec) Decode(raw []byte) (uint8, uint64, []byte, error) {
	return fn.Decode(raw)
}

func (fnCodec) Encode(action uint8, id uint64, data []byte) ([]byte, error) {
	return fn.Encode(action, id, data)
}

// DefaultFnCodec returns a codec implementing the FN protocol.
func DefaultFnCodec() Codec { return fnCodec{} }
