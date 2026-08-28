package memory

import (
	"context"
	"sync"
)

type Store struct {
	mu sync.RWMutex
	m  map[string]string
}

func New(initial map[string]string) *Store {
	cp := map[string]string{}
	for k, v := range initial {
		if v != "" {
			cp[k] = v
		}
	}
	return &Store{m: cp}
}
func (s *Store) Set(_ context.Context, k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v == "" {
		delete(s.m, k)
	} else {
		s.m[k] = v
	}
	return nil
}
func (s *Store) Has(_ context.Context, k string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.m[k] != ""
}
func (s *Store) Get(_ context.Context, k string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[k]
	return v, ok
}
