package archive_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	lpkarchive "github.com/lib-x/lzc-toolkit-go/archive"
)

type trackingWriter struct {
	bytes.Buffer
	closed bool
}

type cancelOnReadFS struct {
	fs.FS
	name   string
	cancel context.CancelFunc
}

func (f cancelOnReadFS) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil || name != f.name {
		return file, err
	}
	return &cancelOnReadFile{File: file, cancel: f.cancel}, nil
}

type cancelOnReadFile struct {
	fs.File
	cancelled bool
	cancel    context.CancelFunc
}

type cancelOnZIPFinalizationWriter struct {
	bytes.Buffer
	cancel    context.CancelFunc
	cancelled bool
}

type cancelOnZIPHeaderWriter struct {
	bytes.Buffer
	cancel    context.CancelFunc
	cancelled bool
}

func (w *cancelOnZIPHeaderWriter) Write(buffer []byte) (int, error) {
	n, err := w.Buffer.Write(buffer)
	if !w.cancelled && bytes.Contains(buffer, []byte{'P', 'K', 3, 4}) {
		w.cancelled = true
		w.cancel()
	}
	return n, err
}

type cancelOnTARTrailerWriter struct {
	bytes.Buffer
	cancel    context.CancelFunc
	cancelled bool
}

type cancelOnTARHeaderWriter struct {
	bytes.Buffer
	cancel    context.CancelFunc
	cancelled bool
}

type failHeaderWriter struct {
	bytes.Buffer
	format lpkarchive.Format
	err    error
}

func (w *failHeaderWriter) Write(buffer []byte) (int, error) {
	isZIPHeader := w.format == lpkarchive.FormatZIP && bytes.Contains(buffer, []byte{'P', 'K', 3, 4})
	isTARHeader := w.format == lpkarchive.FormatTAR && len(buffer) == 512 && !allZero(buffer)
	if isZIPHeader || isTARHeader {
		return 0, w.err
	}
	return w.Buffer.Write(buffer)
}

func (w *cancelOnTARHeaderWriter) Write(buffer []byte) (int, error) {
	n, err := w.Buffer.Write(buffer)
	if !w.cancelled && len(buffer) == 512 && !allZero(buffer) {
		w.cancelled = true
		w.cancel()
	}
	return n, err
}

func (w *cancelOnTARTrailerWriter) Write(buffer []byte) (int, error) {
	n, err := w.Buffer.Write(buffer)
	if !w.cancelled && len(buffer) >= 512 && allZero(buffer) {
		w.cancelled = true
		w.cancel()
	}
	return n, err
}

func allZero(buffer []byte) bool {
	for _, value := range buffer {
		if value != 0 {
			return false
		}
	}
	return true
}

func (w *cancelOnZIPFinalizationWriter) Write(buffer []byte) (int, error) {
	n, err := w.Buffer.Write(buffer)
	if !w.cancelled && bytes.Contains(buffer, []byte{'P', 'K', 1, 2}) {
		w.cancelled = true
		w.cancel()
	}
	return n, err
}

func (f *cancelOnReadFile) Read(buffer []byte) (int, error) {
	n, err := f.File.Read(buffer)
	if !f.cancelled {
		f.cancelled = true
		f.cancel()
	}
	return n, err
}

func (w *trackingWriter) Close() error {
	w.closed = true
	return nil
}

