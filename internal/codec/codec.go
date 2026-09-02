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

func GetCodecer(raw []byte) (Codec, error) {
	// 网络字节流不可信：长度不足 2 字节时直接返回错误，避免 raw[0]/raw[1] 越界 panic
	if len(raw) < 2 {
		return nil, errors.New("frame too short")
	}
	prev := raw[0]
	proto := raw[1]
	// @F [64 70]
	if prev == CODEC_PRE && proto == CODEC_FN {
		return &fnCodec{}, nil
	}
	return nil, errors.New("not support")
}

func UseCodec(corder string) Codec {
	switch corder {
	case CODEC_CODER_FN:
		return &fnCodec{}
	}
	return &fnCodec{}
}
