package sloth

import (
	"encoding/base64"

	"github.com/w6xian/sloth/v3/internal/logger"
	"github.com/w6xian/tlv"
)

func DecodeString(frame []byte) string {
	decoded, err := base64.StdEncoding.DecodeString(string(frame))
	if err != nil {
		logger.Errorw(nil, "base64 decode failed", "func", "DecodeString", "err", err)
		return ""
	}
	return string(decoded)
}

func Decode64ToBytes(frame []byte) []byte {
	decoded, err := base64.StdEncoding.DecodeString(string(frame))
	if err != nil {
		logger.Errorw(nil, "base64 decode failed", "func", "Decode64ToBytes", "err", err)
		return []byte{}
	}
	return decoded
}

func Decode64ToTlv(frame []byte) (*tlv.TlV, error) {
	decoded, err := base64.StdEncoding.DecodeString(string(frame))
	if err != nil {
		logger.Errorw(nil, "base64 decode failed", "func", "Decode64ToTlv", "err", err)
		return nil, err
	}
	tlv, err := tlv.NewFromFrame(decoded)
	if err != nil {
		return nil, err
	}
	return tlv, nil
}