func TestWriteZIPIsDeterministic(t *testing.T) {
	source := testMapFS()
	options := lpkarchive.WriteOptions{Format: lpkarchive.FormatZIP, Reproducible: true}

	first := &trackingWriter{}
	firstResult, err := lpkarchive.Write(context.Background(), first, source, options)
	if err != nil {
		t.Fatal(err)
	}
	second := &trackingWriter{}
	secondResult, err := lpkarchive.Write(context.Background(), second, source, options)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("repeated ZIP writes differ")
	}
	if first.closed || second.closed {
		t.Fatal("Write closed a caller-provided writer")
	}
	assertWriteResult(t, firstResult, lpkarchive.FormatZIP, first.Bytes())
	assertWriteResult(t, secondResult, lpkarchive.FormatZIP, second.Bytes())

	zr, err := zip.NewReader(bytes.NewReader(first.Bytes()), int64(first.Len()))
	if err != nil {
		t.Fatal(err)
	}
	wantTime := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, file := range zr.File {
		if !file.Modified.UTC().Equal(wantTime) {
			t.Fatalf("%q timestamp = %v (%v)", file.Name, file.Modified, file.Modified.Location())
		}
		if file.FileInfo().IsDir() {
			if file.Name[len(file.Name)-1] != '/' {
				t.Fatalf("directory name = %q", file.Name)
			}
			continue
		}
		if file.Method != zip.Deflate {
			t.Fatalf("%q method = %d", file.Name, file.Method)
		}
		if got := file.Mode().Perm(); got != source[file.Name].Mode.Perm() {
			t.Fatalf("%q mode = %v", file.Name, got)
		}
	}
}

func TestWriteTARIsDeterministic(t *testing.T) {
	source := testMapFS()
	options := lpkarchive.WriteOptions{Format: lpkarchive.FormatTAR, Reproducible: true}

	first := &trackingWriter{}
	firstResult, err := lpkarchive.Write(context.Background(), first, source, options)
	if err != nil {
		t.Fatal(err)
	}
	second := &trackingWriter{}
	secondResult, err := lpkarchive.Write(context.Background(), second, source, options)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("repeated TAR writes differ")
	}
	if first.closed || second.closed {
		t.Fatal("Write closed a caller-provided writer")
	}
	assertWriteResult(t, firstResult, lpkarchive.FormatTAR, first.Bytes())
	assertWriteResult(t, secondResult, lpkarchive.FormatTAR, second.Bytes())

	tr := tar.NewReader(bytes.NewReader(first.Bytes()))
	var names []string
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, header.Name)
		if header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" {
			t.Fatalf("%q ownership metadata = uid:%d gid:%d uname:%q gname:%q", header.Name, header.Uid, header.Gid, header.Uname, header.Gname)
		}
		if !header.AccessTime.IsZero() || !header.ChangeTime.IsZero() {
			t.Fatalf("%q access/change time = %v/%v", header.Name, header.AccessTime, header.ChangeTime)
		}
		if !header.ModTime.Equal(time.Unix(0, 0).UTC()) {
			t.Fatalf("%q modification time = %v", header.Name, header.ModTime)
		}
	}
	wantNames := []string{"app/", "app/empty.txt", "app/manifest.yml"}
	if len(names) != len(wantNames) {
		t.Fatalf("names = %#v", names)
	}
	for i := range wantNames {
		if names[i] != wantNames[i] {
			t.Fatalf("names = %#v", names)
		}
	}
}

func TestWriteStopsWhenContextIsCancelledDuringCopy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := cancelOnReadFS{
		FS: fstest.MapFS{
			"payload.bin": &fstest.MapFile{Data: bytes.Repeat([]byte("x"), 64<<10)},
		},
		name:   "payload.bin",
		cancel: cancel,
	}
	dst := &trackingWriter{}

	_, err := lpkarchive.Write(ctx, dst, source, lpkarchive.WriteOptions{Format: lpkarchive.FormatTAR, Reproducible: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if got := err.Error(); got != "CANCELLED" {
		t.Fatalf("error text = %q", got)
	}
	if dst.closed {
		t.Fatal("Write closed a caller-provided writer")
	}
}

func TestWriteZIPDetectsCancellationDuringFinalization(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dst := &cancelOnZIPFinalizationWriter{cancel: cancel}
	source := fstest.MapFS{"empty.txt": &fstest.MapFile{}}

	_, err := lpkarchive.Write(ctx, dst, source, lpkarchive.WriteOptions{Format: lpkarchive.FormatZIP, Reproducible: true})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, lpkgo.ErrCancelled) {
		t.Fatalf("error = %v, central-directory cancellation = %v", err, dst.cancelled)
	}
	if got := err.Error(); got != "CANCELLED" {
		t.Fatalf("error text = %q", got)
	}
}

