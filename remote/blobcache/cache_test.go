package blobcache_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/remote/blobcache"
)

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (reader *trackingReadCloser) Close() error {
	reader.closed = true
	return nil
}

type failingReader struct {
	done bool
}

func (reader *failingReader) Read(data []byte) (int, error) {
	if reader.done {
		return 0, errors.New("source failed")
	}
	reader.done = true
	return copy(data, "partial"), nil
}

func TestCachePutVerifiesDigestAndPreservesReaderOwnership(t *testing.T) {
	cache := blobcache.New(t.TempDir())
	content := []byte("cached blob")
	digest := digestOf(content)
	source := &trackingReadCloser{Reader: bytes.NewReader(content)}

	info, err := cache.Put(context.Background(), stringsUpper(digest), source)
	if err != nil {
		t.Fatal(err)
	}
	if info.Digest != digest || info.Size != int64(len(content)) || source.closed {
		t.Fatalf("info=%#v source.closed=%v", info, source.closed)
	}
	has, err := cache.Has(context.Background(), digest)
	if err != nil || !has {
		t.Fatalf("Has() = %v, %v", has, err)
	}
	opened, err := cache.Open(context.Background(), digest)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(opened)
	closeErr := opened.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(got, content) {
		t.Fatalf("content=%q readErr=%v closeErr=%v", got, readErr, closeErr)
	}
}

func TestCacheRejectsMismatchAndPartialWrites(t *testing.T) {
	root := t.TempDir()
	cache := blobcache.New(root)
	digest := digestOf([]byte("expected"))

	if _, err := cache.Put(context.Background(), digest, bytes.NewReader([]byte("other"))); !errors.Is(err, lpkgo.ErrIntegrityMismatch) {
		t.Fatalf("mismatch error = %#v", err)
	}
	if _, err := cache.Put(context.Background(), digest, &failingReader{}); !errors.Is(err, lpkgo.ErrCommandFailed) {
		t.Fatalf("source error = %#v", err)
	}
	has, err := cache.Has(context.Background(), digest)
	if err != nil || has {
		t.Fatalf("Has() = %v, %v", has, err)
	}
	matches, err := filepath.Glob(filepath.Join(root, ".lzc-cli-cache", "blobs", "sha256", ".tmp-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files = %#v, err=%v", matches, err)
	}
}

func TestCachePutFileCopyAndImportOCI(t *testing.T) {
	root := t.TempDir()
	cache := blobcache.New(root)
	filename := filepath.Join(root, "source.blob")
	if err := os.WriteFile(filename, []byte("file blob"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := cache.PutFile(context.Background(), filename)
	if err != nil {
		t.Fatal(err)
	}
	var copied bytes.Buffer
	if err := cache.CopyTo(context.Background(), fileInfo.Digest, &copied); err != nil {
		t.Fatal(err)
	}
	if copied.String() != "file blob" {
		t.Fatalf("copied = %q", copied.String())
	}

	ociContent := []byte("oci blob")
	ociDigest := digestOf(ociContent)
	layout := fstest.MapFS{
		"blobs/sha256/" + ociDigest[len("sha256:"):]: {Data: ociContent, Mode: 0o644},
		"blobs/sha256/not-a-digest":                  {Data: []byte("ignored"), Mode: 0o644},
	}
	imported, err := cache.ImportOCI(context.Background(), layout)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 1 || imported[0].Digest != ociDigest {
		t.Fatalf("imported = %#v", imported)
	}
}

func TestCacheValidatesInputsAndCancellation(t *testing.T) {
	root := t.TempDir()
	cache := blobcache.New(root)
	for _, digest := range []string{"", "sha256:../bad", "sha512:" + fmt.Sprintf("%064d", 0)} {
		if _, err := cache.Has(context.Background(), digest); !errors.Is(err, lpkgo.ErrInvalidArgument) {
			t.Fatalf("Has(%q) error = %#v", digest, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.Put(ctx, digestOf(nil), bytes.NewReader(nil)); !errors.Is(err, lpkgo.ErrCancelled) {
		t.Fatalf("cancelled Put error = %#v", err)
	}
	if err := cache.CopyTo(context.Background(), digestOf([]byte("missing")), io.Discard); !errors.Is(err, lpkgo.ErrNotFound) {
		t.Fatalf("missing CopyTo error = %#v", err)
	}

	symlinkDigest := digestOf([]byte("symlink target"))
	blobDir := filepath.Join(root, ".lzc-cli-cache", "blobs", "sha256")
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "outside")
	if err := os.WriteFile(target, []byte("symlink target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(blobDir, symlinkDigest[len("sha256:"):])); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := cache.Open(context.Background(), symlinkDigest); !errors.Is(err, lpkgo.ErrIntegrityMismatch) {
		t.Fatalf("symlink Open error = %#v", err)
	}
}

func digestOf(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + fmt.Sprintf("%x", sum)
}

func stringsUpper(value string) string {
	result := []byte(value)
	for index, current := range result {
		if current >= 'a' && current <= 'f' {
			result[index] = current - ('a' - 'A')
		}
	}
	return string(result)
}
