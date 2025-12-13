package decoder

import (
	"github.com/w6xian/sloth/internal/utils/id"
)

func NextId(n ...int64) uint64 {

	if len(n) == 0 {
		n = append(n, 1)
	}
	return uint64(id.NextId(n[0]))
}
