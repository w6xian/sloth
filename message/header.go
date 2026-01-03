package message

import (
	"encoding/json"

	"github.com/w6xian/tlv"
)

type Header map[string]string

func (h Header) Get(key string) string {
	return h[key]
}

func (h Header) Set(key, value string) {
	h[key] = value
}

func (h Header) Bytes() ([]byte, error) {
	return tlv.JsonEnpack(h)
}

func NewHeaderFromBV(bv []byte) (Header, error) {
	var h = Header{}
	bv, err := tlv.JsonUnpack(bv)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(bv, &h)
	if err != nil {
		return nil, err
	}
	return h, nil
}
