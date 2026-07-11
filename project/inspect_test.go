package project_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/lpk"
	"github.com/lib-x/lzc-toolkit-go/project"
)

func TestInspectLocalV2TemplatedServiceProject(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "lzc-build.yml", `manifest: lzc-manifest.yml
contentdir: content
icon: icon.png
deploy_params: lzc-deploy-params.yml
pkgout: dist
lpkPath: dist/demo.lpk
images:
  web:
    context: .
resource_exports:
  - kind: skills
    source: resources/skills
`)
	writeProjectFile(t, root, "package.yml", `package: community.lazycat.app.demo
version: 1.2.3
name: Demo
description: Demo service
author: example
license: MIT
homepage: https://example.invalid
min_os_version: 1.5.0
locales:
  zh:
    name: 示例
  en:
    name: Demo
`)
	writeProjectFile(t, root, "lzc-manifest.yml", `application:
  subdomain: demo
  upstreams:
    - location: /
      backend: http://web:8080/
services:
  db:
    image: postgres:16
    healthcheck:
      test: [CMD-SHELL, pg_isready]
  web:
    # upstream: ghcr.io/example/web:v1.2.3
    image: registry.lazycat.cloud/example/web:abc123
    depends_on: [db]
{{ if .U.enable_worker }}
  worker:
    image: registry.lazycat.cloud/example/worker:abc123
    environment:
      - PRIVATE={{ .U.private_value }}
{{ end }}
`)
	writeProjectFile(t, root, "content/index.html", "demo")
	writeProjectFile(t, root, "icon.png", "png")
	writeProjectFile(t, root, "lzc-deploy-params.yml", "params: []\n")
	writeProjectFile(t, root, "resources/skills/demo/SKILL.md", "# Demo\n")

	got, err := project.Inspect(context.Background(), project.InspectRequest{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || got.Kind != project.KindService || got.Layout != lpk.LayoutV2 || got.ResourceOnly {
		t.Fatalf("inspection header = %#v", got)
	}
	if got.Package.Package != "community.lazycat.app.demo" || got.Package.Version != "1.2.3" || strings.Join(got.Package.LocaleCodes, ",") != "en,zh" {
		t.Fatalf("package = %#v", got.Package)
	}
	if !got.Template.Present || len(got.Services) != 3 || len(got.Images) != 3 || !got.Build.HasContent || len(got.Build.ConfiguredImageAliases) != 1 {
		t.Fatalf("inspection = %#v", got)
	}
	if got.Images[2].Service != "worker" || !got.Images[2].Conditional || got.Images[2].Editable {
		t.Fatalf("worker image = %#v", got.Images[2])
	}
	if got.Files.ContentDir != filepath.Join(root, "content") || got.Files.LPKPath != filepath.Join(root, "dist", "demo.lpk") {
		t.Fatalf("files = %#v", got.Files)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private_value", "PRIVATE=", "lzc-toolkit-template-", "{{"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("inspection JSON leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestInspectLocalLegacyExecProjectHonorsOverrides(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "lzc-build.yml", "manifest: lzc-manifest.yml\n")
	writeProjectFile(t, root, "lzc-manifest.yml", `package: community.lazycat.app.exec
version: 1.0.0
name: Exec
application:
  subdomain: exec
  upstreams:
    - location: /
      backend_launch_command: /lzcapp/pkg/content/server
      backend: file:///lzcapp/var/www/
`)

	got, err := project.Inspect(context.Background(), project.InspectRequest{
		Root: root, VersionOverride: "2.0.0", ForceV2: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != project.KindExec || got.Layout != lpk.LayoutV2 || got.Package.Version != "2.0.0" || !got.Application.HasExecLaunch {
		t.Fatalf("inspection = %#v", got)
	}
}

func TestInspectLocalResourceOnlyProject(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "lzc-build.yml", "resource_exports:\n  - kind: models\n    source: resources/models\n")
	writeProjectFile(t, root, "package.yml", "package: community.lazycat.app.models\nversion: 1.0.0\nname: Models\n")
	writeProjectFile(t, root, "resources/models/tiny/model.bin", "model")

	got, err := project.Inspect(context.Background(), project.InspectRequest{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if !got.ResourceOnly || got.Layout != lpk.LayoutV2 || got.Kind != project.KindStatic || len(got.Build.ResourceExports) != 1 {
		t.Fatalf("inspection = %#v", got)
	}
}

func TestInspectLocalAppliesPackageOverrides(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "lzc-build.yml", `package_override:
  description: Effective description
pkg_id: community.lazycat.app.overridden
pkg_name: Effective name
`)
	writeProjectFile(t, root, "package.yml", "package: community.lazycat.app.original\nversion: 1.0.0\nname: Original\n")
	writeProjectFile(t, root, "lzc-manifest.yml", "application:\n  subdomain: demo\n")

	got, err := project.Inspect(context.Background(), project.InspectRequest{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if got.Package.Package != "community.lazycat.app.overridden" || got.Package.Name != "Effective name" || got.Package.Description != "Effective description" {
		t.Fatalf("package = %#v", got.Package)
	}
}

func TestInspectLocalRejectsUnsafePathsBeforeReading(t *testing.T) {
	t.Run("config escape", func(t *testing.T) {
		root := t.TempDir()
		_, err := project.Inspect(context.Background(), project.InspectRequest{Root: root, ConfigFile: "../outside.yml"})
		assertProjectInspectCode(t, err, lpkgo.CodeInvalidConfig)
	})

	t.Run("manifest symlink", func(t *testing.T) {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "manifest.yml")
		if err := os.WriteFile(outside, []byte("application:\n  subdomain: leaked\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		writeProjectFile(t, root, "lzc-build.yml", "manifest: linked.yml\n")
		if err := os.Symlink(outside, filepath.Join(root, "linked.yml")); err != nil {
			t.Fatal(err)
		}
		_, err := project.Inspect(context.Background(), project.InspectRequest{Root: root})
		assertProjectInspectCode(t, err, lpkgo.CodeInvalidConfig)
	})
}

func TestInspectLocalRejectsTemplatedIdentityAndSubdomain(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "lzc-build.yml", "manifest: lzc-manifest.yml\n")
	writeProjectFile(t, root, "package.yml", "package: community.lazycat.app.demo\nversion: 1.0.0\n")
	writeProjectFile(t, root, "lzc-manifest.yml", "application:\n  subdomain: {{ .U.subdomain }}\n")
	_, err := project.Inspect(context.Background(), project.InspectRequest{Root: root})
	assertProjectInspectCode(t, err, lpkgo.CodeInvalidManifest)
}

func TestInspectLocalContextErrors(t *testing.T) {
	_, err := project.Inspect(nil, project.InspectRequest{})
	assertProjectInspectCode(t, err, lpkgo.CodeInvalidArgument)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = project.Inspect(ctx, project.InspectRequest{})
	assertProjectInspectCode(t, err, lpkgo.CodeCancelled)
}

func writeProjectFile(t *testing.T, root, name, contents string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertProjectInspectCode(t *testing.T, err error, code lpkgo.Code) {
	t.Helper()
	var structured *lpkgo.Error
	if !errors.As(err, &structured) || structured.Code != code {
		t.Fatalf("error = %#v, want %s", err, code)
	}
}
