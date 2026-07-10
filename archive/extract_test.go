package archive_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	lpkarchive "github.com/lib-x/lzc-toolkit-go/archive"
)

func TestExtractRejectsParentTraversal(t *testing.T) {
	data := makeZIPSequence(t, []zipFixture{{name: "../escape", contents: "owned"}})
	r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	parent := t.TempDir()
	destination := filepath.Join(parent, "destination")
	err = r.Extract(context.Background(), destination)
	if !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, "escape")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside file exists or cannot be checked: %v", err)
	}
}

func TestExtractWritesRegularFiles(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "ZIP", data: makeZIP(t, map[string]string{"app/": "", "app/manifest.yml": "name: demo\n"})},
		{name: "TAR", data: makeTAR(t, map[string]string{"app/": "", "app/manifest.yml": "name: demo\n"})},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(test.data), int64(len(test.data)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.Close() })

			destination := filepath.Join(t.TempDir(), "destination")
			if err := r.Extract(context.Background(), destination); err != nil {
				t.Fatal(err)
			}
			contents, err := os.ReadFile(filepath.Join(destination, "app", "manifest.yml"))
			if err != nil {
				t.Fatal(err)
			}
			if got := string(contents); got != "name: demo\n" {
				t.Fatalf("contents = %q", got)
			}
		})
	}
}

func TestExtractRejectsEscapingLinks(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "ZIP symlink",
			data: makeExtractionZIP(t, []extractionFixture{
				{name: "dir/link", mode: 0o777 | fs.ModeSymlink, linkname: "../../outside"},
			}),
		},
		{
			name: "TAR symlink",
			data: makeExtractionTAR(t, []extractionFixture{
				{name: "dir/link", mode: 0o777 | fs.ModeSymlink, typeflag: tar.TypeSymlink, linkname: "../../outside"},
			}),
		},
		{
			name: "TAR hardlink",
			data: makeExtractionTAR(t, []extractionFixture{
				{name: "dir/link", mode: 0o644, typeflag: tar.TypeLink, linkname: "../outside"},
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(test.data), int64(len(test.data)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.Close() })

			parent := t.TempDir()
			err = r.Extract(context.Background(), filepath.Join(parent, "destination"))
			if !errors.Is(err, lpkgo.ErrInvalidArgument) {
				t.Fatalf("error = %v", err)
			}
			if _, err := os.Stat(filepath.Join(parent, "outside")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("outside file exists or cannot be checked: %v", err)
			}
		})
	}
}

func TestExtractRejectsSymlinkResolvingToVolumePrefix(t *testing.T) {
	data := makeExtractionZIP(t, []extractionFixture{
		{name: "dir/current", mode: 0o777 | fs.ModeSymlink, linkname: "../C:/escape"},
	})
	r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if err := r.Extract(context.Background(), filepath.Join(t.TempDir(), "destination")); !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("error = %v", err)
	}
}

func TestExtractCreatesLinksAfterRegularFiles(t *testing.T) {
	t.Run("ZIP symlink", func(t *testing.T) {
		data := makeExtractionZIP(t, []extractionFixture{
			{name: "current", mode: 0o777 | fs.ModeSymlink, linkname: "releases/v1"},
			{name: "releases/v1/manifest.yml", contents: "name: demo\n", mode: 0o640},
		})
		destination := extractFixture(t, data)
		contents, err := os.ReadFile(filepath.Join(destination, "current", "manifest.yml"))
		if err != nil {
			t.Fatal(err)
		}
		if got := string(contents); got != "name: demo\n" {
			t.Fatalf("contents = %q", got)
		}
	})

	t.Run("TAR symlink and forward hardlink", func(t *testing.T) {
		data := makeExtractionTAR(t, []extractionFixture{
			{name: "copy.yml", typeflag: tar.TypeLink, linkname: "releases/v1/manifest.yml", mode: 0o640},
			{name: "current", typeflag: tar.TypeSymlink, linkname: "releases/v1", mode: 0o777 | fs.ModeSymlink},
			{name: "releases/v1/manifest.yml", contents: "name: demo\n", mode: 0o640},
		})
		destination := extractFixture(t, data)
		contents, err := os.ReadFile(filepath.Join(destination, "current", "manifest.yml"))
		if err != nil {
			t.Fatal(err)
		}
		if got := string(contents); got != "name: demo\n" {
			t.Fatalf("contents = %q", got)
		}
		targetInfo, err := os.Stat(filepath.Join(destination, "releases", "v1", "manifest.yml"))
		if err != nil {
			t.Fatal(err)
		}
		copyInfo, err := os.Stat(filepath.Join(destination, "copy.yml"))
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(targetInfo, copyInfo) {
			t.Fatal("TAR hardlink does not reference the extracted target")
		}
	})
}

