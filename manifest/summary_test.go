package manifest

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestSummaryExtractsNormalizedManifestSource(t *testing.T) {
	source := []byte(`package: community.lazycat.app.summary
version: 1.2.3
name: Summary
locales:
  zh:
    name: 摘要
  en:
    name: Summary
unsupported_platforms: [windows, darwin]
application:
  subdomain: summary
  upstreams:
    - location: /
      backend: http://web:8080/
ext_config:
  permissions:
    - user.notify
services:
  cache:
    image: embed:cache_image
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
    image: {{ .U.worker_image }}
{{ end }}
`)

	analysis, err := Analyze(source)
	if err != nil {
		t.Fatal(err)
	}
	summary := analysis.Summary()

	if summary.Package.Package != "community.lazycat.app.summary" || summary.Package.Version != "1.2.3" || summary.Package.Name != "Summary" {
		t.Fatalf("Package = %#v", summary.Package)
	}
	if want := []string{"en", "zh"}; !reflect.DeepEqual(summary.Package.LocaleCodes, want) {
		t.Fatalf("LocaleCodes = %v, want %v", summary.Package.LocaleCodes, want)
	}
	if want := []string{"darwin", "windows"}; !reflect.DeepEqual(summary.Package.UnsupportedPlatforms, want) {
		t.Fatalf("UnsupportedPlatforms = %v, want %v", summary.Package.UnsupportedPlatforms, want)
	}
	if summary.Application.Subdomain != "summary" || !summary.Application.HasServices || !summary.Application.HasUpstreams || !summary.Application.HasPermissions {
		t.Fatalf("Application = %#v", summary.Application)
	}
	if want := []string{"user.notify"}; !reflect.DeepEqual(summary.Application.Permissions, want) {
		t.Fatalf("Permissions = %v, want %v", summary.Application.Permissions, want)
	}
	if got := serviceNames(summary.Services); !reflect.DeepEqual(got, []string{"cache", "db", "web", "worker"}) {
		t.Fatalf("service names = %v", got)
	}
	if !summary.Services[1].HasHealthcheck || !reflect.DeepEqual(summary.Services[2].DependsOn, []string{"db"}) || !summary.Services[3].Conditional {
		t.Fatalf("Services = %#v", summary.Services)
	}
	if got := imageTargets(summary.Images); !reflect.DeepEqual(got, []string{"services.cache.image", "services.db.image", "services.web.image", "services.worker.image"}) {
		t.Fatalf("image targets = %v", got)
	}
	if summary.Images[0].EmbeddedAlias != "cache_image" || summary.Images[0].RuntimeRef != "embed:cache_image" || !summary.Images[0].Editable {
		t.Fatalf("embedded image = %#v", summary.Images[0])
	}
	if summary.Images[2].UpstreamRef != "ghcr.io/example/web:v1.2.3" {
		t.Fatalf("web image = %#v", summary.Images[2])
	}
	worker := summary.Images[3]
	if !worker.Templated || !worker.Conditional || worker.RuntimeRef != "" || worker.Editable {
		t.Fatalf("worker image = %#v", worker)
	}
	if !hasDiagnostic(summary.Diagnostics, "TEMPLATED_IMAGE", "services.worker.image") {
		t.Fatalf("Diagnostics = %#v", summary.Diagnostics)
	}
}

func TestSummaryRetainsMutuallyExclusiveDuplicateUsageKeys(t *testing.T) {
	analysis, err := Analyze([]byte(`{{ if .U.basic }}
usage: basic
{{ else }}
usage: advanced
{{ end }}
`))
	if err != nil {
		t.Fatal(err)
	}
	_ = analysis.Summary()
	document := analysis.Document()
	mapping, err := topLevelMapping(document, "test")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == "usage" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("usage key count = %d, want 2", count)
	}
}

