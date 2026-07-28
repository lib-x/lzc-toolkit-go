package manifest_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/manifest"
)

func TestAnalyzeProjectsAndRestoresLazyCatTemplates(t *testing.T) {
	t.Parallel()
	source := []byte(`{{ if .U.target }}
usage: "to {{ .U.target }}"
{{ else }}
usage: "netmap"
{{ end }}
application:
  subdomain: netmap
  ingress:
    - port: {{ index .U "listen.port" }}
services:
  app:
    image: registry.lazycat.cloud/example/app:1.0.0
    environment:
      - SECRET={{ stable_secret "app_secret" }}
      - URL={{if .U.base_url}}{{.U.base_url}}{{else}}https://{{.S.AppDomain}}{{end}}
`)

	analysis, err := manifest.Analyze(source)
	if err != nil {
		t.Fatal(err)
	}
	info := analysis.Template()
	if !info.Present || info.ControlCount != 3 || !info.HasConditionalBlocks || !info.HasInlineConditions {
		t.Fatalf("Template() = %#v", info)
	}
	info.ActionKinds[0] = "mutated"
	if analysis.Template().ActionKinds[0] == "mutated" {
		t.Fatal("Template() returned a shared ActionKinds slice")
	}
	projected, err := analysis.Document().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(projected, []byte(".U.target")) || bytes.Contains(projected, []byte("stable_secret")) {
		t.Fatalf("projection leaked template bodies:\n%s", projected)
	}
	restored, err := analysis.Restore(projected)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range [][]byte{
		[]byte(`{{ if .U.target }}`),
		[]byte(`{{ .U.target }}`),
		[]byte(`{{ index .U "listen.port" }}`),
		[]byte(`{{ stable_secret "app_secret" }}`),
		[]byte(`{{if .U.base_url}}`),
		[]byte(`{{.S.AppDomain}}`),
	} {
		if !bytes.Contains(restored, action) {
			t.Fatalf("restored source is missing %q:\n%s", action, restored)
		}
	}
}

func TestAnalyzeSupportsInlineConditionAroundWholeSequenceItem(t *testing.T) {
	t.Parallel()
	source := []byte(`services:
  api:
    environment:
      - REQUIRED=true
      {{if .U.client_id}}- CLIENT_ID={{ .U.client_id }}{{end}}
`)

	analysis, err := manifest.Analyze(source)
	if err != nil {
		t.Fatal(err)
	}
	if info := analysis.Template(); !info.Present || !info.HasInlineConditions {
		t.Fatalf("Template() = %#v", info)
	}
	projected, err := analysis.Document().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(projected, []byte(".U.client_id")) {
		t.Fatalf("projection leaked template body:\n%s", projected)
	}
	restored, err := analysis.Restore(projected)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range [][]byte{
		[]byte(`{{if .U.client_id}}`),
		[]byte(`{{ .U.client_id }}`),
		[]byte(`{{end}}`),
	} {
		if !bytes.Contains(restored, action) {
			t.Fatalf("restored source is missing %q:\n%s", action, restored)
		}
	}
}

func TestAnalyzePlainYAMLReturnsIndependentDocument(t *testing.T) {
	t.Parallel()
	analysis, err := manifest.Analyze([]byte("application:\n  subdomain: original\n"))
	if err != nil {
		t.Fatal(err)
	}
	if info := analysis.Template(); info.Present || info.ControlCount != 0 || info.ExpressionCount != 0 {
		t.Fatalf("Template() = %#v", info)
	}

	first := analysis.Document()
	if err := first.Set("changed", "application", "subdomain"); err != nil {
		t.Fatal(err)
	}
	value, found, err := analysis.Document().Lookup("application", "subdomain")
	if err != nil {
		t.Fatal(err)
	}
	if !found || value != "original" {
		t.Fatalf("independent document value = %#v, found = %v", value, found)
	}
}

func TestAnalyzePreservesTrimMarkersAndIndentation(t *testing.T) {
	t.Parallel()
	source := []byte("application:\n  {{- if .U.enabled }}\n  subdomain: demo\n  {{- end -}}\nvalue: {{- printf `%s` .U.value -}}\n")
	analysis, err := manifest.Analyze(source)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := analysis.Document().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(projected, []byte("  # lzc-toolkit-template-control-0")) {
		t.Fatalf("projection lost control indentation:\n%s", projected)
	}
	restored, err := analysis.Restore(projected)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range [][]byte{
		[]byte("\n  {{- if .U.enabled }}\n"),
		[]byte("\n  {{- end -}}\n"),
		[]byte("value: {{- printf `%s` .U.value -}}"),
	} {
		if !bytes.Contains(restored, fragment) {
			t.Fatalf("restored source is missing %q:\n%s", fragment, restored)
		}
	}
}

