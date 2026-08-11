package sloth

import (
	"fmt"
	"strings"
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

func (s *SMap) Range() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]int64, len(s.m))
	for k, v := range s.m {
		out[k] = v
	}
	return out
}

func (s *SMap) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

func (s *SMap) Get(key string) (int64, bool) {
	key = strings.TrimSpace(key)
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[key]
	return v, ok
}

func (s *SMap) Del(key string) error {
	key = strings.TrimSpace(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func (s *SMap) Reg(key string, check bool) (int64, error) {
	key = strings.TrimSpace(key)
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
