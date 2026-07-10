package image_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	imagebuild "github.com/lib-x/lpk-go/image"
	"github.com/lib-x/lpk-go/manifest"
)

func TestNormalizeSupportsReferenceImageConfiguration(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docker/app.Dockerfile", "FROM scratch\n")
	writeFile(t, root, "worker/Dockerfile", "FROM scratch\n")
	raw := map[string]any{
		"worker": map[string]any{
			"dockerfile":     "worker/Dockerfile",
			"builder":        "local",
			"upstream-match": "registry.example/base",
		},
		"app": "docker/app.Dockerfile",
	}
	appManifest := manifest.Manifest{
		PackageInfo: manifest.PackageInfo{Package: "cloud.lazycat.apps.Demo", Version: "1.2.3+Build"},
		Application: manifest.Application{Image: "embed:app"},
		Services: map[string]manifest.Service{
			"worker": {Image: "embed:worker"},
		},
	}

	entries, err := imagebuild.Normalize(context.Background(), root, appManifest, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Alias != "app" || entries[1].Alias != "worker" {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].Builder != imagebuild.BuilderRemote || entries[0].UpstreamMatch != "registry.lazycat.cloud" {
		t.Fatalf("app = %#v", entries[0])
	}
	if entries[0].ContextDir != filepath.Join(root, "docker") || entries[0].DockerfilePath != filepath.Join(root, "docker", "app.Dockerfile") {
		t.Fatalf("app paths = %#v", entries[0])
	}
	if entries[1].Builder != imagebuild.BuilderLocal || entries[1].UpstreamMatch != "registry.example/base" {
		t.Fatalf("worker = %#v", entries[1])
	}
	if entries[1].ImageLabel != "cloud.lazycat.apps.demo-image-worker:1.2.3-build" {
		t.Fatalf("ImageLabel = %q", entries[1].ImageLabel)
	}
}

func TestNormalizeRejectsManifestAliasMissingFromImages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Dockerfile", "FROM scratch\n")
	appManifest := manifest.Manifest{
		PackageInfo: manifest.PackageInfo{Package: "cloud.lazycat.apps.demo", Version: "1.0.0"},
		Application: manifest.Application{Image: "embed:missing"},
	}

	_, err := imagebuild.Normalize(context.Background(), root, appManifest, map[string]any{"app": "Dockerfile"})
	if err == nil {
		t.Fatal("expected missing alias error")
	}
}

func TestNormalizeRejectsDeprecatedFieldSpelling(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Dockerfile", "FROM scratch\n")
	_, err := imagebuild.Normalize(context.Background(), root, manifest.Manifest{}, map[string]any{
		"app": map[string]any{"dockerfile": "Dockerfile", "upstream_match": "registry.example"},
	})
	if err == nil {
		t.Fatal("expected deprecated spelling error")
	}
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
