package build

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadConfigMergesReleaseIntoDevelopmentWithTopLevelReplacement(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", `
manifest: lzc-manifest.yml
contentdir: dist
envs:
  - MODE=release
package_override:
  name: Release
  description: inherited only when the whole field is absent
`)
	writeTestFile(t, root, "lzc-build.dev.yml", `
contentdir: dev-dist
envs:
  - MODE=dev
package_override:
  name: Development
`)

	loaded, err := LoadConfig(context.Background(), root, "lzc-build.dev.yml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Profile != ProfileDevelopment {
		t.Fatalf("Profile = %q", loaded.Profile)
	}
	if loaded.ParentPath != filepath.Join(root, "lzc-build.yml") {
		t.Fatalf("ParentPath = %q", loaded.ParentPath)
	}
	if loaded.Config.Manifest != "lzc-manifest.yml" || loaded.Config.ContentDir != "dev-dist" {
		t.Fatalf("unexpected merged config: %#v", loaded.Config)
	}
	if !reflect.DeepEqual(loaded.Config.Envs, []string{"MODE=dev"}) {
		t.Fatalf("Envs = %#v", loaded.Config.Envs)
	}
	if !reflect.DeepEqual(loaded.Config.PackageOverride, map[string]any{"name": "Development"}) {
		t.Fatalf("PackageOverride = %#v", loaded.Config.PackageOverride)
	}
}

func TestLoadConfigDoesNotInventBaseConfigInheritance(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.base.yml", "contentdir: base-dist\n")
	writeTestFile(t, root, "lzc-build.yml", "manifest: app.yml\n")

	loaded, err := LoadConfig(context.Background(), root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ParentPath != "" {
		t.Fatalf("ParentPath = %q", loaded.ParentPath)
	}
	if loaded.Config.ContentDir != "" {
		t.Fatalf("ContentDir = %q, lzc-build.base.yml must not be inherited", loaded.Config.ContentDir)
	}
}

func TestLoadConfigExpandsExplicitEnvironmentAndNormalizesEnvs(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", `
contentdir: ${OUTPUT_DIR}
envs:
  - CHANNEL=${CHANNEL}
`)

	loaded, err := LoadConfig(context.Background(), root, "lzc-build.yml", map[string]string{
		"OUTPUT_DIR": "dist",
		"CHANNEL":    "stable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config.ContentDir != "dist" {
		t.Fatalf("ContentDir = %q", loaded.Config.ContentDir)
	}
	if !reflect.DeepEqual(loaded.BuildEnv, map[string]string{"CHANNEL": "stable"}) {
		t.Fatalf("BuildEnv = %#v", loaded.BuildEnv)
	}
}

func writeTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