func TestAnalyzeRejectsUnclosedTemplateActionWithoutLeakingBody(t *testing.T) {
	t.Parallel()
	const secretBody = `stable_secret "do-not-leak-this-body"`
	_, err := manifest.Analyze([]byte("value: {{ " + secretBody + "\n"))
	if err == nil {
		t.Fatal("Analyze() error = nil")
	}
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) || toolkitError.Code != lpkgo.CodeInvalidManifest {
		t.Fatalf("Analyze() error = %#v", err)
	}
	if toolkitError.Path != "" {
		t.Fatalf("Analyze() error path = %q", toolkitError.Path)
	}
	if bytes.Contains([]byte(err.Error()), []byte(secretBody)) || bytes.Contains([]byte(toolkitError.Cause.Error()), []byte(secretBody)) {
		t.Fatalf("Analyze() error leaked template body: %#v", err)
	}
}

func TestAnalyzeRejectsReservedMarkerPrefix(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{
		"lzc-toolkit-template-control-",
		"lzc-toolkit-template-expression-",
	} {
		prefix := prefix
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			_, err := manifest.Analyze([]byte("value: " + prefix + "0\n"))
			if !errors.Is(err, lpkgo.ErrInvalidManifest) {
				t.Fatalf("Analyze() error = %#v", err)
			}
		})
	}
}

func TestRestoreRejectsMissingAndDuplicatedMarkers(t *testing.T) {
	t.Parallel()
	analysis, err := manifest.Analyze([]byte("first: {{ .U.first }}\nsecond: {{ .U.second }}\n"))
	if err != nil {
		t.Fatal(err)
	}
	projected, err := analysis.Document().Bytes()
	if err != nil {
		t.Fatal(err)
	}

	missing := bytes.Replace(projected, []byte("lzc-toolkit-template-expression-0"), []byte("missing"), 1)
	if _, err := analysis.Restore(missing); !errors.Is(err, lpkgo.ErrInvalidManifest) {
		t.Fatalf("Restore(missing marker) error = %#v", err)
	}
	duplicated := append(append([]byte(nil), projected...), []byte("copy: lzc-toolkit-template-expression-1\n")...)
	if _, err := analysis.Restore(duplicated); !errors.Is(err, lpkgo.ErrInvalidManifest) {
		t.Fatalf("Restore(duplicated marker) error = %#v", err)
	}
}

func TestAnalyzeHonorsQuotedClosingDelimiterInsideAction(t *testing.T) {
	t.Parallel()
	source := []byte("value: {{ printf \"%s}}\" .U.value }}\n")
	analysis, err := manifest.Analyze(source)
	if err != nil {
		t.Fatal(err)
	}
	if info := analysis.Template(); info.ExpressionCount != 1 {
		t.Fatalf("Template() = %#v", info)
	}
	projected, err := analysis.Document().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(projected, []byte(".U.value")) || bytes.Contains(projected, []byte(`%s}}`)) {
		t.Fatalf("projection leaked action body:\n%s", projected)
	}
	restored, err := analysis.Restore(projected)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(restored, []byte(`value: {{ printf "%s}}" .U.value }}`)) {
		t.Fatalf("Restore() = %s", restored)
	}
}

func TestRestoreDistinguishesNumericMarkerSuffixes(t *testing.T) {
	t.Parallel()
	var source strings.Builder
	for index := range 11 {
		fmt.Fprintf(&source, "value%d: {{ .U.value%d }}\n", index, index)
	}
	analysis, err := manifest.Analyze([]byte(source.String()))
	if err != nil {
		t.Fatal(err)
	}
	projected, err := analysis.Document().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := analysis.Restore(projected)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(restored, []byte("{{ .U.value1 }}")) || !bytes.Contains(restored, []byte("{{ .U.value10 }}")) {
		t.Fatalf("Restore() confused numeric marker suffixes:\n%s", restored)
	}
}

func TestAnalyzeRejectsInvalidControlStructure(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"unmatched else": "{{ else }}\nvalue: demo\n",
		"unmatched end":  "{{ end }}\nvalue: demo\n",
		"duplicate else": "{{ if .U.enabled }}\nvalue: one\n{{ else }}\nvalue: two\n{{ else }}\nvalue: three\n{{ end }}\n",
		"unclosed block": "{{ range .U.values }}\nvalue: demo\n",
	}
	for name, source := range tests {
		name, source := name, source
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := manifest.Analyze([]byte(source)); !errors.Is(err, lpkgo.ErrInvalidManifest) {
				t.Fatalf("Analyze() error = %#v", err)
			}
		})
	}
}

