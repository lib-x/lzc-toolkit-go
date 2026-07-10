package build

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/lib-x/lpk-go/lpk"
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
