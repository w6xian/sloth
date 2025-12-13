package wsocket

import (
	"fmt"
	"testing"

	"github.com/w6xian/sloth/decoder/frame"
)

func TestGetSliceArray(t *testing.T) {
	n := "test"
	message := []byte("这是一个测试字符串abc")
	sliceSize := 10
	slices, err := frame.Split(n, message, sliceSize, frame.TextMessage)
	if err != nil {
		t.Errorf("getSliceArray failed, err = %v", err)
	}
	fmt.Println(len(slices))

	for _, slice := range slices {
		fmt.Println(slice.N, slice.T, slice.I, slice.S, string(slice.D))
		buf := slice.Encode()
		fmt.Println(buf)
		s, err := frame.Decode(buf)
		if err != nil {
			t.Errorf("Decode failed, err = %v", err)
		}
		fmt.Println(s.N, s.T, s.I, s.S, string(s.D))
	}
	if len(slices) != 1 {
		t.Errorf("getSliceArray failed, len(slices) = %d, want 3", len(slices))
	}
}
