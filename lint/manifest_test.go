package lint_test

import (
	"reflect"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/lint"
	"github.com/lib-x/lzc-toolkit-go/manifest"
)

func TestManifestReportsCompatibilityWarningsInStableOrder(t *testing.T) {
	t.Parallel()

	document, err := manifest.Parse([]byte(`package: cloud.lazycat.example
version: 1.0.0
ext_config:
  future_ext: true
  disable_url_raw_path: true
  remove_this_request_headers: [x-secret]
  fix_websocket_header: true
application:
  handlers: {}
  user_app: false
  depends_on: []
  entries:
    - id: main
      future_entry: true
services:
  zeta:
    health_check: {}
  alpha:
    health_check: null
    future_service: true
future_top: true
`))
	if err != nil {
		t.Fatalf("manifest.Parse() error = %v", err)
	}
	var typed manifest.Manifest
	if err := document.Decode(&typed); err != nil {
		t.Fatalf("Document.Decode() error = %v", err)
	}

	warnings, err := lint.Manifest(document, typed)
	if err != nil {
		t.Fatalf("lint.Manifest() error = %v", err)
	}
	got := make([][2]string, len(warnings))
	for index, warning := range warnings {
		if warning.Severity != lpkgo.SeverityWarning {
			t.Errorf("warnings[%d].Severity = %q; want WARNING", index, warning.Severity)
		}
		got[index] = [2]string{warning.Code, warning.Path}
	}
	want := [][2]string{
		{"unknown-manifest-fields", "ext_config.future_ext,application.entries[0].future_entry,services.alpha.future_service,future_top"},
		{"legacy-static-package-fields", "package,version"},
		{"application-handlers-deprecated", "application.handlers"},
		{"application-user-app-deprecated", "application.user_app"},
		{"application-depends-on-deprecated", "application.depends_on"},
		{"service-health-check-deprecated", "services.alpha.health_check,services.zeta.health_check"},
		{"ext-config-http-routing-deprecated", "ext_config.disable_url_raw_path,ext_config.remove_this_request_headers,ext_config.fix_websocket_header"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lint.Manifest() code/path = %#v; want %#v", got, want)
	}
}
