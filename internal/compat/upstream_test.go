package compat_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lib-x/lpk-go/inspect"
	"github.com/lib-x/lpk-go/lint"
	"github.com/lib-x/lpk-go/lpk"
	"github.com/lib-x/lpk-go/signature"
)

func TestUpstreamFixturesOpenInspectLintAndVerify(t *testing.T) {
	fixtures := fixtureDir(t)
	tests := []struct {
		name         string
		filename     string
		layout       lpk.Layout
		packageID    string
		resourceOnly bool
		signed       bool
	}{
		{name: "v1", filename: "v1-simple.lpk", layout: lpk.LayoutV1, packageID: "cloud.lazycat.test.v1"},
		{name: "v2", filename: "v2-simple.lpk", layout: lpk.LayoutV2, packageID: "cloud.lazycat.test.v2"},
		{name: "resource-only", filename: "resource-only.lpk", layout: lpk.LayoutV2, packageID: "cloud.lazycat.test.resources", resourceOnly: true},
		{name: "signed-v2", filename: "signed-v2.lpk", layout: lpk.LayoutV2, packageID: "cloud.lazycat.test.v2", signed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filename := filepath.Join(fixtures, test.filename)
			file, err := os.Open(filename)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()

			reader, err := lpk.Open(context.Background(), file, lpk.WithFilenameHint(test.filename))
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			info, err := inspect.Reader(context.Background(), reader)
			if err != nil {
				t.Fatal(err)
			}
			if info.Layout != test.layout || info.PackageID != test.packageID || info.ResourceOnly != test.resourceOnly || info.Signed != test.signed {
				t.Fatalf("info = %#v", info)
			}

			if test.resourceOnly {
				destination := t.TempDir()
				if err := reader.Extract(context.Background(), destination); err != nil {
					t.Fatal(err)
				}
				warnings, err := lint.ResourcePackage(context.Background(), os.DirFS(destination))
				if err != nil {
					t.Fatal(err)
				}
				if len(warnings) != 0 {
					t.Fatalf("resource warnings = %#v", warnings)
				}
			}
			if test.signed {
				result, err := signature.VerifyFile(context.Background(), filename, signature.VerifyRequest{KeyID: "upstream"})
				if err != nil {
					t.Fatal(err)
				}
				if !result.Valid || result.KeyID != "upstream" {
					t.Fatalf("verify result = %#v", result)
				}
			}
		})
	}
}

func fixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	return filepath.Join(repo, "testdata", "upstream", "2.0.8")
}
