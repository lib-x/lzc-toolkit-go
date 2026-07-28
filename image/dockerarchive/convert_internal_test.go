package dockerarchive

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/oci"
)

func TestInspectDockerLayerPreservesCancellation(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "layer.tar.gz")
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(bytes.Repeat([]byte("layer\n"), 64)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := inspectDockerLayer(ctx, filename, 1<<20)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, lpkgo.ErrCancelled) {
		t.Fatalf("inspectDockerLayer() error = %#v, want cancellation", err)
	}
}

func TestGzipBlobPreservesDeadline(t *testing.T) {
	source := filepath.Join(t.TempDir(), "layer.tar")
	if err := os.WriteFile(source, bytes.Repeat([]byte("layer\n"), 64), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, _, err := gzipBlob(ctx, source, t.TempDir())
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, lpkgo.ErrDeadlineExceeded) {
		t.Fatalf("gzipBlob() error = %#v, want deadline exceeded", err)
	}
}

func TestCopyBlobPreservesCancellation(t *testing.T) {
	data := bytes.Repeat([]byte("layer\n"), 64)
	source := filepath.Join(t.TempDir(), "layer.tar.gz")
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest, err := oci.ParseDigest(fmt.Sprintf("sha256:%x", sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = copyBlob(ctx, source, t.TempDir(), digest, int64(len(data)))
	if !errors.Is(err, context.Canceled) || !errors.Is(err, lpkgo.ErrCancelled) {
		t.Fatalf("copyBlob() error = %#v, want cancellation", err)
	}
}
