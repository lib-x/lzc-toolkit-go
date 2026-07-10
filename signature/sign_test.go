package signature_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	lpkarchive "github.com/lib-x/lzc-toolkit-go/archive"
	"github.com/lib-x/lzc-toolkit-go/lpk"
	"github.com/lib-x/lzc-toolkit-go/signature"
)

type trackingReadCloser struct {
	*bytes.Reader
	closed bool
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}

type trackingWriteCloser struct {
	bytes.Buffer
	closed bool
}

func (w *trackingWriteCloser) Close() error {
	w.closed = true
	return nil
}

func TestSignV1AndV2WritesReferenceMetadata(t *testing.T) {
	keys := generateKeys(t)
	tests := []struct {
		name    string
		request lpk.WriteRequest
		layout  lpk.Layout
		format  lpkarchive.Format
		version string
	}{
		{name: "v1", request: lpk.WriteRequest{Layout: lpk.LayoutV1, Files: v1Root()}, layout: lpk.LayoutV1, format: lpkarchive.FormatZIP, version: "1.0.0"},
		{name: "v2", request: lpk.WriteRequest{Layout: lpk.LayoutV2, Files: v2Root(), Strict: true}, layout: lpk.LayoutV2, format: lpkarchive.FormatTAR, version: "2.0.0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := writePackage(t, test.request)
			var output bytes.Buffer
			result, err := signature.Sign(context.Background(), &output, bytes.NewReader(source), signature.SignRequest{
				PrivateKeyPEM: keys.privatePEM,
				PublicKeyPEM:  keys.publicPEM,
				KeyID:         "dev",
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Layout != test.layout || result.Format != test.format || result.Size != int64(output.Len()) {
				t.Fatalf("result = %#v", result)
			}
			if got, want := result.SHA256, sha256.Sum256(output.Bytes()); got != want {
				t.Fatalf("sha256 = %x, want %x", got, want)
			}

			releaseBytes := readSignedEntry(t, output.Bytes(), "META/release.lock")
			if len(releaseBytes) == 0 || releaseBytes[len(releaseBytes)-1] == '\n' {
				t.Fatalf("release.lock should be non-empty JSON without trailing newline: %q", releaseBytes)
			}
			var lock struct {
				Schema  string `json:"schema"`
				AppID   string `json:"appid"`
				Version string `json:"version"`
				Objects []struct {
					Path   string `json:"path"`
					Digest string `json:"digest"`
					Size   int64  `json:"size"`
				} `json:"objects"`
			}
			if err := json.Unmarshal(releaseBytes, &lock); err != nil {
				t.Fatal(err)
			}
			if lock.Schema != "lazycat.lpk.release-lock/v1" || lock.AppID != "cloud.lazycat.apps.demo" || lock.Version != test.version {
				t.Fatalf("release.lock = %#v", lock)
			}
			var paths []string
			for _, object := range lock.Objects {
				paths = append(paths, object.Path)
				if object.Size <= 0 || len(object.Digest) != len("sha256:")+64 {
					t.Fatalf("object = %#v", object)
				}
			}
			if !sort.StringsAreSorted(paths) {
				t.Fatalf("paths are not sorted: %#v", paths)
			}

			if got := readSignedEntry(t, output.Bytes(), "META/keys/dev.pub"); !bytes.Equal(got, keys.publicPEM) {
				t.Fatal("embedded public key mismatch")
			}
			var sig map[string]any
			if err := json.Unmarshal(readSignedEntry(t, output.Bytes(), "META/signatures/dev.sig"), &sig); err != nil {
				t.Fatal(err)
			}
			if sig["schema"] != "lazycat.lpk.signature/v1" || sig["algorithm"] != "ed25519" || sig["key_id"] != "dev" || sig["signed_file"] != "META/release.lock" {
				t.Fatalf("signature metadata = %#v", sig)
			}
		})
	}
}

func TestSignDoesNotCloseCallerOwnedReaderOrWriter(t *testing.T) {
	keys := generateKeys(t)
	source := writePackage(t, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: v2Root(), Strict: true})
	input := &trackingReadCloser{Reader: bytes.NewReader(source)}
	output := &trackingWriteCloser{}

	if _, err := signature.Sign(context.Background(), output, input, signature.SignRequest{
		PrivateKeyPEM: keys.privatePEM,
		PublicKeyPEM:  keys.publicPEM,
		KeyID:         "dev",
	}); err != nil {
		t.Fatal(err)
	}
	if input.closed {
		t.Fatal("Sign closed caller-owned reader")
	}
	if output.closed {
		t.Fatal("Sign closed caller-owned writer")
	}
}

func TestSignRejectsAlreadySignedAndResignReplacesMeta(t *testing.T) {
	keys := generateKeys(t)
	source := writePackage(t, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: v2Root(), Strict: true})
	signed := signPackage(t, source, keys, signature.SignRequest{KeyID: "old"})

	var output bytes.Buffer
	_, err := signature.Sign(context.Background(), &output, bytes.NewReader(signed), signature.SignRequest{
		PrivateKeyPEM: keys.privatePEM,
		PublicKeyPEM:  keys.publicPEM,
		KeyID:         "new",
	})
	if !errors.Is(err, lpkgo.ErrConflict) {
		t.Fatalf("Sign already signed error = %v", err)
	}

	resigned := signPackage(t, signed, keys, signature.SignRequest{KeyID: "new", Resign: true})
	reader, err := lpk.OpenReaderAt(context.Background(), bytes.NewReader(resigned), int64(len(resigned)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	if _, err := reader.Entry(context.Background(), "META/signatures/new.sig"); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Entry(context.Background(), "META/signatures/old.sig"); !errors.Is(err, lpkgo.ErrNotFound) {
		t.Fatalf("old signature lookup error = %v", err)
	}
}

func TestSignFileSupportsInPlaceAtomicReplacement(t *testing.T) {
	keys := generateKeys(t)
	filename := filepath.Join(t.TempDir(), "package.lpk")
	if _, err := lpk.WriteFile(context.Background(), filename, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: v2Root(), Strict: true}); err != nil {
		t.Fatal(err)
	}
	result, err := signature.SignFile(context.Background(), filename, filename, signature.SignRequest{
		PrivateKeyPEM: keys.privatePEM,
		PublicKeyPEM:  keys.publicPEM,
		KeyID:         "dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if result.Size != info.Size() {
		t.Fatalf("size = %d, want %d", result.Size, info.Size())
	}
	if _, err := signature.VerifyFile(context.Background(), filename, signature.VerifyRequest{KeyID: "dev"}); err != nil {
		t.Fatal(err)
	}
}
