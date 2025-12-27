package lpool

import (
	"testing"
)

//go test -bench=. -benchmem -run=none

func BenchmarkBytePool(b *testing.B) {
	bp := NewBytePool(100, 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := bp.Get()
		bp.Put(buf)
	}
	b.StopTimer()
}
