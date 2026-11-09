package file

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadOnlyStorePreservesFileAndRejectsSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	original := []byte(`{"revision":"test-value"}`)
	if err := os.WriteFile(path, original, 0400); err != nil {
		t.Fatal(err)
	}
	store, err := NewReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := store.Get(context.Background(), "revision"); !ok || value != "test-value" {
		t.Fatal("cannot read credential")
	}
	if err := store.Set(context.Background(), "revision", "replacement"); err == nil {
		t.Fatal("read-only store accepted write")
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != string(original) {
		t.Fatal("credential file changed")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0400 {
		t.Fatal("file permissions changed")
	}
}

func TestStorePersistsSecretWithOwnerOnlyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "provider-keys.json")
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Set(context.Background(), "revision", "secret-value"); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := reopened.Get(context.Background(), "revision"); !ok || value != "secret-value" {
		t.Fatal("secret was not recovered after restart")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secret file permissions = %o", info.Mode().Perm())
	}
}
