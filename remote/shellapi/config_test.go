package shellapi_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/remote/shellapi"
)

func TestLoadConfigReadsFilesAndEnvironmentFallback(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "shellapi_addr"), []byte(" bufnet \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "shellapi_cred"), []byte(" secret-credential \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := shellapi.LoadConfig(context.Background(), shellapi.ConfigOptions{ConfigDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	if config.Address != "bufnet" || config.Credential != "secret-credential" || config.Fallback {
		t.Fatalf("config = %#v", config)
	}

	fallback, err := shellapi.LoadConfig(context.Background(), shellapi.ConfigOptions{
		ConfigDir: t.TempDir(), Environment: map[string]string{"BOX_UID": " user-1 ", "BOX_NAME": " box-one "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !fallback.Fallback || fallback.UID != "user-1" || fallback.BoxName != "box-one" {
		t.Fatalf("fallback = %#v", fallback)
	}
}

func TestLoadConfigRejectsMissingConfiguration(t *testing.T) {
	_, err := shellapi.LoadConfig(context.Background(), shellapi.ConfigOptions{ConfigDir: t.TempDir(), Environment: map[string]string{}})
	if !errors.Is(err, lpkgo.ErrNotFound) {
		t.Fatalf("error = %#v", err)
	}
}
