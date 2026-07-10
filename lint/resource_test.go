package lint_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/lint"
)

func TestResourcePackageReportsMissingRequiredRoots(t *testing.T) {
	t.Parallel()

	warnings, err := lint.ResourcePackage(context.Background(), fstest.MapFS{})
	if err != nil {
		t.Fatalf("ResourcePackage() error = %v", err)
	}
	got := warningCodePaths(warnings)
	want := [][2]string{
		{"resource-package-file-missing", "package.yml"},
		{"resource-exports-missing", "exports"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResourcePackage() code/path = %#v; want %#v", got, want)
	}
}

func TestResourcePackageReportsEmptyExports(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeTestFile(t, directory, "package.yml", "package: cloud.lazycat.resources\nversion: 1.0.0\n")
	mustMkdirAll(t, filepath.Join(directory, "exports", ".hidden"))

	warnings, err := lint.ResourcePackage(context.Background(), os.DirFS(directory))
	if err != nil {
		t.Fatalf("ResourcePackage() error = %v", err)
	}
	want := [][2]string{{"resource-exports-empty", "exports"}}
	if got := warningCodePaths(warnings); !reflect.DeepEqual(got, want) {
		t.Fatalf("ResourcePackage() code/path = %#v; want %#v", got, want)
	}
}

func TestResourcePackageLimitsVisibleKinds(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeTestFile(t, directory, "package.yml", "package: cloud.lazycat.resources\nversion: 1.0.0\n")
	for index := 0; index < 101; index++ {
		writeTestFile(t, directory, fmt.Sprintf("exports/kind-%03d/default/payload", index), "payload")
	}
	writeTestFile(t, directory, "exports/.ignored/default/payload", "ignored")

	warnings, err := lint.ResourcePackage(context.Background(), os.DirFS(directory))
	if err != nil {
		t.Fatalf("ResourcePackage() error = %v", err)
	}
	want := [][2]string{{"resource-exports-too-many-kinds", "exports"}}
	if got := warningCodePaths(warnings); !reflect.DeepEqual(got, want) {
		t.Fatalf("ResourcePackage() code/path = %#v; want %#v", got, want)
	}
}

func TestResourcePackageAcceptsNestedRegularPayload(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeTestFile(t, directory, "package.yml", "package: cloud.lazycat.resources\nversion: 1.0.0\n")
	writeTestFile(t, directory, "exports/config/default/nested/payload.yml", "enabled: true\n")

	warnings, err := lint.ResourcePackage(context.Background(), os.DirFS(directory))
	if err != nil {
		t.Fatalf("ResourcePackage() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("ResourcePackage() warnings = %#v; want none", warnings)
	}
}

func TestResourcePackageHonorsCancellation(t *testing.T) {
	t.Parallel()

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := lint.ResourcePackage(cancelled, fstest.MapFS{})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, lpkgo.ErrCancelled) {
		t.Fatalf("ResourcePackage(cancelled) error = %v; want context.Canceled and CANCELLED", err)
	}
}

func TestResourcePackageRejectsNilFilesystem(t *testing.T) {
	t.Parallel()

	_, err := lint.ResourcePackage(context.Background(), nil)
	if !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("ResourcePackage(nil) error = %v; want INVALID_ARGUMENT", err)
	}
}

func TestResourcePackageValidatesVisibleExportEntries(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeTestFile(t, directory, "package.yml", "package: cloud.lazycat.resources\nversion: 1.0.0\n")
	mustMkdirAll(t, filepath.Join(directory, "exports", "BadKind"))
	writeTestFile(t, directory, "exports/file-kind", "not a directory")
	mustMkdirAll(t, filepath.Join(directory, "exports", "good", "BadID"))
	mustMkdirAll(t, filepath.Join(directory, "exports", "good", "empty"))
	writeTestFile(t, directory, "exports/good/file-id", "not a directory")
	writeTestFile(t, directory, "exports/good/valid/nested/payload.yml", "enabled: true\n")
	writeTestFile(t, directory, "exports/.hidden/ignored/payload.yml", "ignored\n")
	writeTestFile(t, directory, "exports/good/.hidden/payload.yml", "ignored\n")

	warnings, err := lint.ResourcePackage(context.Background(), os.DirFS(directory))
	if err != nil {
		t.Fatalf("ResourcePackage() error = %v", err)
	}
	got := warningCodePaths(warnings)
	want := [][2]string{
		{"resource-export-kind-invalid", "exports/BadKind"},
		{"resource-export-kind-not-directory", "exports/file-kind"},
		{"resource-export-id-invalid", "exports/good/BadID"},
		{"resource-export-payload-empty", "exports/good/empty"},
		{"resource-export-id-not-directory", "exports/good/file-id"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResourcePackage() code/path = %#v; want %#v", got, want)
	}
}

func TestResourcePackageValidatesPackageMetadata(t *testing.T) {
	t.Parallel()

	root := fstest.MapFS{
		"package.yml":                     {Data: []byte("package: Invalid.Package\nversion: '  '\n")},
		"exports/config/default/data.yml": {Data: []byte("enabled: true\n")},
	}
	warnings, err := lint.ResourcePackage(context.Background(), root)
	if err != nil {
		t.Fatalf("ResourcePackage() error = %v", err)
	}
	got := warningCodePaths(warnings)
	want := [][2]string{
		{"resource-package-name-invalid", "package.yml.package"},
		{"resource-package-version-missing", "package.yml.version"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResourcePackage() code/path = %#v; want %#v", got, want)
	}
}

func warningCodePaths(warnings []lpkgo.Warning) [][2]string {
	result := make([][2]string, len(warnings))
	for index, warning := range warnings {
		result[index] = [2]string{warning.Code, warning.Path}
	}
	return result
}

func writeTestFile(t *testing.T, root string, name string, contents string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	mustMkdirAll(t, filepath.Dir(filename))
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", name, err)
	}
}

func mustMkdirAll(t *testing.T, directory string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", directory, err)
	}
}
