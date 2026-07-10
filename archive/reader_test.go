package archive_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"sort"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	lpkarchive "github.com/lib-x/lzc-toolkit-go/archive"
)

type trackingReader struct {
	io.Reader
	closed bool
}

type trackingReaderAt struct {
	io.ReaderAt
	closed bool
}

func (r *trackingReaderAt) Close() error {
	r.closed = true
	return nil
}

func (r *trackingReader) Close() error {
	r.closed = true
	return nil
}

func TestOpenZIPListsAndOpensEntries(t *testing.T) {
	data := makeZIP(t, map[string]string{
		"app/":             "",
		"app/manifest.yml": "name: demo\n",
	})
	input := &trackingReader{Reader: bytes.NewBuffer(data)}
	tempDir := t.TempDir()

	r, err := lpkarchive.Open(context.Background(), input, lpkarchive.WithTempDir(tempDir), lpkarchive.WithFilenameHint("demo.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Format(); got != lpkarchive.FormatZIP {
		t.Fatalf("format = %q", got)
	}
	if got := r.Size(); got != int64(len(data)) {
		t.Fatalf("size = %d, want %d", got, len(data))
	}

	entries, err := r.Entries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].Name != "app" || entries[0].Type != lpkarchive.EntryDirectory {
		t.Fatalf("directory entry = %#v", entries[0])
	}
	if entries[1].Name != "app/manifest.yml" || entries[1].Type != lpkarchive.EntryRegular {
		t.Fatalf("file entry = %#v", entries[1])
	}

	entry, err := r.OpenEntry(context.Background(), "app/manifest.yml")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := entry.Close(); err != nil {
		t.Fatal(err)
	}
	if got := string(contents); got != "name: demo\n" {
		t.Fatalf("contents = %q", got)
	}

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if input.closed {
		t.Fatal("Open closed the caller-provided reader")
	}
	files, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("temporary files remain after Close: %#v", files)
	}
}

func TestOpenReaderAtTARListsAndOpensEntries(t *testing.T) {
	data := makeTAR(t, map[string]string{
		"app/":            "",
		"app/package.yml": "name: demo\n",
	})
	input := &trackingReaderAt{ReaderAt: bytes.NewReader(data)}

	r, err := lpkarchive.OpenReaderAt(context.Background(), input, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Format(); got != lpkarchive.FormatTAR {
		t.Fatalf("format = %q", got)
	}
	entries, err := r.Entries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name != "app" || entries[1].Name != "app/package.yml" {
		t.Fatalf("entries = %#v", entries)
	}

	entry, err := r.OpenEntry(context.Background(), "app/package.yml")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := entry.Close(); err != nil {
		t.Fatal(err)
	}
	if got := string(contents); got != "name: demo\n" {
		t.Fatalf("contents = %q", got)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if input.closed {
		t.Fatal("OpenReaderAt closed the caller-provided reader")
	}
}

func TestOpenRejectsInputLargerThanLimitAndCleansSpool(t *testing.T) {
	data := makeZIP(t, map[string]string{"manifest.yml": "name: demo\n"})
	tempDir := t.TempDir()
	input := &trackingReader{Reader: bytes.NewBuffer(data)}

	_, err := lpkarchive.Open(
		context.Background(),
		input,
		lpkarchive.WithLimits(lpkarchive.Limits{MaxInputBytes: int64(len(data) - 1)}),
		lpkarchive.WithTempDir(tempDir),
	)
	if !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("error = %v", err)
	}
	if got := err.Error(); got != "INVALID_ARGUMENT" {
		t.Fatalf("error text = %q", got)
	}
	if input.closed {
		t.Fatal("Open closed the caller-provided reader")
	}
	files, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("temporary files remain: %#v", files)
	}

	_, err = lpkarchive.OpenReaderAt(
		context.Background(),
		bytes.NewReader(data),
		int64(len(data)),
		lpkarchive.WithLimits(lpkarchive.Limits{MaxInputBytes: int64(len(data) - 1)}),
	)
	if !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("OpenReaderAt error = %v", err)
	}
}

func TestDefaultLimitsAndNegativeValidation(t *testing.T) {
	want := lpkarchive.Limits{
		MaxInputBytes:    32 << 30,
		MaxEntries:       1_000_000,
		MaxFileBytes:     16 << 30,
		MaxTotalBytes:    64 << 30,
		MaxPathBytes:     4096,
		MaxDocumentBytes: 16 << 20,
	}
	if got := lpkarchive.DefaultLimits(); got != want {
		t.Fatalf("DefaultLimits = %#v", got)
	}

	negativeLimits := []lpkarchive.Limits{
		{MaxInputBytes: -1},
		{MaxEntries: -1},
		{MaxFileBytes: -1},
		{MaxTotalBytes: -1},
		{MaxPathBytes: -1},
		{MaxDocumentBytes: -1},
	}
	for _, limits := range negativeLimits {
		_, err := lpkarchive.OpenReaderAt(
			context.Background(),
			bytes.NewReader([]byte("PK")),
			2,
			lpkarchive.WithLimits(limits),
		)
		if !errors.Is(err, lpkgo.ErrInvalidArgument) {
			t.Fatalf("limits %#v error = %v", limits, err)
		}
	}
}

