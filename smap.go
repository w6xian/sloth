package sloth

import (
	"fmt"
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
