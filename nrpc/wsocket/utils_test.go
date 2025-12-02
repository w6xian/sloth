package wsocket

import (
	"fmt"
	"testing"
)

func TestGetSliceArray(t *testing.T) {
	n := "test"
	message := []byte("这是一个测试字符串abc")
	sliceSize := 5
	slices, err := getSliceArray(n, message, sliceSize)
	if err != nil {
		t.Errorf("getSliceArray failed, err = %v", err)
	}
	for _, slice := range slices {
		fmt.Println(slice.N, slice.T, slice.I, slice.S, string(slice.D))
	}
	if len(slices) != 3 {
		t.Errorf("getSliceArray failed, len(slices) = %d, want 3", len(slices))
	}
}
