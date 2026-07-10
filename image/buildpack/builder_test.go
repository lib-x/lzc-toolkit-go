package buildpack_test

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/build"
	"github.com/lib-x/lzc-toolkit-go/image/buildpack"
	"github.com/lib-x/lzc-toolkit-go/manifest"
	"github.com/lib-x/lzc-toolkit-go/oci"
	"github.com/lib-x/lzc-toolkit-go/remote"
	"github.com/lib-x/lzc-toolkit-go/remote/blobcache"
)

type fakeBackend struct {
	info          remote.BackendInfo
	buildResult   remote.BuildPackResult
	contextNames  []string
	contextFiles  map[string]string
	contextDigest oci.Digest
	blobs         map[oci.Digest][]byte
	missingRemote map[oci.Digest]bool
	missingAll    bool
	getCalls      []oci.Digest
}

func (backend *fakeBackend) Info(context.Context) (remote.BackendInfo, error) {
	return backend.info, nil
}

func (backend *fakeBackend) BuildPack(_ context.Context, request remote.BuildPackRequest) (remote.BuildPackResult, error) {
	data, err := io.ReadAll(request.Context)
	if err != nil {
		return remote.BuildPackResult{}, err
	}
	backend.contextDigest = digestBytes(data)
	if request.ContextDigest != backend.contextDigest {
		return remote.BuildPackResult{}, errors.New("context digest mismatch")
	}
	backend.contextFiles = make(map[string]string)
	reader := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return remote.BuildPackResult{}, err
		}
		backend.contextNames = append(backend.contextNames, header.Name)
		if header.Typeflag == tar.TypeReg {
			content, _ := io.ReadAll(reader)
			backend.contextFiles[header.Name] = string(content)
		}
	}
	return backend.buildResult, nil
}

func (backend *fakeBackend) PackImagesManifest(_ context.Context, specs []remote.PackImageSpec) (remote.PackManifest, error) {
	spec := specs[0]
	config := backend.blobs[spec.ImageID]
	layer := backend.buildResult.DiffIDs[0]
	imageManifest := oci.Manifest{
		SchemaVersion: 2,
		MediaType:     oci.MediaTypeImageManifest,
		Config:        oci.Descriptor{MediaType: oci.MediaTypeImageConfig, Digest: spec.ImageID, Size: int64(len(config))},
		Layers:        []oci.Descriptor{{MediaType: oci.MediaTypeImageLayer, Digest: layer, Size: 123}},
	}
	var manifestData bytes.Buffer
	if err := oci.WriteManifest(context.Background(), &manifestData, imageManifest); err != nil {
		return remote.PackManifest{}, err
	}
	manifestDigest := digestBytes(manifestData.Bytes())
	backend.blobs[manifestDigest] = manifestData.Bytes()
	return remote.PackManifest{
		Blobs: []remote.PackBlob{
			{Digest: spec.ImageID, Size: int64(len(config))},
			{Digest: manifestDigest, Size: int64(manifestData.Len())},
		},
		Index: oci.Index{SchemaVersion: 2, Manifests: []oci.Descriptor{{
			MediaType: oci.MediaTypeImageManifest,
			Digest:    manifestDigest,
			Size:      int64(manifestData.Len()),
			Annotations: map[string]string{
				oci.AnnotationRefName: spec.Alias,
			},
		}}},
		LockImages: map[string]oci.LockImage{
			spec.Alias: {
				ImageID:  spec.ImageID,
				Upstream: spec.Upstream,
				Layers:   []oci.LockLayer{{Digest: layer, Source: oci.LayerSourceUpstream}},
			},
		},
	}, nil
}

func (backend *fakeBackend) BlobCheck(_ context.Context, digests []oci.Digest) ([]oci.Digest, error) {
	if backend.missingAll {
		return append([]oci.Digest(nil), digests...), nil
	}
	var missing []oci.Digest
	for _, digest := range digests {
		if backend.missingRemote[digest] {
			missing = append(missing, digest)
		}
	}
	return missing, nil
}

func (backend *fakeBackend) BlobGet(_ context.Context, digest oci.Digest, destination io.Writer) error {
	backend.getCalls = append(backend.getCalls, digest)
	content, exists := backend.blobs[digest]
	if !exists {
		return errors.New("missing blob")
	}
	_, err := destination.Write(content)
	return err
}

