package build

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/lpk"
	"github.com/lib-x/lzc-toolkit-go/oci"
	"go.yaml.in/yaml/v3"
)

func TestBuildWritesLegacyProjectToCallerWriterWithoutRunningScriptByDefault(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", `
buildscript: echo must-not-run
manifest: lzc-manifest.yml
contentdir: content
icon: icon.png
`)
	writeTestFile(t, root, "lzc-manifest.yml", `
package: cloud.lazycat.apps.example
version: 1.2.3
name: Example
application:
  subdomain: example
  image: registry.lazycat.cloud/example:1.2.3
`)
	writeTestFile(t, root, "content/index.html", "hello")
	writeTestFile(t, root, "icon.png", "not-a-real-png")
	runner := &recordingRunner{}
	var output bytes.Buffer

	result, err := Build(context.Background(), &output, Request{Root: root, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d", runner.calls)
	}
	if result.Layout != lpk.LayoutV1 || result.Package != "cloud.lazycat.apps.example" || result.Version != "1.2.3" {
		t.Fatalf("unexpected result: %#v", result)
	}

	reader, err := lpk.Open(context.Background(), bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if reader.Layout() != lpk.LayoutV1 {
		t.Fatalf("layout = %q", reader.Layout())
	}
	assertLPKEntry(t, reader, "manifest.yml")
	assertLPKEntry(t, reader, "content.tar")
	assertNoLPKEntry(t, reader, "package.yml")
	assertNoLPKEntry(t, reader, "icon.png")
}

func TestBuildWritesV2MetadataAndCollectsOptionalFiles(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", `
manifest: manifest.yml
icon: icon.png
compose_override:
  services:
    app:
      environment:
        MODE: production
package_override:
  name: Overridden Name
`)
	writeTestFile(t, root, "manifest.yml", `
application:
  subdomain: example
  image: registry.lazycat.cloud/example:2.0.0
custom_field: preserved
`)
	writeTestFile(t, root, "package.yml", `
package: cloud.lazycat.apps.example
version: 2.0.0
name: Original Name
locales:
  zh:
    name: 示例
permissions:
  required:
    - lightos.manage
  optional:
    - user.notify
custom_package_field:
  preserved: true
`)
	writeTestFile(t, root, "lzc-deploy-params.yml", "params: {}\n")
	if err := os.WriteFile(filepath.Join(root, "icon.png"), append([]byte("\x89PNG\r\n\x1a\n"), []byte("fixture")...), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer

	result, err := Build(context.Background(), &output, Request{Root: root, Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Layout != lpk.LayoutV2 || result.Package != "cloud.lazycat.apps.example" || result.Version != "2.0.0" {
		t.Fatalf("unexpected result: %#v", result)
	}

	reader, err := lpk.Open(context.Background(), bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, name := range []string{"manifest.yml", "package.yml", "icon.png", "deploy_params.yml", "compose.override.yml"} {
		assertLPKEntry(t, reader, name)
	}
	packageEntry, err := reader.OpenEntry(context.Background(), "package.yml")
	if err != nil {
		t.Fatal(err)
	}
	packageData, readErr := io.ReadAll(packageEntry)
	closeErr := packageEntry.Close()
	if readErr != nil || closeErr != nil {
		t.Fatal(errors.Join(readErr, closeErr))
	}
	var packageDocument map[string]any
	if err := yaml.Unmarshal(packageData, &packageDocument); err != nil {
		t.Fatal(err)
	}
	wantPermissions := map[string]any{
		"required": []any{"lightos.manage"},
		"optional": []any{"user.notify"},
	}
	if !reflect.DeepEqual(packageDocument["permissions"], wantPermissions) {
		t.Fatalf("permissions = %#v, want %#v", packageDocument["permissions"], wantPermissions)
	}
	if !reflect.DeepEqual(packageDocument["custom_package_field"], map[string]any{"preserved": true}) {
		t.Fatalf("custom_package_field = %#v", packageDocument["custom_package_field"])
	}
	effective, err := reader.EffectiveManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if effective.Manifest.Name != "Overridden Name" {
		t.Fatalf("name = %q", effective.Manifest.Name)
	}
	if value, found, err := effective.Source.Lookup("custom_field"); err != nil || !found || value != "preserved" {
		t.Fatalf("custom_field = %#v, found=%v, err=%v", value, found, err)
	}
}

func TestBuildWritesResourceOnlyPackage(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", `
resource_exports:
  - kind: models
    source: resources/models
`)
	writeTestFile(t, root, "package.yml", `
package: cloud.lazycat.resources.models
version: 1.0.0
name: Models
`)
	writeTestFile(t, root, "resources/models/tiny/model.bin", "model")
	var output bytes.Buffer

	result, err := Build(context.Background(), &output, Request{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if result.Layout != lpk.LayoutV2 {
		t.Fatalf("layout = %q", result.Layout)
	}
	reader, err := lpk.Open(context.Background(), bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	assertLPKEntry(t, reader, "package.yml")
	assertLPKEntry(t, reader, "exports/models/tiny/model.bin")
	assertNoLPKEntry(t, reader, "manifest.yml")
}

func TestBuildRunsScriptOnlyWhenExplicitlyEnabled(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", "buildscript: make release\nenvs:\n  - CHANNEL=stable\n")
	writeTestFile(t, root, "lzc-manifest.yml", `
package: cloud.lazycat.apps.example
version: 1.0.0
application:
  subdomain: example
`)
	runner := &recordingRunner{}
	var output bytes.Buffer

	_, err := Build(context.Background(), &output, Request{
		Root:           root,
		RunBuildScript: true,
		Runner:         runner,
		Environment:    map[string]string{"BASE": "present"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 || runner.command.Script != "make release" || runner.command.Dir != root {
		t.Fatalf("command = %#v, calls=%d", runner.command, runner.calls)
	}
	if !reflect.DeepEqual(runner.command.Env, map[string]string{"BASE": "present", "CHANNEL": "stable"}) {
		t.Fatalf("environment = %#v", runner.command.Env)
	}
}

func TestBuildConfigTemplatesCanUseManifestAndBuildEnvValues(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", `
manifest: lzc-manifest.yml
contentdir: ${CONTENT_ROOT}
envs:
  - CONTENT_ROOT=${package}
  - APP_VERSION=${version}
buildscript: verify
`)
	writeTestFile(t, root, "lzc-manifest.yml", `
package: community.lazycat.app.templated-app
version: 3.2.1
application:
  subdomain: templated
`)
	writeTestFile(t, root, "community.lazycat.app.templated-app/data.txt", "payload")
	runner := &recordingRunner{}
	var output bytes.Buffer

	_, err := Build(context.Background(), &output, Request{
		Root:           root,
		RunBuildScript: true,
		Runner:         runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if runner.command.Env["CONTENT_ROOT"] != "community.lazycat.app.templated-app" || runner.command.Env["APP_VERSION"] != "3.2.1" {
		t.Fatalf("build environment = %#v", runner.command.Env)
	}
	reader, err := lpk.Open(context.Background(), bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	assertLPKEntry(t, reader, "content.tar")
}

func TestBuildPreservesGoTemplateManifestAcceptedByLzcCLI(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", "manifest: lzc-manifest.yml\n")
	writeTestFile(t, root, "package.yml", `
package: cloud.lazycat.apps.templated
version: 1.0.0
name: Templated
`)
	writeTestFile(t, root, "lzc-manifest.yml", `
application:
  subdomain: templated
{{if .U.multi_instance}}
  multi_instance: true
{{end}}
services:
  app:
    image: registry.lazycat.cloud/example/app:1.0.0
    environment:
      - PASSWORD={{.U.password}}
`)
	var output bytes.Buffer

	_, err := Build(context.Background(), &output, Request{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := lpk.Open(context.Background(), bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	entry, err := reader.OpenEntry(context.Background(), "manifest.yml")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(entry)
	closeErr := entry.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("read=%v close=%v", err, closeErr)
	}
	if !bytes.Contains(data, []byte("{{if .U.multi_instance}}")) || !bytes.Contains(data, []byte("{{.U.password}}")) {
		t.Fatalf("template was not preserved:\n%s", data)
	}
}

func TestBuildPreservesDocumentedLazyCatTemplateForms(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", "manifest: lzc-manifest.yml\n")
	writeTestFile(t, root, "package.yml", "package: cloud.lazycat.apps.template-forms\nversion: 1.0.0\nname: Template Forms\n")
	writeTestFile(t, root, "lzc-manifest.yml", `{{ if .U.target }}
usage: "to {{ .U.target }}"
{{ else }}
usage: "fallback"
{{ end }}
application:
  subdomain: template-forms
  ingress:
    - protocol: tcp
      port: {{ index .U "listen.port" }}
services:
  app:
    image: registry.lazycat.cloud/example/app:1.0.0
    environment:
      - URL={{if .U.base_url}}{{.U.base_url}}{{else}}https://{{.S.AppDomain}}{{end}}
`)
	var output bytes.Buffer
	if _, err := Build(context.Background(), &output, Request{Root: root}); err != nil {
		t.Fatal(err)
	}
	reader, err := lpk.Open(context.Background(), bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	entry, err := reader.OpenEntry(context.Background(), "manifest.yml")
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(entry)
	closeErr := entry.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read=%v close=%v", readErr, closeErr)
	}
	for _, action := range [][]byte{
		[]byte(`{{ if .U.target }}`),
		[]byte(`{{ index .U "listen.port" }}`),
		[]byte(`{{if .U.base_url}}`),
		[]byte(`{{.S.AppDomain}}`),
	} {
		if !bytes.Contains(data, action) {
			t.Fatalf("manifest is missing %q:\n%s", action, data)
		}
	}
}

func TestBuildRejectsConditionalEmbeddedImageBeforeBuilder(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", "manifest: lzc-manifest.yml\nimages:\n  worker:\n    context: .\n")
	writeTestFile(t, root, "package.yml", "package: cloud.lazycat.apps.conditional-image\nversion: 1.0.0\n")
	writeTestFile(t, root, "lzc-manifest.yml", `application:
  subdomain: conditional-image
services:
{{ if .U.worker }}
  worker:
    image: embed:worker
{{ end }}
`)
	builder := &staticImageBuilder{artifact: newTestImageArtifact(t)}
	_, err := Build(context.Background(), &bytes.Buffer{}, Request{Root: root, ImageBuilder: builder})
	if !errors.Is(err, lpkgo.ErrInvalidConfig) || builder.calls != 0 {
		t.Fatalf("error=%v builder calls=%d", err, builder.calls)
	}
}

func TestBuildTemplatedManifestPassesStaticEmbeddedImageToBuilder(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", "manifest: lzc-manifest.yml\nimages:\n  app:\n    context: .\n")
	writeTestFile(t, root, "package.yml", "package: cloud.lazycat.apps.templated-image\nversion: 1.0.0\n")
	writeTestFile(t, root, "lzc-manifest.yml", `application:
  subdomain: templated-image
services:
  app:
    image: embed:app
    environment:
{{ if .U.debug }}
      - DEBUG=true
{{ end }}
`)
	artifact := newTestImageArtifact(t)
	builder := &staticImageBuilder{artifact: artifact}
	if _, err := Build(context.Background(), &bytes.Buffer{}, Request{Root: root, ImageBuilder: builder}); err != nil {
		t.Fatal(err)
	}
	if builder.calls != 1 || builder.request.Manifest.Services["app"].Image != "embed:app" {
		t.Fatalf("builder calls=%d manifest=%#v", builder.calls, builder.request.Manifest)
	}
}

func TestBuildFileAtomicallyWritesPackage(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", "manifest: lzc-manifest.yml\n")
	writeTestFile(t, root, "lzc-manifest.yml", `
package: cloud.lazycat.apps.file
version: 1.0.0
application:
  subdomain: file
`)
	filename := filepath.Join(t.TempDir(), "file.lpk")

	result, err := BuildFile(context.Background(), filename, Request{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if result.Write.Size == 0 {
		t.Fatal("empty write result")
	}
	reader, err := lpk.OpenFile(context.Background(), filename)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if reader.Layout() != lpk.LayoutV1 {
		t.Fatalf("layout = %q", reader.Layout())
	}
}

func TestBuildUsesInjectedImageBuilderAndEmbedsValidatedOCIArtifact(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", `
manifest: lzc-manifest.yml
contentdir: content
images:
  app:
    context: .
`)
	writeTestFile(t, root, "lzc-manifest.yml", `
application:
  subdomain: image-app
  image: embed:app
`)
	writeTestFile(t, root, "package.yml", `
package: cloud.lazycat.apps.image-app
version: 1.0.0
name: Image App
`)
	writeTestFile(t, root, "content/data.txt", "content")
	artifact := newTestImageArtifact(t)
	builder := &staticImageBuilder{artifact: artifact}
	var output bytes.Buffer

	result, err := Build(context.Background(), &output, Request{Root: root, ImageBuilder: builder})
	if err != nil {
		t.Fatal(err)
	}
	if builder.calls != 1 || builder.request.Manifest.Application.Image != "embed:app" {
		t.Fatalf("builder calls=%d request=%#v", builder.calls, builder.request)
	}
	if !artifact.closed {
		t.Fatal("image artifact was not closed")
	}
	if result.ImageCount != 1 || result.ResolvedImages["app"] == "" {
		t.Fatalf("result = %#v", result)
	}
	reader, err := lpk.Open(context.Background(), bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, name := range []string{"images.lock", "images/oci-layout", "images/index.json", "content.tar.gz"} {
		assertLPKEntry(t, reader, name)
	}
	assertNoLPKEntry(t, reader, "content.tar")
	manifestEntry, err := reader.OpenEntry(context.Background(), "manifest.yml")
	if err != nil {
		t.Fatal(err)
	}
	manifestData, err := io.ReadAll(manifestEntry)
	closeErr := manifestEntry.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("read=%v close=%v", err, closeErr)
	}
	wantImage := "embed:app@" + result.ResolvedImages["app"]
	if !bytes.Contains(manifestData, []byte(wantImage)) {
		t.Fatalf("manifest does not contain %q:\n%s", wantImage, manifestData)
	}
}

func TestRewriteEmbeddedImagesDoesNotMatchLongerAliasPrefix(t *testing.T) {
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	got, err := rewriteEmbeddedImages([]byte("first: embed:app-helper\nsecond: embed:app\n"), map[string]string{"app": digest})
	if err != nil {
		t.Fatal(err)
	}
	want := "first: embed:app-helper\nsecond: embed:app@" + digest + "\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildEmptyImagesConfigSelectsV2WithoutRequiringAdapter(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lzc-build.yml", "images: {}\n")
	writeTestFile(t, root, "lzc-manifest.yml", `
package: cloud.lazycat.apps.empty-images
version: 1.0.0
application:
  subdomain: empty-images
`)
	var output bytes.Buffer

	result, err := Build(context.Background(), &output, Request{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if result.Layout != lpk.LayoutV2 || result.ImageCount != 0 {
		t.Fatalf("result = %#v", result)
	}
}

type recordingRunner struct {
	calls   int
	command Command
}

func (r *recordingRunner) Run(_ context.Context, command Command) error {
	r.calls++
	r.command = command
	return nil
}

func assertLPKEntry(t *testing.T, reader *lpk.Reader, name string) {
	t.Helper()
	entry, err := reader.OpenEntry(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	defer entry.Close()
	if _, err := io.Copy(io.Discard, entry); err != nil {
		t.Fatal(err)
	}
}

func assertNoLPKEntry(t *testing.T, reader *lpk.Reader, name string) {
	t.Helper()
	if _, err := reader.Entry(context.Background(), name); err == nil {
		t.Fatalf("unexpected LPK entry %q", name)
	}
}

type staticImageBuilder struct {
	calls    int
	request  ImageBuildRequest
	artifact *testImageArtifact
}

func (b *staticImageBuilder) Build(_ context.Context, request ImageBuildRequest) (ImageArtifact, error) {
	b.calls++
	b.request = request
	return b.artifact, nil
}

type testImageArtifact struct {
	files  fs.FS
	closed bool
}

func (a *testImageArtifact) FS() fs.FS { return a.files }

func (a *testImageArtifact) Close() error {
	a.closed = true
	return nil
}

func newTestImageArtifact(t *testing.T) *testImageArtifact {
	t.Helper()
	config := []byte(`{"architecture":"amd64","os":"linux"}`)
	layer := []byte("embedded-image-layer")
	configDigest := testDigest(t, config)
	layerDigest := testDigest(t, layer)
	imageManifest := oci.Manifest{
		SchemaVersion: 2,
		MediaType:     oci.MediaTypeImageManifest,
		Config:        oci.Descriptor{MediaType: oci.MediaTypeImageConfig, Digest: configDigest, Size: int64(len(config))},
		Layers:        []oci.Descriptor{{MediaType: oci.MediaTypeImageLayerGzip, Digest: layerDigest, Size: int64(len(layer))}},
	}
	manifestData, err := json.Marshal(imageManifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest := testDigest(t, manifestData)
	indexData, err := json.Marshal(oci.Index{SchemaVersion: 2, Manifests: []oci.Descriptor{{
		MediaType: oci.MediaTypeImageManifest,
		Digest:    manifestDigest,
		Size:      int64(len(manifestData)),
		Annotations: map[string]string{
			oci.AnnotationRefName: "app",
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	lockData, err := yaml.Marshal(oci.Lock{Version: 1, Images: map[string]oci.LockImage{
		"app": {ImageID: configDigest, Layers: []oci.LockLayer{{Digest: layerDigest, Source: oci.LayerSourceEmbed}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return &testImageArtifact{files: fstest.MapFS{
		"images.lock":       &fstest.MapFile{Data: lockData},
		"images/oci-layout": &fstest.MapFile{Data: []byte(`{"imageLayoutVersion":"1.0.0"}`)},
		"images/index.json": &fstest.MapFile{Data: indexData},
		"images/blobs/sha256/" + configDigest.Hex():   &fstest.MapFile{Data: config},
		"images/blobs/sha256/" + layerDigest.Hex():    &fstest.MapFile{Data: layer},
		"images/blobs/sha256/" + manifestDigest.Hex(): &fstest.MapFile{Data: manifestData},
	}}
}

func testDigest(t *testing.T, data []byte) oci.Digest {
	t.Helper()
	sum := sha256.Sum256(data)
	digest, err := oci.ParseDigest("sha256:" + hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	return digest
}
