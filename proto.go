package sloth

import (
	"encoding/base64"
	"fmt"

	"github.com/w6xian/sloth/decoder/tlv"
)

func DecodeString(frame []byte) string {
	decoded, err := base64.StdEncoding.DecodeString(string(frame))
	if err != nil {
		fmt.Println("Error decoding:", err)
		return ""
	}
	return string(decoded)
}

func Decode64ToBytes(frame []byte) []byte {
	decoded, err := base64.StdEncoding.DecodeString(string(frame))
	if err != nil {
		fmt.Println("Error decoding:", err)
		return []byte{}
	}
	return decoded
}

func Decode64ToTlv(frame []byte) (*tlv.TlV, error) {
	decoded, err := base64.StdEncoding.DecodeString(string(frame))
	if err != nil {
		fmt.Println("Error decoding:", err)
		return nil, err
	}
	tlv, err := tlv.NewTLVFromFrame(decoded)
	if err != nil {
		return nil, err
	}
	return tlv, nil
}
