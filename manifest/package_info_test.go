package manifest_test

import (
	"bytes"
	"errors"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/manifest"
)

func TestManifestDecodesTypedContract(t *testing.T) {
	t.Parallel()

	document, err := manifest.Parse([]byte(`package: cloud.lazycat.example
version: 1.2.3
usage: Open the application.
ext_config:
  enable_document_access: true
application:
  image: registry.example/app:1.2.3
  background_task: true
  subdomain: example
  gpu_accel: true
  environment:
    - MODE=production
  health_check:
    test_url: http://127.0.0.1:8080/healthz
services:
  worker:
    image: registry.example/worker:1.2.3
    command: [worker, run]
    cpu_shares: 512
    mem_limit: 256m
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	var got manifest.Manifest
	if err := document.Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Package != "cloud.lazycat.example" || got.Version != "1.2.3" {
		t.Fatalf("static package fields = %q, %q", got.Package, got.Version)
	}
	if got.Usage != "Open the application." {
		t.Fatalf("Usage = %q", got.Usage)
	}
	if !got.ExtConfig.EnableDocumentAccess {
		t.Fatal("ExtConfig.EnableDocumentAccess = false; want true")
	}
	if got.Application.Image != "registry.example/app:1.2.3" || !got.Application.BackgroundTask || !got.Application.GPUAccel {
		t.Fatalf("Application = %+v", got.Application)
	}
	worker := got.Services["worker"]
	if worker.Image != "registry.example/worker:1.2.3" || worker.CPUShares != 512 || worker.MemLimit != "256m" {
		t.Fatalf("worker Service = %+v", worker)
	}
}

func TestLoadEffectiveLegacyManifest(t *testing.T) {
	t.Parallel()

	source, err := manifest.Parse([]byte(`# legacy manifest
package: cloud.lazycat.legacy
version: 1.0.0
name: Legacy
application:
  subdomain: legacy
future_field:
  enabled: true
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	effective, err := manifest.LoadEffective(source, nil, true)
	if err != nil {
		t.Fatalf("LoadEffective() error = %v", err)
	}
	if effective.HasPackageFile {
		t.Fatal("HasPackageFile = true; want false")
	}
	if effective.PackageInfo != nil {
		t.Fatalf("PackageInfo = %#v; want nil", effective.PackageInfo)
	}
	if effective.Manifest.Package != "cloud.lazycat.legacy" || effective.Manifest.Name != "Legacy" {
		t.Fatalf("Manifest package metadata = %+v", effective.Manifest.PackageInfo)
	}
	if effective.Manifest.Presence.Package != manifest.Value || effective.Manifest.Presence.Author != manifest.Absent {
		t.Fatalf("Manifest presence = %+v", effective.Manifest.Presence)
	}
	assertLookup(t, effective.Source, true, "future_field", "enabled")

	if err := effective.Source.Set("changed", "application", "subdomain"); err != nil {
		t.Fatalf("effective.Source.Set() error = %v", err)
	}
	assertLookup(t, source, "legacy", "application", "subdomain")
}

func TestLoadEffectivePackageFilePreservesEmptyAndNullOverrides(t *testing.T) {
	t.Parallel()

	source := mustParse(t, `package: cloud.lazycat.old
name: Old Name
description: Old description
author: Old author
application:
  subdomain: example
`)
	packageDocument := mustParse(t, `package: cloud.lazycat.new
description: ""
author: null
`)
	sourceBefore := mustBytes(t, source)
	packageBefore := mustBytes(t, packageDocument)

	effective, err := manifest.LoadEffective(source, packageDocument, false)
	if err != nil {
		t.Fatalf("LoadEffective() error = %v", err)
	}
	if !effective.HasPackageFile {
		t.Fatal("HasPackageFile = false; want true")
	}
	if effective.PackageInfo == nil {
		t.Fatal("PackageInfo = nil")
	}
	if effective.Manifest.Package != "cloud.lazycat.new" || effective.Manifest.Name != "" || effective.Manifest.Description != "" || effective.Manifest.Author != "" {
		t.Fatalf("effective Manifest metadata = %+v", effective.Manifest.PackageInfo)
	}
	wantPresence := manifest.PackagePresence{
		Package:     manifest.Value,
		Description: manifest.Value,
		Author:      manifest.Null,
	}
	if effective.Manifest.Presence != wantPresence {
		t.Fatalf("Manifest presence = %+v; want %+v", effective.Manifest.Presence, wantPresence)
	}
	if effective.PackageInfo.Presence != wantPresence {
		t.Fatalf("PackageInfo presence = %+v; want %+v", effective.PackageInfo.Presence, wantPresence)
	}
	if got := mustBytes(t, source); string(got) != string(sourceBefore) {
		t.Fatalf("source mutated:\n%s\nwant:\n%s", got, sourceBefore)
	}
	if got := mustBytes(t, packageDocument); string(got) != string(packageBefore) {
		t.Fatalf("package document mutated:\n%s\nwant:\n%s", got, packageBefore)
	}
}

func TestLoadEffectiveAliasNullRoundTripsAsNull(t *testing.T) {
	t.Parallel()

	source := mustParse(t, "application:\n  subdomain: example\n")
	packageDocument := mustParse(t, `_null: &n null
author: *n
`)

	effective, err := manifest.LoadEffective(source, packageDocument, false)
	if err != nil {
		t.Fatalf("LoadEffective() error = %v", err)
	}
	if effective.PackageInfo == nil || effective.PackageInfo.Presence.Author != manifest.Null {
		t.Fatalf("PackageInfo presence = %+v; want author Null", effective.PackageInfo)
	}
	if effective.Manifest.Presence.Author != manifest.Null {
		t.Fatalf("Manifest presence = %+v; want author Null", effective.Manifest.Presence)
	}

	manifestDocument, splitPackage, err := manifest.SplitEffective(effective.Source, effective.PackageInfo, nil)
	if err != nil {
		t.Fatalf("SplitEffective() error = %v", err)
	}
	assertLookup(t, splitPackage, nil, "author")
	roundTripped, err := manifest.LoadEffective(manifestDocument, splitPackage, true)
	if err != nil {
		t.Fatalf("LoadEffective(split documents) error = %v", err)
	}
	if roundTripped.Manifest.Presence.Author != manifest.Null {
		t.Fatalf("round-tripped presence = %+v; want author Null", roundTripped.Manifest.Presence)
	}
}

func TestLoadEffectiveStrictRejectsStaticManifestFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		staticField string
	}{
		{name: "value", staticField: "package: cloud.lazycat.legacy"},
		{name: "empty", staticField: `package: ""`},
		{name: "null", staticField: "package: null"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			source := mustParse(t, test.staticField+"\napplication:\n  subdomain: example\n")
			packageDocument := mustParse(t, "package: cloud.lazycat.v2\n")
			_, err := manifest.LoadEffective(source, packageDocument, true)
			if !errors.Is(err, lpkgo.ErrInvalidManifest) {
				t.Fatalf("LoadEffective(strict) error = %v; want INVALID_MANIFEST", err)
			}
		})
	}
}

