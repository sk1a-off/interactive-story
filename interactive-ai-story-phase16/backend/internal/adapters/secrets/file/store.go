package file

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Store is a small local-only secret store for the single-user application.
// Secrets never enter PostgreSQL or public configuration responses. The file
// and its parent directory are restricted to the backend OS user.
type Store struct {
	mu   sync.RWMutex
	path string
	data map[string]string
}

func New(path string) (*Store, error) {
	if filepath.Clean(path) == "." || path == "" {
		return nil, errors.New("secret store path is required")
	}
	s := &Store{path: path, data: map[string]string{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = json.Unmarshal(raw, &s.data); err != nil {
		return err
	}
	if s.data == nil {
		s.data = map[string]string{}
	}
	return os.Chmod(s.path, 0o600)
}

func (s *Store) Set(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if value == "" {
		delete(s.data, key)
	} else {
		s.data[key] = value
	}
	return s.persist()
}

func (s *Store) Has(_ context.Context, key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[key] != ""
}

func (s *Store) Get(_ context.Context, key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.data[key]
	return value, ok && value != ""
}

func (s *Store) persist() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".provider-keys-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = os.Remove(tmpName)
		}
	}()
	if err = tmp.Chmod(0o600); err == nil {
		err = json.NewEncoder(tmp).Encode(s.data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmpName, s.path); err != nil {
		return err
	}
	keep = true
	return os.Chmod(s.path, 0o600)
}