func TestExtractRejectsMaliciousPaths(t *testing.T) {
	formats := []struct {
		name  string
		build func(*testing.T, []extractionFixture) []byte
	}{
		{name: "ZIP", build: makeExtractionZIP},
		{name: "TAR", build: makeExtractionTAR},
	}
	paths := []struct {
		name string
		path func(string) string
	}{
		{name: "parent traversal", path: func(string) string { return "../escape" }},
		{name: "absolute", path: func(outside string) string { return filepath.ToSlash(outside) }},
		{name: "backslash traversal", path: func(string) string { return `..\escape` }},
		{name: "drive volume", path: func(string) string { return `C:\escape` }},
	}

	for _, format := range formats {
		for _, malicious := range paths {
			t.Run(format.name+"/"+malicious.name, func(t *testing.T) {
				parent := t.TempDir()
				outside := filepath.Join(parent, "outside")
				data := format.build(t, []extractionFixture{{name: malicious.path(outside), contents: "owned"}})
				r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = r.Close() })

				err = r.Extract(context.Background(), filepath.Join(parent, "destination"))
				if !errors.Is(err, lpkgo.ErrInvalidArgument) {
					t.Fatalf("error = %v", err)
				}
				if _, err := os.Stat(outside); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("outside file exists or cannot be checked: %v", err)
				}
			})
		}
	}

	t.Run("ZIP/NUL", func(t *testing.T) {
		parent := t.TempDir()
		data := makeExtractionZIP(t, []extractionFixture{{name: "bad\x00name", contents: "owned"}})
		r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = r.Close() })
		if err := r.Extract(context.Background(), filepath.Join(parent, "destination")); !errors.Is(err, lpkgo.ErrInvalidArgument) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("TAR/NUL PAX path", func(t *testing.T) {
		parent := t.TempDir()
		data := makeTARWithNULPAXPath(t)
		_, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
		if !errors.Is(err, lpkgo.ErrUnsupportedFormat) {
			t.Fatalf("error = %v", err)
		}
		if _, err := os.Stat(filepath.Join(parent, "destination")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("destination exists or cannot be checked: %v", err)
		}
	})
}

func TestExtractRejectsLinkThenFileEscape(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "ZIP",
			data: makeExtractionZIP(t, []extractionFixture{
				{name: "redirect", mode: 0o777 | fs.ModeSymlink, linkname: "../../outside"},
				{name: "redirect/owned", contents: "owned"},
			}),
		},
		{
			name: "TAR",
			data: makeExtractionTAR(t, []extractionFixture{
				{name: "redirect", mode: 0o777 | fs.ModeSymlink, typeflag: tar.TypeSymlink, linkname: "../../outside"},
				{name: "redirect/owned", contents: "owned"},
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parent := t.TempDir()
			r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(test.data), int64(len(test.data)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.Close() })
			if err := r.Extract(context.Background(), filepath.Join(parent, "destination")); !errors.Is(err, lpkgo.ErrInvalidArgument) {
				t.Fatalf("error = %v", err)
			}
			if _, err := os.Stat(filepath.Join(parent, "outside", "owned")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("outside file exists or cannot be checked: %v", err)
			}
		})
	}
}