func TestSummaryDoesNotGuessConditionalOrDuplicateIdentityFields(t *testing.T) {
	analysis, err := Analyze([]byte(`{{ if .U.community }}
package: community.lazycat.app.one
version: 1.0.0
name: One
description: First
author: First Author
license: MIT
homepage: https://one.example
min_os_version: 1.3.8
{{ else }}
package: community.lazycat.app.two
version: 2.0.0
name: Two
description: Second
author: Second Author
license: Apache-2.0
homepage: https://two.example
min_os_version: 1.4.0
{{ end }}
application:
{{ if .U.primary }}
  subdomain: one
{{ else }}
  subdomain: two
{{ end }}
`))
	if err != nil {
		t.Fatal(err)
	}
	summary := analysis.Summary()
	if summary.Package.Package != "" || summary.Package.Version != "" || summary.Package.Name != "" ||
		summary.Package.Description != "" || summary.Package.Author != "" || summary.Package.License != "" ||
		summary.Package.Homepage != "" || summary.Package.MinOSVersion != "" || summary.Application.Subdomain != "" {
		t.Fatalf("summary guessed ambiguous identity fields: %#v / %#v", summary.Package, summary.Application)
	}
	for _, path := range []string{"package", "version", "name", "description", "author", "license", "homepage", "min_os_version", "application.subdomain"} {
		if !hasDiagnostic(summary.Diagnostics, "TEMPLATED_FIELD", path) {
			t.Fatalf("Diagnostics missing TEMPLATED_FIELD for %q: %#v", path, summary.Diagnostics)
		}
	}
}

func TestSummaryDoesNotExposeTemplatedUpstreamComments(t *testing.T) {
	const actionKind = ".U.upstream_image"
	analysis, err := Analyze([]byte(`services:
  api:
    # upstream: {{ ` + actionKind + ` }}
    image: registry.lazycat.cloud/example/api:abc123
`))
	if err != nil {
		t.Fatal(err)
	}
	summary := analysis.Summary()
	if len(summary.Images) != 1 || summary.Images[0].UpstreamRef != "" {
		t.Fatalf("Images = %#v", summary.Images)
	}
	if !hasDiagnostic(summary.Diagnostics, "TEMPLATED_FIELD", "services.api.image.upstreamRef") {
		t.Fatalf("Diagnostics = %#v", summary.Diagnostics)
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{templateExpressionPrefix, templateControlPrefix, actionKind, "{{ " + actionKind + " }}"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("summary exposed %q: %s", private, encoded)
		}
	}
}

func TestSummaryMarksDuplicateConditionalServiceAsAmbiguous(t *testing.T) {
	analysis, err := Analyze([]byte(`services:
{{ if .U.v1 }}
  api:
    image: example/api:v1
{{ else }}
  api:
    image: example/api:v2
{{ end }}
`))
	if err != nil {
		t.Fatal(err)
	}
	summary := analysis.Summary()
	if len(summary.Services) != 2 || !summary.Services[0].Conditional || !summary.Services[1].Conditional {
		t.Fatalf("Services = %#v", summary.Services)
	}
	if len(summary.Images) != 2 {
		t.Fatalf("Images = %#v", summary.Images)
	}
	for _, image := range summary.Images {
		if image.Editable || image.Reason == "" {
			t.Fatalf("duplicate image = %#v", image)
		}
	}
	if !hasDiagnostic(summary.Diagnostics, "DUPLICATE_SERVICE", "services.api") ||
		!hasDiagnostic(summary.Diagnostics, "DUPLICATE_IMAGE_TARGET", "services.api.image") {
		t.Fatalf("Diagnostics = %#v", summary.Diagnostics)
	}
}

func TestSummaryDetectsApplicationExecAndImageForms(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "recursive route", source: `application:
  image: app:v1
  routes:
    - /launch:
        nested: exec://bin/start
`},
		{name: "upstream launch command", source: `application:
  image: app:v1
  upstreams:
    - location: /
      backend_launch_command: ./start
`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			analysis, err := Analyze([]byte(test.source))
			if err != nil {
				t.Fatal(err)
			}
			summary := analysis.Summary()
			if !summary.Application.HasImage || !summary.Application.HasExecLaunch || len(summary.Images) != 1 {
				t.Fatalf("Summary = %#v", summary)
			}
			if summary.Images[0].Target != "application.image" || summary.Images[0].Service != "" || summary.Images[0].RuntimeRef != "app:v1" {
				t.Fatalf("Images = %#v", summary.Images)
			}
		})
	}
}

