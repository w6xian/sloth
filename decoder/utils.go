package decoder

import (
	"encoding/json"

	"github.com/w6xian/sloth/internal/utils/id"
)

func NextId(n ...int64) uint64 {
	if len(n) == 0 {
		n = append(n, 1)
	}
	return uint64(id.NextId(n[0]))
}

func Serialize(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	return b
}
