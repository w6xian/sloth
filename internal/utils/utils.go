package utils

import (
	"encoding/json"
)

func Serialize(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	return b
}

func Deserialize(b []byte, v any) error {
	err := json.Unmarshal(b, v)
	if err != nil {
		return err
	}
	return nil
}

func Max[T int | int64 | float64 | float32 | byte](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func Min[T int | int64 | float64 | float32 | byte](a, b T) T {
	if a < b {
		return a
	}
	return b
}
