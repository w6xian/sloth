package decoder

import (
	"fmt"

	"github.com/w6xian/sloth/v4/utils/id"
)

func NextId(n ...int64) uint64 {

	if len(n) == 0 {
		n = append(n, 1)
	}
	return uint64(id.NextId(n[0]))
}
// DecodeArgs 逐个解码入参，任一个解不开就整体返回错误。
//
// 原实现失败时把原始字节**原样塞回**（a = append(a, v) continue）：对端发扩展帧
// 而本侧限制更小时，带帧头的字节会被当成业务数据交给反射调用——静默错数据，
// 比直接报错难查得多。
func DecodeArgs(args [][]byte, decoder func([]byte) ([]byte, error)) ([][]byte, error) {
	a := make([][]byte, 0, len(args))
	for _, v := range args {
		b, err := decoder(v)
		if err != nil {
			return nil, fmt.Errorf("ag: decode arg: %w", err)
		}
		a = append(a, b)
	}
	return a, nil
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