func TestExtractRejectsDuplicateTypeChanges(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "ZIP",
			data: makeExtractionZIP(t, []extractionFixture{
				{name: "same", mode: 0o755 | fs.ModeDir},
				{name: "same", contents: "file"},
			}),
		},
		{
			name: "TAR",
			data: makeExtractionTAR(t, []extractionFixture{
				{name: "same", mode: 0o755 | fs.ModeDir, typeflag: tar.TypeDir},
				{name: "same", contents: "file"},
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(test.data), int64(len(test.data)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.Close() })
			if err := r.Extract(context.Background(), filepath.Join(t.TempDir(), "destination")); !errors.Is(err, lpkgo.ErrConflict) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestExtractRejectsUnsupportedSpecialEntries(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "ZIP device", data: makeExtractionZIP(t, []extractionFixture{{name: "device", mode: 0o600 | fs.ModeDevice}})},
		{name: "ZIP FIFO", data: makeExtractionZIP(t, []extractionFixture{{name: "fifo", mode: 0o600 | fs.ModeNamedPipe}})},
		{name: "TAR device", data: makeExtractionTAR(t, []extractionFixture{{name: "device", mode: 0o600 | fs.ModeDevice, typeflag: tar.TypeChar}})},
		{name: "TAR FIFO", data: makeExtractionTAR(t, []extractionFixture{{name: "fifo", mode: 0o600 | fs.ModeNamedPipe, typeflag: tar.TypeFifo}})},
		{name: "TAR unsupported", data: makeExtractionTAR(t, []extractionFixture{{name: "unknown", mode: 0o600, typeflag: 'V'}})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(test.data), int64(len(test.data)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.Close() })
			if err := r.Extract(context.Background(), filepath.Join(t.TempDir(), "destination")); !errors.Is(err, lpkgo.ErrUnsupportedFormat) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestExtractEnforcesEntryAndSizeLimits(t *testing.T) {
	formats := []struct {
		name  string
		build func(*testing.T, []extractionFixture) []byte
	}{
		{name: "ZIP", build: makeExtractionZIP},
		{name: "TAR", build: makeExtractionTAR},
	}
	tests := []struct {
		name     string
		fixtures []extractionFixture
		limits   lpkarchive.Limits
	}{
		{name: "too many entries", fixtures: []extractionFixture{{name: "a"}, {name: "b"}}, limits: lpkarchive.Limits{MaxEntries: 1}},
		{name: "single file too large", fixtures: []extractionFixture{{name: "large", contents: "1234"}}, limits: lpkarchive.Limits{MaxFileBytes: 3}},
		{name: "total size too large", fixtures: []extractionFixture{{name: "a", contents: "123"}, {name: "b", contents: "456"}}, limits: lpkarchive.Limits{MaxTotalBytes: 5}},
		{name: "path too long", fixtures: []extractionFixture{{name: "long-name", contents: "x"}}, limits: lpkarchive.Limits{MaxPathBytes: 8}},
	}
	for _, format := range formats {
		for _, test := range tests {
			t.Run(format.name+"/"+test.name, func(t *testing.T) {
				data := format.build(t, test.fixtures)
				r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)), lpkarchive.WithLimits(test.limits))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = r.Close() })
				if err := r.Extract(context.Background(), filepath.Join(t.TempDir(), "destination")); !errors.Is(err, lpkgo.ErrInvalidArgument) {
					t.Fatalf("error = %v", err)
				}
			})
		}
	}
}

func TestExtractRejectsRawHeaderPathOverLimit(t *testing.T) {
	formats := []struct {
		name  string
		build func(*testing.T, []extractionFixture) []byte
	}{
		{name: "ZIP", build: makeExtractionZIP},
		{name: "TAR", build: makeExtractionTAR},
	}
	for _, format := range formats {
		t.Run(format.name, func(t *testing.T) {
			data := format.build(t, []extractionFixture{{name: "././././a", contents: "data"}})
			r, err := lpkarchive.OpenReaderAt(
				context.Background(),
				bytes.NewReader(data),
				int64(len(data)),
				lpkarchive.WithLimits(lpkarchive.Limits{MaxPathBytes: 8}),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.Close() })
			destination := t.TempDir()
			if err := r.Extract(context.Background(), destination); !errors.Is(err, lpkgo.ErrInvalidArgument) {
				t.Fatalf("error = %v", err)
			}
			if _, err := os.Stat(filepath.Join(destination, "a")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("cleaned path was extracted: %v", err)
			}
		})
	}
}

func TestExtractRejectsPreexistingEscapingSymlink(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "destination")
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(destination, "redirect")); err != nil {
		t.Fatal(err)
	}
	data := makeExtractionZIP(t, []extractionFixture{{name: "redirect/owned", contents: "owned"}})
	r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if err := r.Extract(context.Background(), destination); err == nil {
		t.Fatal("Extract succeeded through a preexisting escaping symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "owned")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside file exists or cannot be checked: %v", err)
	}
}

func TestExtractRejectsPreexistingSymlinkWithinDestination(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "destination")
	actual := filepath.Join(destination, "actual")
	if err := os.MkdirAll(actual, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("actual", filepath.Join(destination, "redirect")); err != nil {
		t.Fatal(err)
	}
	data := makeExtractionTAR(t, []extractionFixture{{name: "redirect/owned", contents: "owned"}})
	r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	if err := r.Extract(context.Background(), destination); !errors.Is(err, lpkgo.ErrConflict) {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(actual, "owned")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file was written through preexisting symlink: %v", err)
	}
}

func TestExtractRejectsPreexistingFinalSymlink(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "destination")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	actual := filepath.Join(destination, "actual")
	if err := os.WriteFile(actual, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("actual", filepath.Join(destination, "payload")); err != nil {
		t.Fatal(err)
	}
	data := makeExtractionZIP(t, []extractionFixture{{name: "payload", contents: "replacement"}})
	r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	if err := r.Extract(context.Background(), destination); !errors.Is(err, lpkgo.ErrConflict) {
		t.Fatalf("error = %v", err)
	}
	contents, err := os.ReadFile(actual)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(contents); got != "original" {
		t.Fatalf("preexisting symlink target = %q", got)
	}
}

