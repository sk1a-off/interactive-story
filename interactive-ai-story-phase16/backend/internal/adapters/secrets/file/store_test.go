package file

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