func TestSummaryReportsLegacyHealthCheck(t *testing.T) {
	analysis, err := Analyze([]byte(`services:
  db:
    image: postgres:16
    health_check:
      test: pg_isready
`))
	if err != nil {
		t.Fatal(err)
	}
	summary := analysis.Summary()
	if len(summary.Services) != 1 || !summary.Services[0].HasHealthcheck {
		t.Fatalf("Services = %#v", summary.Services)
	}
	if !hasDiagnostic(summary.Diagnostics, "LEGACY_HEALTH_CHECK", "services.db.health_check") {
		t.Fatalf("Diagnostics = %#v", summary.Diagnostics)
	}
}

func TestSummaryRejectsOpaquePermissionAndMalformedEmbedForms(t *testing.T) {
	analysis, err := Analyze([]byte(`ext_config:
  permissions: user.notify
services:
  empty:
    image: "embed:"
  spaced:
    image: "embed:bad alias"
  digest:
    image: "embed:app@not-a-digest"
`))
	if err != nil {
		t.Fatal(err)
	}
	summary := analysis.Summary()
	if !summary.Application.HasPermissions || len(summary.Application.Permissions) != 0 ||
		!hasDiagnostic(summary.Diagnostics, "UNSUPPORTED_PERMISSIONS_SHAPE", "ext_config.permissions") {
		t.Fatalf("Application/Diagnostics = %#v / %#v", summary.Application, summary.Diagnostics)
	}
	for _, image := range summary.Images {
		if image.EmbeddedAlias != "" {
			t.Fatalf("malformed embedded image = %#v", image)
		}
	}
}

func TestSummaryNeverExposesEnvironmentOrTemplateBodies(t *testing.T) {
	const secret = "TOP_SECRET_VALUE"
	const action = ".U.private_image_expression"
	analysis, err := Analyze([]byte(`application:
  environment:
    SECRET: ` + secret + `
services:
  api:
    environment:
      TOKEN: ` + secret + `
    image: {{ ` + action + ` }}
`))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(analysis.Summary())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), "{{ "+action+" }}") {
		t.Fatalf("summary exposed private source: %s", encoded)
	}
}

func TestSummaryReturnsDefensiveSortedCopies(t *testing.T) {
	analysis, err := Analyze([]byte(`locales:
  zh: {}
  en: {}
unsupported_platforms: [windows, darwin]
ext_config:
  permissions: [z.permission, a.permission]
services:
  web:
    image: web:v1
    depends_on: {db: {condition: service_healthy}, cache: {}}
  cache:
    image: cache:v1
  db:
    image: db:v1
`))
	if err != nil {
		t.Fatal(err)
	}
	first := analysis.Summary()
	first.Package.LocaleCodes[0] = "mutated"
	first.Package.UnsupportedPlatforms[0] = "mutated"
	first.Application.Permissions[0] = "mutated"
	first.Services[0].DependsOn = append(first.Services[0].DependsOn, "mutated")
	first.Services[2].DependsOn[0] = "mutated"
	first.Services[0].Name = "mutated"
	first.Images[0].Target = "mutated"
	first.Template.ActionKinds = append(first.Template.ActionKinds, "mutated")
	first.Diagnostics = append(first.Diagnostics, Diagnostic{Code: "mutated"})

	second := analysis.Summary()
	if !reflect.DeepEqual(second.Package.LocaleCodes, []string{"en", "zh"}) ||
		!reflect.DeepEqual(second.Package.UnsupportedPlatforms, []string{"darwin", "windows"}) ||
		!reflect.DeepEqual(second.Application.Permissions, []string{"a.permission", "z.permission"}) ||
		!reflect.DeepEqual(serviceNames(second.Services), []string{"cache", "db", "web"}) ||
		!reflect.DeepEqual(second.Services[2].DependsOn, []string{"cache", "db"}) ||
		!reflect.DeepEqual(imageTargets(second.Images), []string{"services.cache.image", "services.db.image", "services.web.image"}) {
		t.Fatalf("second Summary = %#v", second)
	}
}

func serviceNames(services []ServiceSummary) []string {
	result := make([]string, len(services))
	for index, service := range services {
		result[index] = service.Name
	}
	return result
}

func imageTargets(images []ImageSummary) []string {
	result := make([]string, len(images))
	for index, image := range images {
		result[index] = image.Target
	}
	return result
}

func hasDiagnostic(diagnostics []Diagnostic, code string, path string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code && diagnostic.Path == path {
			return true
		}
	}
	return false
}
