package message

import (
	"encoding/json"
	"maps"
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

func (h Header) Bytes() ([]byte, error) {
	return tlv.JsonEnpack(h)
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
	clone := make(Header)
	maps.Copy(clone, h)
	return clone
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
