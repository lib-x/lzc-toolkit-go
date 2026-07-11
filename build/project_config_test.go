package build

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
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

func TestResolveConfigCoercesStaticManifestScalars(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		manifest    string
		environment string
		want        string
	}{
		{name: "null", key: "package", manifest: "package: null\n", environment: "fallback", want: ""},
		{name: "bool", key: "version", manifest: "version: true\n", environment: "fallback", want: "true"},
		{name: "int", key: "name", manifest: "name: 42\n", environment: "fallback", want: "42"},
		{name: "plain nested unsigned integer", key: "subdomain", manifest: "application:\n  subdomain: 18446744073709551615\n", environment: "fallback", want: "fallback"},
		{name: "float", key: "description", manifest: "description: 1.25\n", environment: "fallback", want: "1.25"},
		{name: "quoted string", key: "license", manifest: "license: 'Apache-2.0'\n", environment: "fallback", want: "Apache-2.0"},
		{name: "templated", key: "package", manifest: "package: '{{ .Package }}'\n", environment: "fallback", want: "fallback"},
		{name: "conditional", key: "package", manifest: "{{ if .UsePackage }}\npackage: 42\n{{ end }}\n", environment: "fallback", want: "fallback"},
		{name: "static with unrelated template", key: "package", manifest: "package: 42\n{{ if .Worker }}\nservices: {}\n{{ end }}\n", environment: "fallback", want: "42"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, root, "lzc-build.yml", "contentdir: ${"+test.key+"}\n")
			writeTestFile(t, root, "lzc-manifest.yml", test.manifest)

			resolved, err := ResolveConfig(context.Background(), ConfigRequest{
				Root:        root,
				Environment: map[string]string{test.key: test.environment},
			})
			if err != nil {
				t.Fatal(err)
			}
			if resolved.Loaded.Config.ContentDir != test.want {
				t.Fatalf("ContentDir = %q, want %q", resolved.Loaded.Config.ContentDir, test.want)
			}
		})
	}
}

func TestResolveConfigPlainYAMLDoesNotAddNestedSubdomainSubstitution(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", "contentdir: ${subdomain}\n")
	writeTestFile(t, root, "lzc-manifest.yml", "application:\n  subdomain: manifest-value\n")

	resolved, err := ResolveConfig(context.Background(), ConfigRequest{
		Root:        root,
		Environment: map[string]string{"subdomain": "environment-value"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Loaded.Config.ContentDir != "environment-value" {
		t.Fatalf("ContentDir = %q", resolved.Loaded.Config.ContentDir)
	}
}

func TestResolveConfigTemplatedManifestAddsStaticSubdomainSubstitution(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", "contentdir: ${subdomain}\n")
	writeTestFile(t, root, "lzc-manifest.yml", "application:\n  subdomain: manifest-value\n{{ if .U.enabled }}\n  background_task: true\n{{ end }}\n")

	resolved, err := ResolveConfig(context.Background(), ConfigRequest{
		Root:        root,
		Environment: map[string]string{"subdomain": "environment-value"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Loaded.Config.ContentDir != "manifest-value" {
		t.Fatalf("ContentDir = %q", resolved.Loaded.Config.ContentDir)
	}
}

func TestResolveConfigRejectsDuplicatePlainManifestMapping(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", "contentdir: ${version}\n")
	manifestPath := filepath.Join(root, "lzc-manifest.yml")
	writeTestFile(t, root, "lzc-manifest.yml", "version: first\nversion: second\n")

	_, err := ResolveConfig(context.Background(), ConfigRequest{Root: root})
	var structured *lpkgo.Error
	if !errors.As(err, &structured) {
		t.Fatalf("error = %#v", err)
	}
	if structured.Code != lpkgo.CodeInvalidConfig || structured.Op != "build.template_manifest" || structured.Path != filepath.ToSlash(manifestPath) {
		t.Fatalf("error = %#v", structured)
	}
	if structured.Cause == nil || !strings.Contains(structured.Cause.Error(), "already defined") {
		t.Fatalf("cause = %v", structured.Cause)
	}
}

func TestResolveConfigPlainYAMLResolvesMergeKeyPackageSubstitution(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", "contentdir: ${package}\n")
	writeTestFile(t, root, "lzc-manifest.yml", `
defaults: &defaults
  package: community.lazycat.app.merged
<<: *defaults
version: 1.0.0
`)

	resolved, err := ResolveConfig(context.Background(), ConfigRequest{
		Root:        root,
		Environment: map[string]string{"package": "fallback"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Loaded.Config.ContentDir != "community.lazycat.app.merged" {
		t.Fatalf("ContentDir = %q", resolved.Loaded.Config.ContentDir)
	}
}

func TestResolveConfigAndBuildWrapMalformedPlainManifestAsInvalidConfig(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", "contentdir: content\n")
	manifestPath := filepath.Join(root, "lzc-manifest.yml")
	writeTestFile(t, root, "lzc-manifest.yml", "package: [unterminated\n")

	operations := []struct {
		name string
		run  func() error
	}{
		{name: "resolve config", run: func() error {
			_, err := ResolveConfig(context.Background(), ConfigRequest{Root: root})
			return err
		}},
		{name: "build", run: func() error {
			_, err := Build(context.Background(), &bytes.Buffer{}, Request{Root: root})
			return err
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			err := operation.run()
			var structured *lpkgo.Error
			if !errors.As(err, &structured) {
				t.Fatalf("error = %#v", err)
			}
			if structured.Code != lpkgo.CodeInvalidConfig || structured.Op != "build.template_manifest" || structured.Path != filepath.ToSlash(manifestPath) {
				t.Fatalf("error = %#v", structured)
			}
			if structured.Cause == nil || structured.Cause.Error() != "yaml: line 1: did not find expected ',' or ']'" {
				t.Fatalf("cause = %v", structured.Cause)
			}
			if err.Error() != string(lpkgo.CodeInvalidConfig) {
				t.Fatalf("error text = %q", err.Error())
			}
		})
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
