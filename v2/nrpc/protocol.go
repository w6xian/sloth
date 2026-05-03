package nrpc

import "encoding/json"

// DefaultEncoder is the default encoder.
func DefaultEncoder(v any) ([]byte, error) {
	return json.Marshal(v)
}

// DefaultDecoder is the default decoder.
func DefaultDecoder(data []byte) ([]byte, error) {
	return data, nil
}
