package dockerarchive_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/image/dockerarchive"
	"github.com/lib-x/lzc-toolkit-go/oci"
)

func TestConvertCreatesValidMixedOCIArtifact(t *testing.T) {
	archiveData, spec := dockerArchiveFixture(t)
	destination := filepath.Join(t.TempDir(), "artifact")

	result, err := dockerarchive.Convert(context.Background(), bytes.NewReader(archiveData), destination, dockerarchive.Request{Specs: []dockerarchive.Spec{spec}})
	if err != nil {
		t.Fatal(err)
	}
	if result.ImageCount != 1 || result.EmbeddedLayerCount != 1 || result.UpstreamLayerCount != 1 {
		t.Fatalf("result = %#v", result)
	}
	report, err := oci.Validate(context.Background(), os.DirFS(destination))
	if err != nil {
		t.Fatal(err)
	}
	if report.ResolvedByAlias["app"] != spec.ImageID.String() {
		t.Fatalf("report = %#v", report)
	}
	if _, err := os.Stat(filepath.Join(destination, "images", "blobs", "sha256", spec.EmbeddedDiffIDs[0].Hex())); !os.IsNotExist(err) {
		t.Fatalf("raw embedded diff ID unexpectedly stored: %v", err)
	}
}

func TestConvertPreservesAlreadyGzippedEmbeddedLayer(t *testing.T) {
	var rawLayer bytes.Buffer
	layerWriter := tar.NewWriter(&rawLayer)
	writeTarEntry(t, layerWriter, "payload.txt", []byte("payload"))
	if err := layerWriter.Close(); err != nil {
		t.Fatal(err)
	}
	var compressedLayer bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressedLayer)
	if _, err := gzipWriter.Write(rawLayer.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	rawDigest := digest(t, rawLayer.Bytes())
	compressedDigest := digest(t, compressedLayer.Bytes())
	configData, err := json.Marshal(map[string]any{
		"architecture": "amd64",
		"os":           "linux",
		"rootfs":       map[string]any{"type": "layers", "diff_ids": []string{rawDigest.String()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	imageID := digest(t, configData)
	manifestData, err := json.Marshal([]map[string]any{{
		"Config": imageID.Hex() + ".json", "RepoTags": []string{"debug.bridge/app:1.0.0"}, "Layers": []string{"layer/app.tar.gz"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	archiveWriter := tar.NewWriter(&archive)
	writeTarEntry(t, archiveWriter, "manifest.json", manifestData)
	writeTarEntry(t, archiveWriter, imageID.Hex()+".json", configData)
	writeTarEntry(t, archiveWriter, "layer/app.tar.gz", compressedLayer.Bytes())
	if err := archiveWriter.Close(); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "artifact")
	_, err = dockerarchive.Convert(context.Background(), bytes.NewReader(archive.Bytes()), destination, dockerarchive.Request{Specs: []dockerarchive.Spec{{
		Ref: "debug.bridge/app:1.0.0", Alias: "app", ImageID: imageID, EmbeddedDiffIDs: []oci.Digest{rawDigest},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	lockFile, err := os.Open(filepath.Join(destination, "images.lock"))
	if err != nil {
		t.Fatal(err)
	}
	lock, readErr := oci.ReadLock(context.Background(), lockFile)
	closeErr := lockFile.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read lock: %v; close: %v", readErr, closeErr)
	}
	layers := lock.Images["app"].Layers
	if len(layers) != 1 || layers[0].Digest != compressedDigest || layers[0].Source != oci.LayerSourceEmbed {
		t.Fatalf("layers = %#v", layers)
	}
	blob, err := os.ReadFile(filepath.Join(destination, "images", "blobs", "sha256", compressedDigest.Hex()))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(blob, compressedLayer.Bytes()) {
		t.Fatal("embedded gzip layer bytes changed")
	}
	if _, err := oci.Validate(context.Background(), os.DirFS(destination)); err != nil {
		t.Fatal(err)
	}
}

func TestConvertRejectsGzipLayerExpandedBeyondFileLimit(t *testing.T) {
	rawLayer := bytes.Repeat([]byte("expanded-layer-content\n"), 256)
	var compressedLayer bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressedLayer)
	if _, err := gzipWriter.Write(rawLayer); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	const maxFileBytes = 1024
	if compressedLayer.Len() >= maxFileBytes || len(rawLayer) <= maxFileBytes {
		t.Fatalf("invalid test fixture sizes: compressed=%d raw=%d", compressedLayer.Len(), len(rawLayer))
	}
	rawDigest := digest(t, rawLayer)
	configData, err := json.Marshal(map[string]any{
		"architecture": "amd64",
		"os":           "linux",
		"rootfs":       map[string]any{"type": "layers", "diff_ids": []string{rawDigest.String()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	imageID := digest(t, configData)
	manifestData, err := json.Marshal([]map[string]any{{
		"Config": imageID.Hex() + ".json", "RepoTags": []string{"debug.bridge/app:1.0.0"}, "Layers": []string{"layer/app.tar.gz"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	archiveWriter := tar.NewWriter(&archive)
	writeTarEntry(t, archiveWriter, "manifest.json", manifestData)
	writeTarEntry(t, archiveWriter, imageID.Hex()+".json", configData)
	writeTarEntry(t, archiveWriter, "layer/app.tar.gz", compressedLayer.Bytes())
	if err := archiveWriter.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = dockerarchive.Convert(context.Background(), bytes.NewReader(archive.Bytes()), filepath.Join(t.TempDir(), "artifact"), dockerarchive.Request{
		Specs: []dockerarchive.Spec{{
			Ref: "debug.bridge/app:1.0.0", Alias: "app", ImageID: imageID, EmbeddedDiffIDs: []oci.Digest{rawDigest},
		}},
		Limits: dockerarchive.Limits{MaxFileBytes: maxFileBytes},
	})
	if !errors.Is(err, lpkgo.ErrInvalidConfig) {
		t.Fatalf("Convert() error = %#v, want invalid config", err)
	}
}

func TestConvertCachesRepeatedGzipLayerAgainstExpandedLimit(t *testing.T) {
	rawLayer := bytes.Repeat([]byte("shared-layer\n"), 64)
	compressedLayer := gzipBytes(t, rawLayer)
	rawDigest := digest(t, rawLayer)
	configData, err := json.Marshal(map[string]any{
		"architecture": "amd64",
		"os":           "linux",
		"rootfs": map[string]any{
			"type": "layers", "diff_ids": []string{rawDigest.String(), rawDigest.String()},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	imageID := digest(t, configData)
	manifestData, err := json.Marshal([]map[string]any{{
		"Config": imageID.Hex() + ".json", "RepoTags": []string{"debug.bridge/app:1.0.0"},
		"Layers": []string{"layer/shared.tar.gz", "layer/shared.tar.gz"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	archiveWriter := tar.NewWriter(&archive)
	writeTarEntry(t, archiveWriter, "manifest.json", manifestData)
	writeTarEntry(t, archiveWriter, imageID.Hex()+".json", configData)
	writeTarEntry(t, archiveWriter, "layer/shared.tar.gz", compressedLayer)
	if err := archiveWriter.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = dockerarchive.Convert(context.Background(), bytes.NewReader(archive.Bytes()), filepath.Join(t.TempDir(), "artifact"), dockerarchive.Request{
		Specs: []dockerarchive.Spec{{
			Ref: "debug.bridge/app:1.0.0", Alias: "app", ImageID: imageID, EmbeddedDiffIDs: []oci.Digest{rawDigest},
		}},
		Limits: dockerarchive.Limits{MaxFileBytes: int64(len(rawLayer)), MaxExpandedBytes: int64(len(rawLayer))},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConvertRejectsCumulativeExpandedLayerLimit(t *testing.T) {
	rawLayers := [][]byte{
		bytes.Repeat([]byte("first-layer\n"), 64),
		bytes.Repeat([]byte("second-layer\n"), 64),
	}
	diffIDs := []string{digest(t, rawLayers[0]).String(), digest(t, rawLayers[1]).String()}
	configData, err := json.Marshal(map[string]any{
		"architecture": "amd64",
		"os":           "linux",
		"rootfs":       map[string]any{"type": "layers", "diff_ids": diffIDs},
	})
	if err != nil {
		t.Fatal(err)
	}
	imageID := digest(t, configData)
	manifestData, err := json.Marshal([]map[string]any{{
		"Config": imageID.Hex() + ".json", "RepoTags": []string{"debug.bridge/app:1.0.0"},
		"Layers": []string{"layer/first.tar.gz", "layer/second.tar.gz"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	archiveWriter := tar.NewWriter(&archive)
	writeTarEntry(t, archiveWriter, "manifest.json", manifestData)
	writeTarEntry(t, archiveWriter, imageID.Hex()+".json", configData)
	writeTarEntry(t, archiveWriter, "layer/first.tar.gz", gzipBytes(t, rawLayers[0]))
	writeTarEntry(t, archiveWriter, "layer/second.tar.gz", gzipBytes(t, rawLayers[1]))
	if err := archiveWriter.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = dockerarchive.Convert(context.Background(), bytes.NewReader(archive.Bytes()), filepath.Join(t.TempDir(), "artifact"), dockerarchive.Request{
		Specs: []dockerarchive.Spec{{
			Ref: "debug.bridge/app:1.0.0", Alias: "app", ImageID: imageID,
			EmbeddedDiffIDs: []oci.Digest{digest(t, rawLayers[0]), digest(t, rawLayers[1])},
		}},
		Limits: dockerarchive.Limits{
			MaxFileBytes:     int64(len(rawLayers[1])),
			MaxExpandedBytes: int64(len(rawLayers[0]) + len(rawLayers[1]) - 1),
		},
	})
	if !errors.Is(err, lpkgo.ErrInvalidConfig) {
		t.Fatalf("Convert() error = %#v, want invalid config", err)
	}
}

func TestConvertRejectsDuplicateArchivePath(t *testing.T) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	writeTarEntry(t, writer, "manifest.json", []byte("[]"))
	writeTarEntry(t, writer, "manifest.json", []byte("[]"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := dockerarchive.Convert(context.Background(), bytes.NewReader(archive.Bytes()), filepath.Join(t.TempDir(), "out"), dockerarchive.Request{})
	if err == nil {
		t.Fatal("expected duplicate path error")
	}
}

func TestConvertRejectsArchiveTraversal(t *testing.T) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	writeTarEntry(t, writer, "../escape", []byte("escape"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := dockerarchive.Convert(context.Background(), bytes.NewReader(archive.Bytes()), filepath.Join(t.TempDir(), "out"), dockerarchive.Request{})
	if err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestConvertRejectsMismatchedImageID(t *testing.T) {
	archiveData, spec := dockerArchiveFixture(t)
	spec.ImageID = digest(t, []byte("different config"))

	_, err := dockerarchive.Convert(context.Background(), bytes.NewReader(archiveData), filepath.Join(t.TempDir(), "out"), dockerarchive.Request{Specs: []dockerarchive.Spec{spec}})
	if err == nil {
		t.Fatal("expected image ID mismatch")
	}
}

func TestConvertRejectsUpstreamLayerWithoutReference(t *testing.T) {
	archiveData, spec := dockerArchiveFixture(t)
	spec.Upstream = ""

	_, err := dockerarchive.Convert(context.Background(), bytes.NewReader(archiveData), filepath.Join(t.TempDir(), "out"), dockerarchive.Request{Specs: []dockerarchive.Spec{spec}})
	if err == nil {
		t.Fatal("expected missing upstream error")
	}
}

func TestConvertIsReproducible(t *testing.T) {
	archiveData, spec := dockerArchiveFixture(t)
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	for _, destination := range []string{first, second} {
		if _, err := dockerarchive.Convert(context.Background(), bytes.NewReader(archiveData), destination, dockerarchive.Request{Specs: []dockerarchive.Spec{spec}}); err != nil {
			t.Fatal(err)
		}
	}
	firstIndex, err := os.ReadFile(filepath.Join(first, "images", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	secondIndex, err := os.ReadFile(filepath.Join(second, "images", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstIndex, secondIndex) {
		t.Fatal("OCI index is not reproducible")
	}
}

func dockerArchiveFixture(t *testing.T) ([]byte, dockerarchive.Spec) {
	t.Helper()
	upstreamLayer := []byte("upstream-layer-tar")
	embeddedLayer := []byte("embedded-layer-tar")
	upstreamDigest := digest(t, upstreamLayer)
	embeddedDigest := digest(t, embeddedLayer)
	config := map[string]any{
		"architecture": "amd64",
		"os":           "linux",
		"rootfs": map[string]any{
			"type":     "layers",
			"diff_ids": []string{upstreamDigest.String(), embeddedDigest.String()},
		},
	}
	configData, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	imageID := digest(t, configData)
	configName := imageID.Hex() + ".json"
	manifestData, err := json.Marshal([]map[string]any{{
		"Config":   configName,
		"RepoTags": []string{"debug.bridge/app:1.0.0"},
		"Layers":   []string{"layer/upstream.tar", "layer/embedded.tar"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	writeTarEntry(t, writer, "manifest.json", manifestData)
	writeTarEntry(t, writer, configName, configData)
	writeTarEntry(t, writer, "layer/upstream.tar", upstreamLayer)
	writeTarEntry(t, writer, "layer/embedded.tar", embeddedLayer)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes(), dockerarchive.Spec{
		Ref:             "debug.bridge/app:1.0.0",
		Alias:           "app",
		ImageID:         imageID,
		Upstream:        "registry.example/base@" + upstreamDigest.String(),
		EmbeddedDiffIDs: []oci.Digest{embeddedDigest},
	}
}

func writeTarEntry(t *testing.T, writer *tar.Writer, name string, data []byte) {
	t.Helper()
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func digest(t *testing.T, data []byte) oci.Digest {
	t.Helper()
	sum := sha256.Sum256(data)
	digest, err := oci.ParseDigest("sha256:" + hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	return digest
}
