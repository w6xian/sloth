package sloth

import (
	"fmt"
	"iter"
	"sync"
)

type SMap struct {
	m   map[string]int64
	mu  sync.RWMutex
	idx int64
}

func NewSMap() *SMap {
	return &SMap{
		m:   map[string]int64{},
		mu:  sync.RWMutex{},
		idx: 0,
	}
}

// 提供给 for range 遍历 map所有 key
//
//	for k,v := range smap.Range(){
//		fmt.Println(k,v)
//	}
func (s *SMap) Range() iter.Seq2[string, int64] {
	return func(yield func(string, int64) bool) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		for k, v := range s.m {
			if !yield(k, v) {
				return
			}
		}
	}
}

func (s *SMap) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

func (s *SMap) Get(key string) (int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[key]
	return v, ok
}

func (s *SMap) Del(key string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.m[key]
	if ok {
		delete(s.m, key)
	}
	return nil
}

func (s *SMap) Reg(key string, check bool) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx, ok := s.m[key]; ok {
		if check {
			return 0, fmt.Errorf("key %s already registered", key)
		}
		return idx, nil
	}
	s.idx--
	s.m[key] = s.idx
	return s.idx, nil
}