func TestEntriesReportsZIPSymlinkTarget(t *testing.T) {
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	header := &zip.FileHeader{Name: "current"}
	header.SetMode(0o777 | fs.ModeSymlink)
	entry, err := w.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(entry, "releases/v1"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	entries, err := r.Entries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Type != lpkarchive.EntrySymlink || entries[0].Linkname != "releases/v1" {
		t.Fatalf("entries = %#v", entries)
	}
	if _, err := r.OpenEntry(context.Background(), "current"); !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("OpenEntry error = %v", err)
	}
}

func TestEntriesEnforcesLimitsAndNormalizedDuplicates(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		limits lpkarchive.Limits
		want   error
	}{
		{
			name:   "entry count",
			data:   makeZIP(t, map[string]string{"a": "1", "b": "2"}),
			limits: lpkarchive.Limits{MaxEntries: 1},
			want:   lpkgo.ErrInvalidArgument,
		},
		{
			name:   "file size",
			data:   makeZIP(t, map[string]string{"a": "1234"}),
			limits: lpkarchive.Limits{MaxFileBytes: 3},
			want:   lpkgo.ErrInvalidArgument,
		},
		{
			name:   "total size",
			data:   makeZIP(t, map[string]string{"a": "123", "b": "456"}),
			limits: lpkarchive.Limits{MaxTotalBytes: 5},
			want:   lpkgo.ErrInvalidArgument,
		},
		{
			name:   "path size",
			data:   makeZIP(t, map[string]string{"long-name": "1"}),
			limits: lpkarchive.Limits{MaxPathBytes: 8},
			want:   lpkgo.ErrInvalidArgument,
		},
		{
			name: "normalized duplicate",
			data: makeZIPSequence(t, []zipFixture{
				{name: "app\\manifest.yml", contents: "one"},
				{name: "app/manifest.yml", contents: "two"},
			}),
			want: lpkgo.ErrConflict,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, err := lpkarchive.OpenReaderAt(
				context.Background(),
				bytes.NewReader(test.data),
				int64(len(test.data)),
				lpkarchive.WithLimits(test.limits),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.Close() })
			if _, err := r.Entries(context.Background()); !errors.Is(err, test.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestOpenRejectsUnsupportedFormat(t *testing.T) {
	_, err := lpkarchive.Open(context.Background(), bytes.NewBufferString("not an archive"))
	if !errors.Is(err, lpkgo.ErrUnsupportedFormat) {
		t.Fatalf("error = %v", err)
	}
	if got := err.Error(); got != "UNSUPPORTED_FORMAT" {
		t.Fatalf("error text = %q", got)
	}
}

func TestOpenFileChecksCancelledContextBeforeFilesystem(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := lpkarchive.OpenFile(ctx, t.TempDir()+"/missing.zip")
	if !errors.Is(err, context.Canceled) || !errors.Is(err, lpkgo.ErrCancelled) {
		t.Fatalf("error = %v", err)
	}
	if got := err.Error(); got != "CANCELLED" {
		t.Fatalf("error text = %q", got)
	}
}

func TestOpenEntryStopsReadingWhenContextIsCancelled(t *testing.T) {
	data := makeZIP(t, map[string]string{"manifest.yml": "name: demo\n"})
	r, err := lpkarchive.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	entry, err := r.OpenEntry(ctx, "manifest.yml")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = entry.Close() })
	cancel()
	if _, err := entry.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func makeZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	fixtures := make([]zipFixture, 0, len(names))
	for _, name := range names {
		fixtures = append(fixtures, zipFixture{name: name, contents: files[name]})
	}
	return makeZIPSequence(t, fixtures)
}

type zipFixture struct {
	name     string
	contents string
}

func makeZIPSequence(t *testing.T, fixtures []zipFixture) []byte {
	t.Helper()
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	for _, fixture := range fixtures {
		header := &zip.FileHeader{Name: fixture.name, Method: zip.Store}
		if fixture.name[len(fixture.name)-1] == '/' {
			header.SetMode(0o755 | fs.ModeDir)
		} else {
			header.SetMode(0o644)
		}
		entry, err := w.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, fixture.contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func makeTAR(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	w := tar.NewWriter(&buffer)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		contents := files[name]
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(contents))}
		if name[len(name)-1] == '/' {
			header.Typeflag = tar.TypeDir
			header.Mode = 0o755
			header.Size = 0
		} else {
			header.Typeflag = tar.TypeReg
		}
		if err := w.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