func TestSplitEffectiveMovesStaticFieldsAndPreservesSource(t *testing.T) {
	t.Parallel()

	source := mustParse(t, `# package identifier comment
package: cloud.lazycat.example
version: 2.3.4
name: Example
description: remove me
author: Example Author
license: MIT
homepage: https://example.test
min_os_version: 1.5.0
unsupported_platforms:
  - ios
locales:
  en:
    name: Example
application:
  subdomain: example
future_field:
  nested: preserved
`)
	sourceBefore := mustBytes(t, source)

	manifestDocument, packageDocument, err := manifest.SplitEffective(source, nil, []string{"description"})
	if err != nil {
		t.Fatalf("SplitEffective() error = %v", err)
	}
	for _, field := range manifest.StaticPackageFields() {
		if _, found, err := manifestDocument.Lookup(field); err != nil {
			t.Fatalf("manifest Lookup(%q) error = %v", field, err)
		} else if found {
			t.Errorf("manifest Lookup(%q) found static field", field)
		}
	}
	assertLookup(t, manifestDocument, "example", "application", "subdomain")
	assertLookup(t, manifestDocument, "preserved", "future_field", "nested")
	assertLookup(t, packageDocument, "cloud.lazycat.example", "package")
	assertLookup(t, packageDocument, "2.3.4", "version")
	assertLookup(t, packageDocument, "Example", "name")
	assertLookup(t, packageDocument, "Example Author", "author")
	assertLookup(t, packageDocument, "MIT", "license")
	assertLookup(t, packageDocument, "https://example.test", "homepage")
	assertLookup(t, packageDocument, "1.5.0", "min_os_version")
	assertLookup(t, packageDocument, "Example", "locales", "en", "name")
	if _, found, err := packageDocument.Lookup("description"); err != nil {
		t.Fatalf("package Lookup(description) error = %v", err)
	} else if found {
		t.Fatal("removed description remains in package document")
	}
	if encoded := mustBytes(t, packageDocument); !bytes.Contains(encoded, []byte("# package identifier comment")) {
		t.Fatalf("package document lost moved comment:\n%s", encoded)
	}
	if got := mustBytes(t, source); string(got) != string(sourceBefore) {
		t.Fatalf("source mutated:\n%s\nwant:\n%s", got, sourceBefore)
	}
}

func TestStaticPackageFieldsReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	fields := manifest.StaticPackageFields()
	fields[0] = "changed"
	if got := manifest.StaticPackageFields()[0]; got != "package" {
		t.Fatalf("StaticPackageFields()[0] = %q; want package", got)
	}
}

func TestSplitEffectiveRoundTripsPackagePresence(t *testing.T) {
	t.Parallel()

	source := mustParse(t, `package: cloud.lazycat.example
version: 3.0.0
name: Source Name
description: Source description
author: Source author
application:
  subdomain: example
`)
	packageInfo := &manifest.PackageInfo{
		Name:        "Package Name",
		Description: "",
		Author:      "ignored for null",
		Homepage:    "https://example.test",
		Presence: manifest.PackagePresence{
			Name:        manifest.Value,
			Description: manifest.Value,
			Author:      manifest.Null,
		},
	}

	manifestDocument, packageDocument, err := manifest.SplitEffective(source, packageInfo, nil)
	if err != nil {
		t.Fatalf("SplitEffective() error = %v", err)
	}
	assertLookup(t, packageDocument, "cloud.lazycat.example", "package")
	assertLookup(t, packageDocument, "Package Name", "name")
	assertLookup(t, packageDocument, "", "description")
	assertLookup(t, packageDocument, nil, "author")
	assertLookup(t, packageDocument, "https://example.test", "homepage")

	effective, err := manifest.LoadEffective(manifestDocument, packageDocument, true)
	if err != nil {
		t.Fatalf("LoadEffective(split documents) error = %v", err)
	}
	if effective.Manifest.Name != "Package Name" || effective.Manifest.Description != "" || effective.Manifest.Author != "" || effective.Manifest.Homepage != "https://example.test" {
		t.Fatalf("round-tripped metadata = %+v", effective.Manifest.PackageInfo)
	}
	if effective.Manifest.Presence.Description != manifest.Value || effective.Manifest.Presence.Author != manifest.Null || effective.Manifest.Presence.Homepage != manifest.Value {
		t.Fatalf("round-tripped presence = %+v", effective.Manifest.Presence)
	}
}

func TestSplitEffectiveLocalizesManifestAliasToMovedStaticAnchor(t *testing.T) {
	t.Parallel()

	source := mustParse(t, `description: &shared shared-value
application:
  environment:
    - *shared
`)
	manifestDocument, packageDocument, err := manifest.SplitEffective(source, nil, nil)
	if err != nil {
		t.Fatalf("SplitEffective() error = %v", err)
	}

	reparsedManifest, err := manifest.Parse(mustBytes(t, manifestDocument))
	if err != nil {
		t.Fatalf("Parse(manifest Bytes()) error = %v", err)
	}
	var typedManifest manifest.Manifest
	if err := reparsedManifest.Decode(&typedManifest); err != nil {
		t.Fatalf("manifest Decode() error = %v", err)
	}
	environment, ok := typedManifest.Application.Environment.([]any)
	if !ok || len(environment) != 1 || environment[0] != "shared-value" {
		t.Fatalf("manifest environment = %#v; want [shared-value]", typedManifest.Application.Environment)
	}

	reparsedPackage, err := manifest.Parse(mustBytes(t, packageDocument))
	if err != nil {
		t.Fatalf("Parse(package Bytes()) error = %v", err)
	}
	assertLookup(t, reparsedPackage, "shared-value", "description")
}

func TestSplitEffectiveLocalizesMovedStaticAliasToManifestAnchor(t *testing.T) {
	t.Parallel()

	source := mustParse(t, `application:
  subdomain: &shared shared-value
description: *shared
`)
	manifestDocument, packageDocument, err := manifest.SplitEffective(source, nil, nil)
	if err != nil {
		t.Fatalf("SplitEffective() error = %v", err)
	}

	reparsedManifest, err := manifest.Parse(mustBytes(t, manifestDocument))
	if err != nil {
		t.Fatalf("Parse(manifest Bytes()) error = %v", err)
	}
	assertLookup(t, reparsedManifest, "shared-value", "application", "subdomain")

	reparsedPackage, err := manifest.Parse(mustBytes(t, packageDocument))
	if err != nil {
		t.Fatalf("Parse(package Bytes()) error = %v", err)
	}
	assertLookup(t, reparsedPackage, "shared-value", "description")
}

func mustParse(t *testing.T, input string) *manifest.Document {
	t.Helper()

	document, err := manifest.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return document
}

func mustBytes(t *testing.T, document *manifest.Document) []byte {
	t.Helper()

	data, err := document.Bytes()
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	return data
}
