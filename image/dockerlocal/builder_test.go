package dockerlocal_test

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/lib-x/lzc-toolkit-go/build"
	"github.com/lib-x/lzc-toolkit-go/image/dockerlocal"
	"github.com/lib-x/lzc-toolkit-go/manifest"
	"github.com/lib-x/lzc-toolkit-go/oci"
)

func TestBuilderImplementsBuildImageAdapterWithFakeEngine(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM registry.example/base:1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture, metadata := localArchiveFixture(t)
	engine := &fakeEngine{archive: fixture, result: metadata}
	builder := dockerlocal.New(engine)
	request := build.ImageBuildRequest{
		Root: root,
		Config: map[string]any{
			"app": map[string]any{"dockerfile": "Dockerfile", "builder": "local", "upstream-match": "registry.example"},
		},
		Manifest: manifest.Manifest{
			PackageInfo: manifest.PackageInfo{Package: "cloud.lazycat.apps.demo", Version: "1.0.0"},
			Application: manifest.Application{Image: "embed:app"},
		},
	}

	artifact, err := builder.Build(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer artifact.Close()
	if engine.buildCalls != 1 || engine.platform != "linux/amd64" {
		t.Fatalf("buildCalls=%d platform=%q", engine.buildCalls, engine.platform)
	}
	if !reflect.DeepEqual(engine.savedRefs, []string{metadata.Ref}) {
		t.Fatalf("saved refs = %#v", engine.savedRefs)
	}
	if !reflect.DeepEqual(engine.removedRefs, []string{metadata.Ref}) {
		t.Fatalf("removed refs = %#v", engine.removedRefs)
	}
	report, err := oci.Validate(context.Background(), artifact.FS())
	if err != nil {
		t.Fatal(err)
	}
	if report.ResolvedByAlias["app"] != metadata.ImageID.String() || report.EmbeddedLayerCount != 1 || report.UpstreamLayerCount != 1 {
		t.Fatalf("report = %#v", report)
	}
}

type fakeEngine struct {
	archive     []byte
	result      dockerlocal.BuildResult
	buildCalls  int
	platform    string
	savedRefs   []string
	removedRefs []string
}

func (e *fakeEngine) Build(_ context.Context, request dockerlocal.BuildRequest) (dockerlocal.BuildResult, error) {
	e.buildCalls++
	e.platform = request.Platform
	return e.result, nil
}

func (e *fakeEngine) Save(_ context.Context, refs []string, destination io.Writer) error {
	e.savedRefs = append([]string(nil), refs...)
	_, err := destination.Write(e.archive)
	return err
}

func (e *fakeEngine) Remove(_ context.Context, ref string) error {
	e.removedRefs = append(e.removedRefs, ref)
	return nil
}

func localArchiveFixture(t *testing.T) ([]byte, dockerlocal.BuildResult) {
	t.Helper()
	upstreamLayer := []byte("base-layer")
	embeddedLayer := []byte("app-layer")
	upstreamDigest := localDigest(t, upstreamLayer)
	embeddedDigest := localDigest(t, embeddedLayer)
	configData, err := json.Marshal(map[string]any{
		"architecture": "amd64",
		"os":           "linux",
		"rootfs":       map[string]any{"type": "layers", "diff_ids": []string{upstreamDigest.String(), embeddedDigest.String()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	imageID := localDigest(t, configData)
	ref := "debug.bridge/cloud.lazycat.apps.demo-image-app:1.0.0"
	manifestData, err := json.Marshal([]map[string]any{{
		"Config": imageID.Hex() + ".json", "RepoTags": []string{ref}, "Layers": []string{"base.tar", "app.tar"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer := tar.NewWriter(&output)
	localTarEntry(t, writer, "manifest.json", manifestData)
	localTarEntry(t, writer, imageID.Hex()+".json", configData)
	localTarEntry(t, writer, "base.tar", upstreamLayer)
	localTarEntry(t, writer, "app.tar", embeddedLayer)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes(), dockerlocal.BuildResult{
		Ref:             ref,
		ImageID:         imageID,
		DiffIDs:         []oci.Digest{upstreamDigest, embeddedDigest},
		Upstream:        "registry.example/base@" + upstreamDigest.String(),
		UpstreamDiffIDs: []oci.Digest{upstreamDigest},
	}
}

func localTarEntry(t *testing.T, writer *tar.Writer, name string, data []byte) {
	t.Helper()
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
}

func localDigest(t *testing.T, data []byte) oci.Digest {
	t.Helper()
	sum := sha256.Sum256(data)
	digest, err := oci.ParseDigest("sha256:" + hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	return digest
}
