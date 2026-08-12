package decoder

import (
	"github.com/w6xian/sloth/v3/internal/utils/id"
)

func NextId(n ...int64) uint64 {

	if len(n) == 0 {
		n = append(n, 1)
	}
	return uint64(id.NextId(n[0]))
}
func DecodeArgs(args [][]byte, decoder func([]byte) ([]byte, error)) [][]byte {
	a := [][]byte{}
	for _, v := range args {
		b, err := decoder(v)
		if err != nil {
			a = append(a, v)
			continue
		}
		a = append(a, b)
	}
	return a
}

func EncodeArgs(args []any, encoder func(any) ([]byte, error)) ([][]byte, error) {
	a := [][]byte{}
	for _, v := range args {
		b, err := encoder(v)
		if err != nil {
			return nil, err
		}
		a = append(a, b)
	}
	return a, nil
}