func TestWriteZIPPreservesCancellationDuringHeader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dst := &cancelOnZIPHeaderWriter{cancel: cancel}
	source := fstest.MapFS{strings.Repeat("x", 5000): &fstest.MapFile{}}

	_, err := lpkarchive.Write(ctx, dst, source, lpkarchive.WriteOptions{Format: lpkarchive.FormatZIP, Reproducible: true})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, lpkgo.ErrCancelled) {
		t.Fatalf("error = %v, header cancellation = %v", err, dst.cancelled)
	}
	if got := err.Error(); got != "CANCELLED" {
		t.Fatalf("error text = %q", got)
	}
}

func TestWriteTARDetectsCancellationDuringFinalization(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dst := &cancelOnTARTrailerWriter{cancel: cancel}
	source := fstest.MapFS{"empty.txt": &fstest.MapFile{}}

	_, err := lpkarchive.Write(ctx, dst, source, lpkarchive.WriteOptions{Format: lpkarchive.FormatTAR, Reproducible: true})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, lpkgo.ErrCancelled) {
		t.Fatalf("error = %v, trailer cancellation = %v", err, dst.cancelled)
	}
	if got := err.Error(); got != "CANCELLED" {
		t.Fatalf("error text = %q", got)
	}
}

func TestWriteTARPreservesCancellationDuringHeader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dst := &cancelOnTARHeaderWriter{cancel: cancel}
	source := fstest.MapFS{"empty.txt": &fstest.MapFile{}}

	_, err := lpkarchive.Write(ctx, dst, source, lpkarchive.WriteOptions{Format: lpkarchive.FormatTAR, Reproducible: true})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, lpkgo.ErrCancelled) {
		t.Fatalf("error = %v, header cancellation = %v", err, dst.cancelled)
	}
	if got := err.Error(); got != "CANCELLED" {
		t.Fatalf("error text = %q", got)
	}
}

func TestWriteHeaderFailureRemainsCommandFailed(t *testing.T) {
	writeErr := errors.New("header write failed")
	tests := []struct {
		name   string
		format lpkarchive.Format
		source fstest.MapFS
	}{
		{
			name:   "ZIP",
			format: lpkarchive.FormatZIP,
			source: fstest.MapFS{strings.Repeat("x", 5000): &fstest.MapFile{}},
		},
		{
			name:   "TAR",
			format: lpkarchive.FormatTAR,
			source: fstest.MapFS{"empty.txt": &fstest.MapFile{}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dst := &failHeaderWriter{format: test.format, err: writeErr}
			_, err := lpkarchive.Write(context.Background(), dst, test.source, lpkarchive.WriteOptions{Format: test.format, Reproducible: true})
			if !errors.Is(err, writeErr) || !errors.Is(err, lpkgo.ErrCommandFailed) {
				t.Fatalf("error = %v", err)
			}
			if errors.Is(err, lpkgo.ErrCancelled) {
				t.Fatalf("error unexpectedly matches cancellation: %v", err)
			}
			if got := err.Error(); got != "COMMAND_FAILED" {
				t.Fatalf("error text = %q", got)
			}
		})
	}
}

func testMapFS() fstest.MapFS {
	return fstest.MapFS{
		"app/manifest.yml": &fstest.MapFile{
			Data:    []byte("name: demo\n"),
			Mode:    0o640,
			ModTime: time.Date(2026, 7, 10, 12, 30, 0, 0, time.FixedZone("test", 2*60*60)),
		},
		"app/empty.txt": &fstest.MapFile{Mode: 0o600},
	}
}

func assertWriteResult(t *testing.T, result lpkarchive.WriteResult, format lpkarchive.Format, data []byte) {
	t.Helper()
	if result.Format != format {
		t.Fatalf("format = %q", result.Format)
	}
	if result.Size != int64(len(data)) {
		t.Fatalf("size = %d, want %d", result.Size, len(data))
	}
	if want := sha256.Sum256(data); result.SHA256 != want {
		t.Fatalf("sha256 = %x, want %x", result.SHA256, want)
	}
}