func TestAnalyzeSupportsElseIfChain(t *testing.T) {
	t.Parallel()
	source := []byte(`{{ if .U.first }}
value: first
{{ else if .U.second }}
value: second
{{ else }}
value: fallback
{{ end }}
`)
	analysis, err := manifest.Analyze(source)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := analysis.Document().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := analysis.Restore(projected)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(restored, []byte("{{ else if .U.second }}")) {
		t.Fatalf("Restore() lost else-if chain:\n%s", restored)
	}
}

func TestAnalyzeSupportsElseWithChain(t *testing.T) {
	t.Parallel()
	source := []byte(`{{ with .U.first }}
value: first
{{ else with .U.second }}
value: second
{{ else }}
value: fallback
{{ end }}
`)
	analysis, err := manifest.Analyze(source)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := analysis.Document().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := analysis.Restore(projected)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(restored, []byte("{{ else with .U.second }}")) {
		t.Fatalf("Restore() lost else-with chain:\n%s", restored)
	}
}

func TestAnalyzeReturnsSortedActionKinds(t *testing.T) {
	t.Parallel()
	source := []byte(`{{ if .U.enabled }}
value: {{ stable_secret "secret" }}-{{ index .U "port" }}-{{ .U.value }}
{{ else }}
value: fallback
{{ end }}
`)
	analysis, err := manifest.Analyze(source)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"else", "end", "expression", "if", "index", "stable_secret"}
	if got := analysis.Template().ActionKinds; !slices.Equal(got, want) {
		t.Fatalf("Template().ActionKinds = %#v, want %#v", got, want)
	}
}

func TestAnalyzeActionKindsNeverExposeLiteralTokens(t *testing.T) {
	t.Parallel()
	source := []byte("string: {{ \"private.registry/internal/image:secret-tag\" }}\nnumber: {{ 8675309 }}\nraw: {{ `private-raw-literal` }}\ncharacter: {{ 'x' }}\nvariable: {{ $private }}\npath: {{ .U.private }}\nparenthesized: {{ (index .U \"port\") }}\nknown: {{ index .U \"port\" }}-{{ stable_secret \"name\" }}\n")
	analysis, err := manifest.Analyze(source)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"expression", "index", "stable_secret"}
	if got := analysis.Template().ActionKinds; !slices.Equal(got, want) {
		t.Fatalf("Template().ActionKinds = %#v, want %#v", got, want)
	}
	encoded, err := json.Marshal(analysis.Template())
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private.registry/internal/image:secret-tag", "8675309", "private-raw-literal", "$private", ".U.private", "(index"} {
		if bytes.Contains(encoded, []byte(private)) {
			t.Fatalf("Template() exposed %q: %s", private, encoded)
		}
	}
}

func TestAnalysisStaticScalarRequiresOneUnconditionalExpressionFreeValue(t *testing.T) {
	t.Parallel()
	source := []byte(`string: value
boolean: true
integer: 42
unsigned: 18446744073709551615
floating: 1.25
null_value: null
templated: {{ .U.value }}
duplicate: first
duplicate: second
{{ if .U.enabled }}
conditional: hidden
{{ end }}
application:
  subdomain: demo
`)
	analysis, err := manifest.Analyze(source)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path  []string
		want  any
		found bool
	}{
		{path: []string{"string"}, want: "value", found: true},
		{path: []string{"boolean"}, want: true, found: true},
		{path: []string{"integer"}, want: 42, found: true},
		{path: []string{"unsigned"}, want: uint64(18446744073709551615), found: true},
		{path: []string{"floating"}, want: 1.25, found: true},
		{path: []string{"null_value"}, want: nil, found: true},
		{path: []string{"application", "subdomain"}, want: "demo", found: true},
		{path: []string{"templated"}, found: false},
		{path: []string{"duplicate"}, found: false},
		{path: []string{"conditional"}, found: false},
	}
	for _, test := range tests {
		got, found, err := analysis.StaticScalar(test.path...)
		if err != nil {
			t.Fatalf("StaticScalar(%v) error = %v", test.path, err)
		}
		if found != test.found || !reflect.DeepEqual(got, test.want) {
			t.Fatalf("StaticScalar(%v) = (%#v, %t), want (%#v, %t)", test.path, got, found, test.want, test.found)
		}
	}
}
