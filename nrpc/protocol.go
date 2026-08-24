package nrpc

import (
	"encoding/json"
	"time"
)

// DefaultEncoder is the default encoder.
func DefaultEncoder(v any) ([]byte, error) {
	return json.Marshal(v)
}

// DefaultDecoder is the default decoder.
func DefaultDecoder(data []byte) ([]byte, error) {
	return data, nil
}

type TimeOut struct {
	Read  time.Duration
	Write time.Duration
}

type DataChannel struct {
	Read  chan []byte
	Write chan []byte
}
