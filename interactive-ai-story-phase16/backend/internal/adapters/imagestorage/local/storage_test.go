package local

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStorageWritesAtomicRelativeMediaPath(t *testing.T) {
	root := t.TempDir()
	s, e := New(root, "/media")
	if e != nil {
		t.Fatal(e)
	}
	url, e := s.Save(context.Background(), "stories/s/scenes/x/g/1.png", "image/png", []byte("bytes"))
	if e != nil {
		t.Fatal(e)
	}
	if url != "/media/stories/s/scenes/x/g/1.png" {
		t.Fatalf("url=%s", url)
	}
	if _, e = os.Stat(filepath.Join(root, "stories/s/scenes/x/g/1.png")); e != nil {
		t.Fatal(e)
	}
}
func TestStorageRejectsEscape(t *testing.T) {
	s, _ := New(t.TempDir(), "/media")
	if _, e := s.Save(context.Background(), "../../x", "image/png", []byte("x")); e == nil {
		t.Fatal("path escape accepted")
	}
}
