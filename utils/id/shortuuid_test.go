package id

import (
	"sync"
	"testing"
)

// NextId 必须全局唯一：原实现每次调用新建 snowflake 节点（step 从 0 开始），
// 同一毫秒内的多次调用会返回**完全相同**的 ID，导致 RPC 响应串包。
func TestNextIdUnique(t *testing.T) {
	const n = 10000
	seen := make(map[int64]struct{}, n)
	for i := 0; i < n; i++ {
		id := NextId(1)
		if id == 0 {
			t.Fatalf("NextId returned 0 at %d", i)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicated id %d at %d", id, i)
		}
		seen[id] = struct{}{}
	}
}

// 并发唯一性（多 goroutine 争抢同一节点）
func TestNextIdUniqueConcurrent(t *testing.T) {
	const (
		workers = 8
		per     = 2000
	)
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen = make(map[int64]struct{}, workers*per)
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]int64, 0, per)
			for i := 0; i < per; i++ {
				local = append(local, NextId(int64(w%1024)))
			}
			mu.Lock()
			for _, id := range local {
				if _, dup := seen[id]; dup {
					mu.Unlock()
					t.Errorf("duplicated id %d", id)
					return
				}
				seen[id] = struct{}{}
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(seen) != workers*per {
		t.Fatalf("expected %d unique ids, got %d", workers*per, len(seen))
	}
}

// svr 越界必须有可用的兜底 ID（原实现直接返回 0，会造成调用方 ID 冲突）
func TestNextIdFallback(t *testing.T) {
	seen := make(map[int64]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := NextId(999999)
		if id == 0 {
			t.Fatal("fallback id must not be 0")
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicated fallback id %d", id)
		}
		seen[id] = struct{}{}
	}
}

func BenchmarkNextId(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = NextId(1)
		}
	})
}
