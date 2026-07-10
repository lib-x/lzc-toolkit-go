package manifest_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/manifest"
)

func TestDocumentPreservesSyntaxAndIsolatesMutations(t *testing.T) {
	t.Parallel()

	input := []byte(`# leading package comment
package: cloud.lazycat.example
application:
  subdomain: old
  future:
    enabled: true
`)
	document, err := manifest.Parse(input)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	input[2] = 'X'
	if err := document.Set("new", "application", "subdomain"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	clone := document.Clone()
	if err := clone.Set(false, "application", "future", "enabled"); err != nil {
		t.Fatalf("Clone().Set() error = %v", err)
	}
	originalFuture, found, err := document.Lookup("application", "future", "enabled")
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if !found || originalFuture != true {
		t.Fatalf("original future value = %#v, %v; want true, true", originalFuture, found)
	}

	encoded, err := document.Bytes()
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	if !bytes.Contains(encoded, []byte("# leading package comment")) {
		t.Fatalf("Bytes() = %q; leading comment was lost", encoded)
	}

	reparsed, err := manifest.Parse(encoded)
	if err != nil {
		t.Fatalf("Parse(Bytes()) error = %v", err)
	}
	assertLookup(t, reparsed, "new", "application", "subdomain")
	assertLookup(t, reparsed, true, "application", "future", "enabled")
}

func TestDocumentMutationAndDecode(t *testing.T) {
	t.Parallel()

	document, err := manifest.Parse([]byte("application:\n  image: example:v1\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := document.Set("example.test", "application", "subdomain"); err != nil {
		t.Fatalf("Set(new key) error = %v", err)
	}
	if deleted := document.Delete("application", "image"); !deleted {
		t.Fatal("Delete(existing key) = false; want true")
	}
	if deleted := document.Delete("application", "image"); deleted {
		t.Fatal("Delete(missing key) = true; want false")
	}

	var decoded struct {
		Application struct {
			Image     string `yaml:"image"`
			Subdomain string `yaml:"subdomain"`
		} `yaml:"application"`
	}
	if err := document.Decode(&decoded); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.Application.Image != "" || decoded.Application.Subdomain != "example.test" {
		t.Fatalf("Decode() = %+v; want deleted image and appended subdomain", decoded.Application)
	}
}

func TestDocumentRejectsInvalidDocumentsAndPaths(t *testing.T) {
	t.Parallel()

	invalidInputs := [][]byte{
		nil,
		[]byte("package: one\n---\npackage: two\n"),
		[]byte("package: [\n"),
	}
	for _, input := range invalidInputs {
		_, err := manifest.Parse(input)
		if !errors.Is(err, lpkgo.ErrInvalidManifest) {
			t.Errorf("Parse(%q) error = %v; want INVALID_MANIFEST", input, err)
		}
	}

	document, err := manifest.Parse([]byte("application: scalar\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if _, _, err := document.Lookup("application", "subdomain"); !errors.Is(err, lpkgo.ErrInvalidManifest) {
		t.Errorf("Lookup(non-mapping path) error = %v; want INVALID_MANIFEST", err)
	}
	if err := document.Set("value", "missing", "field"); !errors.Is(err, lpkgo.ErrInvalidManifest) {
		t.Errorf("Set(missing parent) error = %v; want INVALID_MANIFEST", err)
	}
	if err := document.Set("value", ""); !errors.Is(err, lpkgo.ErrInvalidManifest) {
		t.Errorf("Set(empty path component) error = %v; want INVALID_MANIFEST", err)
	}
}

func TestDocumentCloneRetainsIndependentAliases(t *testing.T) {
	t.Parallel()

	document, err := manifest.Parse([]byte(`defaults: &defaults
  enabled: true
copy: *defaults
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	clone := document.Clone()
	if err := clone.Set(false, "defaults", "enabled"); err != nil {
		t.Fatalf("clone.Set() error = %v", err)
	}

	var original, changed map[string]map[string]bool
	if err := document.Decode(&original); err != nil {
		t.Fatalf("original.Decode() error = %v", err)
	}
	if err := clone.Decode(&changed); err != nil {
		t.Fatalf("clone.Decode() error = %v", err)
	}
	if !original["defaults"]["enabled"] || !original["copy"]["enabled"] {
		t.Fatalf("original aliases changed: %#v", original)
	}
	if changed["defaults"]["enabled"] || changed["copy"]["enabled"] {
		t.Fatalf("clone aliases did not share cloned anchor: %#v", changed)
	}
}

func TestDocumentSetPreservesAnchorAliases(t *testing.T) {
	t.Parallel()

	document, err := manifest.Parse([]byte(`settings: &settings
  enabled: true
copy: *settings
copy2: *settings
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := document.Set(map[string]bool{"enabled": false}, "settings"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	reparsed, err := manifest.Parse(mustDocumentBytes(t, document))
	if err != nil {
		t.Fatalf("Parse(Bytes()) error = %v", err)
	}
	var got map[string]map[string]bool
	if err := reparsed.Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got["settings"]["enabled"] || got["copy"]["enabled"] || got["copy2"]["enabled"] {
		t.Fatalf("decoded aliases = %#v; want all enabled=false", got)
	}
}

func TestDocumentDeleteMaterializesReferencedAnchor(t *testing.T) {
	t.Parallel()

	document, err := manifest.Parse([]byte(`settings: &settings
  enabled: true
copy: *settings
copy2: *settings
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if deleted := document.Delete("settings"); !deleted {
		t.Fatal("Delete(settings) = false; want true")
	}

	reparsed, err := manifest.Parse(mustDocumentBytes(t, document))
	if err != nil {
		t.Fatalf("Parse(Bytes()) error = %v", err)
	}
	var got map[string]map[string]bool
	if err := reparsed.Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got["copy"] == nil || !got["copy"]["enabled"] {
		t.Fatalf("decoded document = %#v; want materialized copy.enabled=true", got)
	}
	if got["copy2"] == nil || !got["copy2"]["enabled"] {
		t.Fatalf("decoded document = %#v; want materialized copy2.enabled=true", got)
	}
	if _, exists := got["settings"]; exists {
		t.Fatalf("decoded document = %#v; deleted settings remains", got)
	}
}

func TestDocumentDecodeSanitizesYAMLErrorChain(t *testing.T) {
	t.Parallel()

	const secret = "SENSITIVE_TOKEN_ABC123"
	const secretMarker = "SENSITI"
	document, err := manifest.Parse([]byte("application:\n  background_task: " + secret + "\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	var target manifest.Manifest
	err = document.Decode(&target)
	if !errors.Is(err, lpkgo.ErrInvalidManifest) {
		t.Fatalf("Decode() error = %v; want INVALID_MANIFEST", err)
	}
	if errors.Unwrap(err) == nil {
		t.Fatal("Decode() error has no safe cause")
	}
	for current := err; current != nil; current = errors.Unwrap(current) {
		if strings.Contains(current.Error(), secretMarker) {
			t.Fatalf("error chain leaked sensitive scalar %q through %T: %q", secret, current, current.Error())
		}
	}
}

func assertLookup(t *testing.T, document *manifest.Document, want any, path ...string) {
	t.Helper()

	got, found, err := document.Lookup(path...)
	if err != nil {
		t.Fatalf("Lookup(%q) error = %v", path, err)
	}
	if !found || got != want {
		t.Fatalf("Lookup(%q) = %#v, %v; want %#v, true", path, got, found, want)
	}
}

func mustDocumentBytes(t *testing.T, document *manifest.Document) []byte {
	t.Helper()

	data, err := document.Bytes()
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	return data
}
