package lint_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/lint"
	"github.com/lib-x/lzc-toolkit-go/manifest"
)

func TestManifestOfficialWarningsAreOptional(t *testing.T) {
	t.Parallel()

	document, err := manifest.Parse([]byte(`application:
  subdomain: demo
services:
  web:
    image: docker.io/library/nginx:latest
`))
	if err != nil {
		t.Fatalf("manifest.Parse() error = %v", err)
	}
	typed := manifest.Manifest{
		PackageInfo: manifest.PackageInfo{
			Version: "not-semver",
		},
	}

	defaultWarnings, err := lint.Manifest(document, typed)
	if err != nil {
		t.Fatalf("lint.Manifest() error = %v", err)
	}
	if len(defaultWarnings) != 0 {
		t.Fatalf("default lint warnings = %#v; want no official warnings by default", defaultWarnings)
	}

	officialWarnings, err := lint.Manifest(document, typed, lint.WithOfficial())
	if err != nil {
		t.Fatalf("lint.Manifest(WithOfficial) error = %v", err)
	}
	got := warningCodePaths(officialWarnings)
	want := [][2]string{
		{"store-version-invalid-semver", "version"},
		{"store-locales-required", "locales"},
		{"store-image-registry-invalid", "services.web.image"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("official warning code/path = %#v; want %#v", got, want)
	}
	for _, warning := range officialWarnings {
		if !lint.IsOfficialWarning(warning) {
			t.Fatalf("IsOfficialWarning(%#v) = false; want true", warning)
		}
	}
}

func TestPackageOfficialChecksIconAndImageRegistryOnlyWhenEnabled(t *testing.T) {
	t.Parallel()

	root := fstest.MapFS{
		"package.yml":  {Data: []byte("package: cloud.lazycat.demo\nversion: 1.0.0\nname: Demo\n")},
		"manifest.yml": {Data: []byte("application:\n  subdomain: demo\nservices:\n  web:\n    image: docker.io/library/nginx:latest\n")},
	}

	defaultWarnings, err := lint.Package(context.Background(), root)
	if err != nil {
		t.Fatalf("lint.Package() error = %v", err)
	}
	if len(defaultWarnings) != 0 {
		t.Fatalf("default package warnings = %#v; want none", defaultWarnings)
	}

	officialWarnings, err := lint.Package(context.Background(), root, lint.WithOfficial())
	if err != nil {
		t.Fatalf("lint.Package(WithOfficial) error = %v", err)
	}
	got := warningCodePaths(officialWarnings)
	want := [][2]string{
		{"lpk-icon-invalid", "icon.png"},
		{"store-locales-required", "locales"},
		{"store-image-registry-invalid", "services.web.image"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("official package warnings = %#v; want %#v", got, want)
	}
}

func TestPackageOfficialAcceptsNonOfficialRegistryWhenDisabled(t *testing.T) {
	t.Parallel()

	root := fstest.MapFS{
		"package.yml":  {Data: []byte("package: cloud.lazycat.demo\nversion: 1.0.0\nname: Demo\n")},
		"manifest.yml": {Data: []byte("application:\n  subdomain: demo\nservices:\n  web:\n    image: docker.io/library/nginx:latest\n")},
		"icon.png":     {Data: minimalPNG()},
	}

	warnings, err := lint.Package(context.Background(), root)
	if err != nil {
		t.Fatalf("lint.Package() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("lint.Package() warnings = %#v; want no warnings without WithOfficial", warnings)
	}
}

func TestPackageOfficialReportsLargePNGIcon(t *testing.T) {
	t.Parallel()

	root := fstest.MapFS{
		"package.yml":  {Data: []byte("package: cloud.lazycat.demo\nversion: 1.0.0\nname: Demo\nlocales:\n  zh:\n    name: Demo\n")},
		"manifest.yml": {Data: []byte("application:\n  subdomain: demo\nservices:\n  web:\n    image: registry.lazycat.cloud/demo/web:1.0.0\n")},
		"icon.png":     {Data: append(minimalPNG(), make([]byte, 200*1024)...)},
	}

	warnings, err := lint.Package(context.Background(), root, lint.WithOfficial())
	if err != nil {
		t.Fatalf("lint.Package(WithOfficial) error = %v", err)
	}
	got := warningCodePaths(warnings)
	want := [][2]string{{"store-icon-too-large", "icon.png"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("official package warnings = %#v; want %#v", got, want)
	}
}

func TestManifestOfficialChecksIconPathSize(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	iconPath := filepath.Join(directory, "icon.png")
	if err := os.WriteFile(iconPath, append(minimalPNG(), make([]byte, 200*1024)...), 0o600); err != nil {
		t.Fatalf("WriteFile(icon) error = %v", err)
	}
	document, err := manifest.Parse([]byte(`application:
  subdomain: demo
services:
  web:
    image: registry.lazycat.cloud/demo/web:1.0.0
`))
	if err != nil {
		t.Fatalf("manifest.Parse() error = %v", err)
	}
	typed := manifest.Manifest{
		PackageInfo: manifest.PackageInfo{
			Version: "1.0.0",
			Locales: map[string]any{"zh": map[string]any{"name": "Demo"}},
		},
	}

	warnings, err := lint.Manifest(document, typed, lint.WithOfficial(), lint.WithIconPath(iconPath))
	if err != nil {
		t.Fatalf("lint.Manifest(WithOfficial, WithIconPath) error = %v", err)
	}
	got := warningCodePaths(warnings)
	want := [][2]string{{"store-icon-too-large", "icon.png"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("official warning code/path = %#v; want %#v", got, want)
	}
}

func TestOfficialEmbeddedImageWarningsFollowUpstreamRules(t *testing.T) {
	t.Parallel()

	document, err := manifest.Parse([]byte(`application:
  subdomain: demo
services:
  web:
    image: embed:web
  api:
    image: embed:api@sha256:1111
  worker:
    image: embed:worker
  local:
    image: embed:local
`))
	if err != nil {
		t.Fatalf("manifest.Parse() error = %v", err)
	}
	typed := manifest.Manifest{
		PackageInfo: manifest.PackageInfo{
			Version: "1.0.0",
			Locales: map[string]any{"zh": map[string]any{"name": "Demo"}},
		},
	}

	warnings, err := lint.Manifest(document, typed,
		lint.WithOfficial(),
		lint.WithEmbeddedImages(map[string]lint.EmbeddedImage{
			"api": {
				Upstream: "docker.io/example/api:1.0.0",
			},
			"worker": {
				Layers: []lint.EmbeddedLayer{
					{Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Source: "embed", BlobExists: false},
				},
			},
			"local": {
				Layers: []lint.EmbeddedLayer{
					{Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Source: "embed", BlobExists: true},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("lint.Manifest(WithOfficial, WithEmbeddedImages) error = %v", err)
	}
	got := warningCodePaths(warnings)
	want := [][2]string{
		{"store-image-embed-upstream-invalid", "services.api.image"},
		{"store-image-embed-alias-missing", "services.web.image"},
		{"store-image-embed-blob-missing", "services.worker.image"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("embedded image warnings = %#v; want %#v", got, want)
	}
}

func TestOfficialEmbeddedImageBuildOptions(t *testing.T) {
	t.Parallel()

	document, err := manifest.Parse([]byte(`application:
  subdomain: demo
services:
  web:
    image: embed:web
  api:
    image: embed:api
`))
	if err != nil {
		t.Fatalf("manifest.Parse() error = %v", err)
	}
	typed := manifest.Manifest{
		PackageInfo: manifest.PackageInfo{
			Version: "1.0.0",
			Locales: map[string]any{"zh": map[string]any{"name": "Demo"}},
		},
	}

	warnings, err := lint.Manifest(document, typed,
		lint.WithOfficial(),
		lint.WithImageBuilds(map[string]lint.ImageBuild{
			"api": {UpstreamMatch: "docker.io/example"},
			"web": {},
		}),
	)
	if err != nil {
		t.Fatalf("lint.Manifest(WithOfficial, WithImageBuilds) error = %v", err)
	}
	got := warningCodePaths(warnings)
	want := [][2]string{{"store-image-embed-upstream-invalid", "services.api.image"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("embedded image build warnings = %#v; want %#v", got, want)
	}
}

func TestPackageOfficialResourceOnlyMatchesLZCCLIBehavior(t *testing.T) {
	t.Parallel()

	root := fstest.MapFS{
		"package.yml":                   {Data: []byte("package: cloud.lazycat.resources\nversion: 1.0.0\n")},
		"exports/config/default/data":   {Data: []byte("payload")},
		"services/not-a-store-manifest": {Data: []byte("ignored")},
	}

	warnings, err := lint.Package(context.Background(), root, lint.WithOfficial())
	if err != nil {
		t.Fatalf("lint.Package(resource-only, WithOfficial) error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("resource-only official warnings = %#v; want none", warnings)
	}
}

func TestPackageOfficialWarnsAboutDevshell(t *testing.T) {
	t.Parallel()

	root := fstest.MapFS{
		"package.yml":  {Data: []byte("package: cloud.lazycat.demo\nversion: 1.0.0\nname: Demo\nlocales:\n  zh:\n    name: Demo\n")},
		"manifest.yml": {Data: []byte("application:\n  subdomain: demo\n")},
		"icon.png":     {Data: minimalPNG()},
		"devshell":     {Data: []byte("devshell marker")},
	}

	warnings, err := lint.Package(context.Background(), root, lint.WithOfficial())
	if err != nil {
		t.Fatalf("lint.Package(WithOfficial) error = %v", err)
	}
	got := warningCodePaths(warnings)
	want := [][2]string{{"lpk-devshell-disallowed", "devshell"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("official package warnings = %#v; want %#v", got, want)
	}
}

func TestOfficialWarningClassifier(t *testing.T) {
	t.Parallel()

	if !lint.IsOfficialWarning(lpkgo.Warning{Code: "store-locales-required"}) {
		t.Fatal("store-* warning should be official")
	}
	if !lint.IsOfficialWarning(lpkgo.Warning{Code: "lpk-icon-invalid"}) {
		t.Fatal("lpk-icon-invalid warning should be official")
	}
	if lint.IsOfficialWarning(lpkgo.Warning{Code: "unknown-manifest-fields"}) {
		t.Fatal("unknown-manifest-fields should not be official-only")
	}
}

func minimalPNG() []byte {
	return []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
}
