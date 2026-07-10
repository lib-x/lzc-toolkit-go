package archive_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	lpkgo "github.com/lib-x/lpk-go"
	lpkarchive "github.com/lib-x/lpk-go/archive"
)

func TestWriteFileAtomicFailurePreservesExistingDestination(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "package.lpk")
	if err := os.WriteFile(destination, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := lpkarchive.WriteFileAtomic(
		context.Background(),
		destination,
		fstest.MapFS{"manifest.yml": &fstest.MapFile{Data: []byte("name: demo\n")}},
		lpkarchive.WriteOptions{},
	)
	if !errors.Is(err, lpkgo.ErrUnsupportedFormat) {
		t.Fatalf("error = %v", err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(contents); got != "original" {
		t.Fatalf("destination = %q", got)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "package.lpk" {
		t.Fatalf("temporary files remain: %#v", entries)
	}
}

func TestWriteFileAtomicReplacesDestinationAndReportsFinalFile(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "nested", "package.lpk")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := fstest.MapFS{"manifest.yml": &fstest.MapFile{Data: []byte("name: demo\n"), Mode: 0o640}}

	result, err := lpkarchive.WriteFileAtomic(
		context.Background(),
		destination,
		source,
		lpkarchive.WriteOptions{Format: lpkarchive.FormatZIP, Reproducible: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != lpkarchive.FormatZIP {
		t.Fatalf("format = %q", result.Format)
	}
	if result.Size != int64(len(contents)) {
		t.Fatalf("size = %d, want %d", result.Size, len(contents))
	}
	if want := sha256.Sum256(contents); result.SHA256 != want {
		t.Fatalf("sha256 = %x, want %x", result.SHA256, want)
	}
	if _, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents))); err != nil {
		t.Fatalf("final file is not a ZIP archive: %v", err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("mode = %v", got)
	}
	entries, err := os.ReadDir(filepath.Dir(destination))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "package.lpk" {
		t.Fatalf("temporary files remain: %#v", entries)
	}
}

func TestWriteFileAtomicCreatesDestinationParents(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "one", "two", "package.lpk")
	_, err := lpkarchive.WriteFileAtomic(
		context.Background(),
		destination,
		fstest.MapFS{"manifest.yml": &fstest.MapFile{Data: []byte("name: demo\n")}},
		lpkarchive.WriteOptions{Format: lpkarchive.FormatTAR, Reproducible: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatal(err)
	}
}

func TestWriteFileAtomicCancellationPreservesExistingDestination(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "package.lpk")
	if err := os.WriteFile(destination, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	source := cancelOnReadFS{
		FS: fstest.MapFS{
			"payload.bin": &fstest.MapFile{Data: bytes.Repeat([]byte("x"), 64<<10)},
		},
		name:   "payload.bin",
		cancel: cancel,
	}

	_, err := lpkarchive.WriteFileAtomic(
		ctx,
		destination,
		source,
		lpkarchive.WriteOptions{Format: lpkarchive.FormatTAR, Reproducible: true},
	)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, lpkgo.ErrCancelled) {
		t.Fatalf("error = %v", err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(contents); got != "original" {
		t.Fatalf("destination = %q", got)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "package.lpk" {
		t.Fatalf("temporary files remain: %#v", entries)
	}
}

func TestWriteFileAtomicRenameFailureCleansTemporaryFile(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "existing-directory")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := lpkarchive.WriteFileAtomic(
		context.Background(),
		destination,
		fstest.MapFS{"manifest.yml": &fstest.MapFile{Data: []byte("name: demo\n")}},
		lpkarchive.WriteOptions{Format: lpkarchive.FormatZIP, Reproducible: true},
	)
	if !errors.Is(err, lpkgo.ErrCommandFailed) {
		t.Fatalf("error = %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "existing-directory" {
		t.Fatalf("temporary files remain: %#v", entries)
	}
}
