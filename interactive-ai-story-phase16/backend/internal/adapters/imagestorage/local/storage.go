package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Storage struct{ RootDir, BaseURL string }

func New(root, base string) (*Storage, error) {
	root = strings.TrimSpace(root)
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if root == "" || base == "" {
		return nil, errors.New("image storage root/base URL required")
	}
	if e := os.MkdirAll(root, 0755); e != nil {
		return nil, e
	}
	return &Storage{RootDir: root, BaseURL: base}, nil
}
func (s *Storage) Save(_ context.Context, key, contentType string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("empty image")
	}
	clean := filepath.Clean(strings.TrimLeft(key, "/"))
	if clean == "." || strings.HasPrefix(clean, "..") {
		return "", errors.New("invalid image key")
	}
	target := filepath.Join(s.RootDir, clean)
	rootAbs, _ := filepath.Abs(s.RootDir)
	targetAbs, _ := filepath.Abs(target)
	if !strings.HasPrefix(targetAbs, rootAbs+string(os.PathSeparator)) {
		return "", errors.New("image key escapes root")
	}
	if e := os.MkdirAll(filepath.Dir(target), 0755); e != nil {
		return "", e
	}
	tmp := target + ".tmp"
	if e := os.WriteFile(tmp, data, 0644); e != nil {
		return "", e
	}
	if e := os.Rename(tmp, target); e != nil {
		return "", e
	}
	return s.BaseURL + "/" + filepath.ToSlash(clean), nil
}