func TestExtractSelectedWritesOnlyNamedEntries(t *testing.T) {
	data := makeExtractionZIP(t, []extractionFixture{
		{name: "app/manifest.yml", contents: "name: demo\n"},
		{name: "app/secret.txt", contents: "secret"},
	})
	r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	destination := filepath.Join(t.TempDir(), "destination")
	if err := r.ExtractSelected(context.Background(), destination, []string{"app/manifest.yml"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "app", "manifest.yml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "app", "secret.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unselected file exists or cannot be checked: %v", err)
	}
	if err := r.ExtractSelected(context.Background(), destination, []string{"missing"}); !errors.Is(err, lpkgo.ErrNotFound) {
		t.Fatalf("missing selection error = %v", err)
	}
}

func TestExtractSelectedHardlinkRequiresSelectedRegularTarget(t *testing.T) {
	data := makeExtractionTAR(t, []extractionFixture{
		{name: "source", contents: "archive data", mode: 0o640},
		{name: "copy", typeflag: tar.TypeLink, linkname: "source", mode: 0o640},
	})
	r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	destination := t.TempDir()
	if err := os.WriteFile(filepath.Join(destination, "actual"), []byte("preexisting"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("actual", filepath.Join(destination, "source")); err != nil {
		t.Fatal(err)
	}
	if err := r.ExtractSelected(context.Background(), destination, []string{"copy"}); !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(destination, "copy")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("copy exists or cannot be checked: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(destination, "actual"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(contents); got != "preexisting" {
		t.Fatalf("preexisting target = %q", got)
	}
}

func TestExtractSelectedHardlinkWithSelectedRegularTarget(t *testing.T) {
	data := makeExtractionTAR(t, []extractionFixture{
		{name: "source", contents: "archive data", mode: 0o640},
		{name: "copy", typeflag: tar.TypeLink, linkname: "source", mode: 0o640},
	})
	r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	destination := t.TempDir()
	if err := r.ExtractSelected(context.Background(), destination, []string{"source", "copy"}); err != nil {
		t.Fatal(err)
	}
	sourceInfo, err := os.Stat(filepath.Join(destination, "source"))
	if err != nil {
		t.Fatal(err)
	}
	copyInfo, err := os.Stat(filepath.Join(destination, "copy"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(sourceInfo, copyInfo) {
		t.Fatal("selected hardlink does not reference the selected regular target")
	}
	contents, err := os.ReadFile(filepath.Join(destination, "copy"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(contents); got != "archive data" {
		t.Fatalf("copy contents = %q", got)
	}
}

func TestExtractPreservesModes(t *testing.T) {
	data := makeExtractionTAR(t, []extractionFixture{
		{name: "private", mode: 0o750 | fs.ModeDir, typeflag: tar.TypeDir},
		{name: "private/data", contents: "data", mode: 0o640},
	})
	destination := extractFixture(t, data)
	for name, want := range map[string]fs.FileMode{"private": 0o750, "private/data": 0o640} {
		info, err := os.Stat(filepath.Join(destination, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s mode = %v, want %v", name, got, want)
		}
	}
}

func TestExtractDetectsDeclaredZIPSizeMismatch(t *testing.T) {
	data := makeExtractionZIP(t, []extractionFixture{{name: "payload", contents: "1234"}})
	centralDirectory := bytes.Index(data, []byte{'P', 'K', 1, 2})
	if centralDirectory < 0 {
		t.Fatal("ZIP central directory not found")
	}
	binary.LittleEndian.PutUint32(data[centralDirectory+24:centralDirectory+28], 3)
	r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if err := r.Extract(context.Background(), filepath.Join(t.TempDir(), "destination")); !errors.Is(err, lpkgo.ErrIntegrityMismatch) {
		t.Fatalf("error = %v", err)
	}
}

func TestExtractHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	data := makeExtractionZIP(t, []extractionFixture{{name: "payload", contents: "data"}})
	r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	destination := filepath.Join(t.TempDir(), "destination")
	if err := r.Extract(ctx, destination); !errors.Is(err, context.Canceled) || !errors.Is(err, lpkgo.ErrCancelled) {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination exists or cannot be checked: %v", err)
	}
}

func TestExtractStopsWhenContextIsCancelledDuringCopy(t *testing.T) {
	data := makeExtractionZIP(t, []extractionFixture{{name: "payload", contents: string(bytes.Repeat([]byte("x"), 64<<10))}})
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	dataOffset, err := zr.File[0].DataOffset()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	source := &cancelRangeReaderAt{
		ReaderAt: bytes.NewReader(data),
		cancel:   cancel,
		start:    dataOffset,
		end:      dataOffset + int64(zr.File[0].CompressedSize64),
	}
	r, err := lpkarchive.OpenReaderAt(context.Background(), source, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if err := r.Extract(ctx, filepath.Join(t.TempDir(), "destination")); !errors.Is(err, context.Canceled) || !errors.Is(err, lpkgo.ErrCancelled) {
		t.Fatalf("error = %v", err)
	}
}

func TestExtractTARReadsArchiveInBoundedPasses(t *testing.T) {
	fixtures := make([]extractionFixture, 64)
	for i := range fixtures {
		fixtures[i] = extractionFixture{
			name:     fmt.Sprintf("files/%03d", i),
			contents: string(bytes.Repeat([]byte{byte(i)}, 1024)),
		}
	}
	data := makeExtractionTAR(t, fixtures)
	source := &countingReaderAt{ReaderAt: bytes.NewReader(data)}
	r, err := lpkarchive.OpenReaderAt(context.Background(), source, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if err := r.Extract(context.Background(), t.TempDir()); err != nil {
		t.Fatal(err)
	}

	// Metadata validation and extraction each make one sequential TAR pass.
	// Four archive lengths leaves stable headroom for detection and headers
	// while still failing the former per-entry full reparse behavior.
	maxRead := int64(len(data) * 4)
	if source.bytesRead > maxRead {
		t.Fatalf("ReadAt bytes = %d, want <= %d (%d archive bytes)", source.bytesRead, maxRead, len(data))
	}
}

type cancelRangeReaderAt struct {
	io.ReaderAt
	cancel context.CancelFunc
	start  int64
	end    int64
	once   sync.Once
}

type countingReaderAt struct {
	io.ReaderAt
	bytesRead int64
}

func (r *countingReaderAt) ReadAt(buffer []byte, offset int64) (int, error) {
	n, err := r.ReaderAt.ReadAt(buffer, offset)
	r.bytesRead += int64(n)
	return n, err
}

func (r *cancelRangeReaderAt) ReadAt(buffer []byte, offset int64) (int, error) {
	n, err := r.ReaderAt.ReadAt(buffer, offset)
	if offset < r.end && offset+int64(n) > r.start {
		r.once.Do(r.cancel)
	}
	return n, err
}

func extractFixture(t *testing.T, data []byte) string {
	t.Helper()
	r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	destination := filepath.Join(t.TempDir(), "destination")
	if err := r.Extract(context.Background(), destination); err != nil {
		t.Fatal(err)
	}
	return destination
}

type extractionFixture struct {
	name     string
	contents string
	linkname string
	mode     fs.FileMode
	typeflag byte
}

func makeExtractionZIP(t *testing.T, fixtures []extractionFixture) []byte {
	t.Helper()
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	for _, fixture := range fixtures {
		header := &zip.FileHeader{Name: fixture.name, Method: zip.Store}
		mode := fixture.mode
		if mode == 0 {
			mode = 0o644
		}
		header.SetMode(mode)
		entry, err := w.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		contents := fixture.contents
		if mode&fs.ModeSymlink != 0 {
			contents = fixture.linkname
		}
		if _, err := io.WriteString(entry, contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func makeExtractionTAR(t *testing.T, fixtures []extractionFixture) []byte {
	t.Helper()
	var buffer bytes.Buffer
	w := tar.NewWriter(&buffer)
	for _, fixture := range fixtures {
		typeflag := fixture.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		mode := fixture.mode.Perm()
		if mode == 0 {
			mode = 0o644
		}
		header := &tar.Header{
			Name:     fixture.name,
			Linkname: fixture.linkname,
			Mode:     int64(mode),
			Typeflag: typeflag,
		}
		if typeflag == tar.TypeReg || typeflag == tar.TypeRegA {
			header.Size = int64(len(fixture.contents))
		}
		if err := w.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, fixture.contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func makeTARWithNULPAXPath(t *testing.T) []byte {
	t.Helper()
	name := strings.Repeat("a", 110) + "x"
	var buffer bytes.Buffer
	w := tar.NewWriter(&buffer)
	if err := w.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()
	needle := []byte("path=" + name + "\n")
	index := bytes.Index(data, needle)
	if index < 0 {
		t.Fatal("PAX path record not found")
	}
	data[index+len(needle)-2] = 0
	return data
}
