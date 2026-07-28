package dockerlocal

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/lib-x/lzc-toolkit-go/oci"
)

func TestShouldResolveDockerArchiveConfigImageID(t *testing.T) {
	inspectID := cliTestDigest(t, []byte("inspect"))
	otherID := cliTestDigest(t, []byte("other"))
	tests := []struct {
		name       string
		descriptor string
		want       bool
	}{
		{name: "missing", descriptor: "", want: false},
		{name: "different", descriptor: otherID.String(), want: false},
		{name: "same", descriptor: inspectID.String(), want: true},
		{name: "malformed", descriptor: "sha256:not-a-digest", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldResolveDockerArchiveConfigImageID(test.descriptor, inspectID); got != test.want {
				t.Fatalf("shouldResolveDockerArchiveConfigImageID(%q) = %t, want %t", test.descriptor, got, test.want)
			}
		})
	}
}

func TestDockerArchiveConfigImageIDUsesMatchingRepoTag(t *testing.T) {
	wantedRef := "debug.bridge/example:latest"
	otherConfig := []byte(`{"rootfs":{"diff_ids":["sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]}}`)
	wantedConfig := []byte(`{"rootfs":{"diff_ids":["sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"]}}`)
	manifest, err := json.Marshal([]map[string]any{
		{"Config": "other.json", "RepoTags": []string{"debug.bridge/other:latest"}, "Layers": []string{"other.tar"}},
		{"Config": "wanted.json", "RepoTags": []string{wantedRef}, "Layers": []string{"wanted.tar"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "image.tar")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	cliTestTarEntry(t, writer, "wanted.json", wantedConfig)
	cliTestTarEntry(t, writer, "manifest.json", manifest)
	cliTestTarEntry(t, writer, "other.json", otherConfig)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := dockerArchiveConfigImageID(context.Background(), archivePath, wantedRef)
	if err != nil {
		t.Fatal(err)
	}
	if want := cliTestDigest(t, wantedConfig); got != want {
		t.Fatalf("dockerArchiveConfigImageID() = %s, want %s", got, want)
	}
}

func TestDockerArchiveConfigImageIDFallsBackToFirstManifest(t *testing.T) {
	config := []byte(`{"architecture":"amd64"}`)
	manifest := []byte(`[{"Config":"config.json","RepoTags":null,"Layers":[]}]`)
	archivePath := filepath.Join(t.TempDir(), "image.tar")
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	cliTestTarEntry(t, writer, "manifest.json", manifest)
	cliTestTarEntry(t, writer, "config.json", config)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := dockerArchiveConfigImageID(context.Background(), archivePath, "missing:tag")
	if err != nil {
		t.Fatal(err)
	}
	if want := cliTestDigest(t, config); got != want {
		t.Fatalf("dockerArchiveConfigImageID() = %s, want %s", got, want)
	}
}

func cliTestTarEntry(t *testing.T, writer *tar.Writer, name string, data []byte) {
	t.Helper()
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
}

func cliTestDigest(t *testing.T, data []byte) oci.Digest {
	t.Helper()
	sum := sha256.Sum256(data)
	digest, err := oci.ParseDigest(fmt.Sprintf("sha256:%x", sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	return digest
}
