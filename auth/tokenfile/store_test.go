package tokenfile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lib-x/lzc-toolkit-go/auth/tokenfile"
)

func TestStoreRoundTripUsesPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "token.json")
	store := tokenfile.Store{Path: path}
	if err := store.Save(context.Background(), " secret-token "); err != nil {
		t.Fatal(err)
	}
	token, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "secret-token" {
		t.Fatalf("token = %q", token)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	if err := store.Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stat error = %v", err)
	}
}