func TestBuilderBuildsRemoteOCIArtifactWithBlobCache(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\nCOPY data.txt /data.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("must-not-upload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".dockerignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := []byte(`{"rootfs":{"type":"layers","diff_ids":[]}}`)
	configDigest := digestBytes(config)
	layer := digestBytes([]byte("upstream-layer"))
	backend := &fakeBackend{
		info: remote.BackendInfo{Version: "1.0.5", Platform: remote.Platform{OS: "linux", Architecture: "amd64"}},
		buildResult: remote.BuildPackResult{
			Tag: "debug.bridge/demo", ArchiveKey: "archive-key", ImageID: configDigest,
			DiffIDs: []oci.Digest{layer}, BaseRepoDigest: "registry.lazycat.cloud/base/demo@" + layer.String(), BaseDiffIDs: []oci.Digest{layer},
		},
		blobs: make(map[oci.Digest][]byte), missingRemote: make(map[oci.Digest]bool),
	}
	backend.blobs[configDigest] = config
	cache := blobcache.New(root)
	if _, err := cache.Put(context.Background(), configDigest.String(), bytes.NewReader(config)); err != nil {
		t.Fatal(err)
	}
	builder := buildpack.New(backend, cache)

	artifact, err := builder.Build(context.Background(), build.ImageBuildRequest{
		Root:   root,
		Config: map[string]any{"web": map[string]any{"dockerfile": "Dockerfile", "builder": "remote"}},
		Manifest: manifest.Manifest{
			PackageInfo: manifest.PackageInfo{Package: "cloud.lazycat.apps.demo", Version: "1.0.0"},
			Application: manifest.Application{Image: "embed:web"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer artifact.Close()
	if backend.contextFiles["Dockerfile"] == "" || backend.contextFiles["data.txt"] != "payload" {
		t.Fatalf("context files = %#v", backend.contextFiles)
	}
	if _, exists := backend.contextFiles["secret.txt"]; exists {
		t.Fatalf("unreferenced secret was included: %#v", backend.contextFiles)
	}
	if _, exists := backend.contextFiles["ignored.txt"]; exists {
		t.Fatalf("dockerignored file was included: %#v", backend.contextFiles)
	}
	report, err := oci.Validate(context.Background(), artifact.FS())
	if err != nil {
		t.Fatal(err)
	}
	if report.ImageCount != 1 || report.UpstreamLayerCount != 1 || report.ResolvedByAlias["web"] != configDigest.String() {
		t.Fatalf("report = %#v", report)
	}
	if len(backend.getCalls) != 1 || backend.getCalls[0] == configDigest {
		t.Fatalf("blob get calls = %#v", backend.getCalls)
	}
}

func TestBuilderRequiresRemoteCapabilities(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backend := &fakeBackend{info: remote.BackendInfo{Version: "1.0.3", Platform: remote.Platform{OS: "linux", Architecture: "amd64"}}}
	builder := buildpack.New(backend, blobcache.New(root))
	_, err := builder.Build(context.Background(), build.ImageBuildRequest{
		Root:   root,
		Config: map[string]any{"web": map[string]any{"dockerfile": "Dockerfile", "builder": "remote"}},
		Manifest: manifest.Manifest{
			PackageInfo: manifest.PackageInfo{Package: "cloud.lazycat.apps.demo", Version: "1.0.0"},
			Application: manifest.Application{Image: "embed:web"},
		},
	})
	if !errors.Is(err, lpkgo.ErrIncompatibleBackend) {
		t.Fatalf("error = %#v", err)
	}
}

func TestBuilderRejectsBlobMissingOnBackend(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := []byte(`{"rootfs":{"type":"layers","diff_ids":[]}}`)
	configDigest := digestBytes(config)
	layer := digestBytes([]byte("upstream-layer"))
	backend := &fakeBackend{
		info: remote.BackendInfo{Version: "1.0.5", Platform: remote.Platform{OS: "linux", Architecture: "amd64"}},
		buildResult: remote.BuildPackResult{
			Tag: "debug.bridge/demo", ArchiveKey: "archive-key", ImageID: configDigest,
			DiffIDs: []oci.Digest{layer}, BaseRepoDigest: "registry.lazycat.cloud/base/demo@" + layer.String(), BaseDiffIDs: []oci.Digest{layer},
		},
		blobs: map[oci.Digest][]byte{configDigest: config}, missingAll: true,
	}
	builder := buildpack.New(backend, blobcache.New(root))
	_, err := builder.Build(context.Background(), build.ImageBuildRequest{
		Root:   root,
		Config: map[string]any{"web": map[string]any{"dockerfile": "Dockerfile", "builder": "remote"}},
		Manifest: manifest.Manifest{
			PackageInfo: manifest.PackageInfo{Package: "cloud.lazycat.apps.demo", Version: "1.0.0"},
			Application: manifest.Application{Image: "embed:web"},
		},
	})
	if !errors.Is(err, lpkgo.ErrNotFound) {
		t.Fatalf("error = %#v", err)
	}
}

func digestBytes(data []byte) oci.Digest {
	sum := sha256.Sum256(data)
	digest, _ := oci.ParseDigest(fmt.Sprintf("sha256:%x", sum))
	return digest
}
