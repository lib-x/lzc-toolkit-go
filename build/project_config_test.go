package build

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/lpk"
)

func TestResolveConfigExpandsManifestPackageValues(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", `
contentdir: ${package}
envs:
  - APP_VERSION=${version}
`)
	writeTestFile(t, root, "lzc-manifest.yml", `
package: community.lazycat.app.demo
version: 1.2.3
application:
  subdomain: demo
`)

	resolved, err := ResolveConfig(context.Background(), ConfigRequest{
		Root:        root,
		ConfigFile:  "lzc-build.yml",
		Environment: map[string]string{"CHANNEL": "stable"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Root != root || resolved.Loaded.Config.ContentDir != "community.lazycat.app.demo" {
		t.Fatalf("resolved = %#v", resolved)
	}
	if resolved.Loaded.BuildEnv["APP_VERSION"] != "1.2.3" {
		t.Fatalf("BuildEnv = %#v", resolved.Loaded.BuildEnv)
	}
}

func TestResolveConfigExpandsStaticValuesFromTemplatedManifest(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", `
contentdir: ${package}
envs:
  - APP_VERSION=${version}
`)
	writeTestFile(t, root, "lzc-manifest.yml", `
package: community.lazycat.app.templated
version: 4.5.6
{{ if .Features.Worker }}
services:
  worker:
    image: registry.lazycat.cloud/worker:latest
{{ end }}
application:
  subdomain: templated
`)

	resolved, err := ResolveConfig(context.Background(), ConfigRequest{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Loaded.Config.ContentDir != "community.lazycat.app.templated" || resolved.Loaded.BuildEnv["APP_VERSION"] != "4.5.6" {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestResolveConfigPreservesStaticEmptyManifestPackageValue(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", "contentdir: ${package}\n")
	writeTestFile(t, root, "lzc-manifest.yml", "package: ''\nversion: 1.0.0\n")

	resolved, err := ResolveConfig(context.Background(), ConfigRequest{
		Root:        root,
		Environment: map[string]string{"package": "inherited-value"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Loaded.Config.ContentDir != "" {
		t.Fatalf("ContentDir = %q", resolved.Loaded.Config.ContentDir)
	}
}

func TestResolveConfigDevelopmentInheritsReleaseConfiguration(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", "manifest: app.yml\ncontentdir: release\n")
	writeTestFile(t, root, "lzc-build.dev.yml", "contentdir: development\n")
	writeTestFile(t, root, "app.yml", "package: community.lazycat.app.demo\nversion: 1.0.0\n")

	resolved, err := ResolveConfig(context.Background(), ConfigRequest{Root: root, ConfigFile: "lzc-build.dev.yml"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Loaded.Profile != ProfileDevelopment || resolved.Loaded.Config.Manifest != "app.yml" || resolved.Loaded.Config.ContentDir != "development" {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestResolveConfigExplicitEnvironmentOverridesInheritedProcessValues(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LPK_GO_CONFIG_CHANNEL", "process")
	writeTestFile(t, root, "lzc-build.yml", "contentdir: ${LPK_GO_CONFIG_CHANNEL}\n")
	writeTestFile(t, root, "lzc-manifest.yml", "package: community.lazycat.app.demo\nversion: 1.0.0\n")

	resolved, err := ResolveConfig(context.Background(), ConfigRequest{
		Root:               root,
		InheritEnvironment: true,
		Environment:        map[string]string{"LPK_GO_CONFIG_CHANNEL": "explicit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Loaded.Config.ContentDir != "explicit" {
		t.Fatalf("ContentDir = %q", resolved.Loaded.Config.ContentDir)
	}
}

func TestResolveConfigReturnsDefensiveMapCopies(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", `
envs:
  - CHANNEL=${CHANNEL}
compose_override:
  services:
    app:
      environment:
        CHANNEL: stable
package_override:
  name: Demo
remote:
  host: example.invalid
`)
	writeTestFile(t, root, "lzc-manifest.yml", "package: community.lazycat.app.demo\nversion: 1.0.0\n")
	environment := map[string]string{"CHANNEL": "stable"}

	first, err := ResolveConfig(context.Background(), ConfigRequest{Root: root, Environment: environment})
	if err != nil {
		t.Fatal(err)
	}
	environment["CHANNEL"] = "mutated"
	if first.Loaded.BuildEnv["CHANNEL"] != "stable" {
		t.Fatalf("BuildEnv aliases request environment: %#v", first.Loaded.BuildEnv)
	}
	rawCompose := first.Loaded.Raw["compose_override"].(map[string]any)
	rawCompose["services"] = "raw-mutated"
	if first.Loaded.Config.ComposeOverride["services"] == "raw-mutated" {
		t.Fatal("Raw aliases Config.ComposeOverride")
	}
	first.Loaded.BuildEnv["CHANNEL"] = "mutated"
	first.Loaded.Raw["envs"] = []any{"CHANNEL=mutated"}
	first.Loaded.Config.ComposeOverride["services"] = "mutated"
	first.Loaded.Config.PackageOverride["name"] = "Mutated"
	first.Loaded.Config.Remote["host"] = "mutated.invalid"

	second, err := ResolveConfig(context.Background(), ConfigRequest{Root: root, Environment: map[string]string{"CHANNEL": "stable"}})
	if err != nil {
		t.Fatal(err)
	}
	if second.Loaded.BuildEnv["CHANNEL"] != "stable" || second.Loaded.Config.PackageOverride["name"] != "Demo" || second.Loaded.Config.Remote["host"] != "example.invalid" {
		t.Fatalf("second = %#v", second)
	}
	if !reflect.DeepEqual(second.Loaded.Config.ComposeOverride, map[string]any{
		"services": map[string]any{"app": map[string]any{"environment": map[string]any{"CHANNEL": "stable"}}},
	}) {
		t.Fatalf("ComposeOverride = %#v", second.Loaded.Config.ComposeOverride)
	}
}

func TestResolveConfigContextErrors(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name string
		ctx  context.Context
		code lpkgo.Code
	}{
		{name: "nil", ctx: nil, code: lpkgo.CodeInvalidArgument},
		{name: "cancelled", ctx: cancelledContext(), code: lpkgo.CodeCancelled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ResolveConfig(test.ctx, ConfigRequest{Root: root})
			var structured *lpkgo.Error
			if !errors.As(err, &structured) || structured.Code != test.code {
				t.Fatalf("error = %#v", err)
			}
		})
	}
}

func TestPredictLayoutMatchesBuildRules(t *testing.T) {
	stringValue := "value"
	tests := []struct {
		name          string
		forceV2       bool
		loaded        LoadedConfig
		packageExists bool
		resourceOnly  bool
		want          lpk.Layout
	}{
		{name: "legacy", want: lpk.LayoutV1},
		{name: "forced", forceV2: true, want: lpk.LayoutV2},
		{name: "package file", packageExists: true, want: lpk.LayoutV2},
		{name: "images", loaded: LoadedConfig{Config: Config{Images: map[string]any{}}}, want: lpk.LayoutV2},
		{name: "envs", loaded: LoadedConfig{Config: Config{Envs: []string{"MODE=release"}}}, want: lpk.LayoutV2},
		{name: "package overrides", loaded: LoadedConfig{Config: Config{PackageOverride: map[string]any{"name": "Demo"}}}, want: lpk.LayoutV2},
		{name: "package id", loaded: LoadedConfig{Config: Config{PackageID: &stringValue}}, want: lpk.LayoutV2},
		{name: "package name", loaded: LoadedConfig{Config: Config{PackageName: &stringValue}}, want: lpk.LayoutV2},
		{name: "resource only", resourceOnly: true, want: lpk.LayoutV2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := PredictLayout(test.forceV2, test.loaded, test.packageExists, test.resourceOnly); got != test.want {
				t.Fatalf("PredictLayout() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveConfigCleansAbsoluteRoot(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", "contentdir: content\n")
	writeTestFile(t, root, "lzc-manifest.yml", "package: community.lazycat.app.demo\nversion: 1.0.0\n")

	resolved, err := ResolveConfig(context.Background(), ConfigRequest{Root: filepath.Join(root, ".")})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Root != filepath.Clean(root) || !filepath.IsAbs(resolved.Root) {
		t.Fatalf("Root = %q", resolved.Root)
	}
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
