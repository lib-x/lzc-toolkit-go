package manifest_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/manifest"
)

func TestPreprocessSelectsProfileBranch(t *testing.T) {
	t.Parallel()

	input := []byte("before\n#@build if profile = release\nrelease\n#@build else\ndevelopment\n#@build end\nafter\n")
	got, err := manifest.Preprocess(context.Background(), "manifest.yml", input, manifest.BuildContext{Profile: "release"}, fstest.MapFS{})
	if err != nil {
		t.Fatalf("Preprocess() error = %v", err)
	}
	if want := "before\nrelease\nafter\n"; string(got) != want {
		t.Fatalf("Preprocess() = %q; want %q", got, want)
	}
}

func TestPreprocessRequiresLiveContext(t *testing.T) {
	t.Parallel()

	if _, err := manifest.Preprocess(nil, "manifest.yml", []byte("value\n"), manifest.BuildContext{}, fstest.MapFS{}); !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("Preprocess(nil context) error = %v; want INVALID_ARGUMENT", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manifest.Preprocess(cancelled, "manifest.yml", []byte("#@build include must-not-read.yml\n"), manifest.BuildContext{}, panicOpenFS{}); !errors.Is(err, context.Canceled) || !errors.Is(err, lpkgo.ErrCancelled) {
		t.Fatalf("Preprocess(cancelled) error = %v; want context.Canceled and CANCELLED", err)
	}
}

func TestPreprocessNestsConditionsWithoutActivatingAnInactiveParent(t *testing.T) {
	t.Parallel()

	input := []byte("#@build if profile = disabled\nouter\n#@build if env.INNER = yes\nwrong-inner\n#@build else\nwrong-fallback\n#@build end\n#@build else\nselected\n#@build end\n")
	got, err := manifest.Preprocess(context.Background(), "manifest.yml", input, manifest.BuildContext{
		Profile: "release",
		Env:     map[string]string{"INNER": "yes"},
	}, fstest.MapFS{})
	if err != nil {
		t.Fatalf("Preprocess() error = %v", err)
	}
	if want := "selected\n"; string(got) != want {
		t.Fatalf("Preprocess() = %q; want %q", got, want)
	}
}

func TestPreprocessEvaluatesConditionGrammar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		condition string
		context   manifest.BuildContext
		want      string
	}{
		{name: "profile equality", condition: `profile = "release candidate"`, context: manifest.BuildContext{Profile: "release candidate"}, want: "selected\n"},
		{name: "profile inequality", condition: "profile != release", context: manifest.BuildContext{Profile: "debug"}, want: "selected\n"},
		{name: "environment presence", condition: "env.FEATURE", context: manifest.BuildContext{Env: map[string]string{"FEATURE": " yes "}}, want: "selected\n"},
		{name: "empty environment is absent", condition: "env.FEATURE", context: manifest.BuildContext{Env: map[string]string{"FEATURE": "  "}}, want: "fallback\n"},
		{name: "environment equality", condition: `env.CHANNEL = 'nightly build'`, context: manifest.BuildContext{Env: map[string]string{"CHANNEL": "nightly build"}}, want: "selected\n"},
		{name: "environment inequality", condition: "env.CHANNEL != stable", context: manifest.BuildContext{Env: map[string]string{"CHANNEL": "edge"}}, want: "selected\n"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := []byte("#@build if " + test.condition + "\nselected\n#@build else\nfallback\n#@build end\n")
			got, err := manifest.Preprocess(context.Background(), "manifest.yml", input, test.context, fstest.MapFS{})
			if err != nil {
				t.Fatalf("Preprocess() error = %v", err)
			}
			if string(got) != test.want {
				t.Fatalf("Preprocess() = %q; want %q", got, test.want)
			}
		})
	}
}

func TestPreprocessLoadsOnlyActiveIncludesWithDirectiveIndentation(t *testing.T) {
	t.Parallel()

	includes := fstest.MapFS{
		"config/fragments/service.yml": {Data: []byte("image: example:v1\nenvironment:\n  READY: yes\n")},
	}
	input := []byte("services:\n  api:\n    #@build include 'fragments/service.yml'\n#@build if profile = never\n#@build include fragments/missing.yml\n#@build end\n")
	got, err := manifest.Preprocess(context.Background(), "config/manifest.yml", input, manifest.BuildContext{Profile: "release"}, includes)
	if err != nil {
		t.Fatalf("Preprocess() error = %v", err)
	}
	want := "services:\n  api:\n    image: example:v1\n    environment:\n      READY: yes\n\n"
	if string(got) != want {
		t.Fatalf("Preprocess() = %q; want %q", got, want)
	}
}

func TestPreprocessReportsStructuredDirectiveLocations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourceName string
		input      string
		context    manifest.BuildContext
		includes   fstest.MapFS
		wantPath   string
	}{
		{name: "missing include", sourceName: "config/manifest.yml", input: "first\n#@build include absent.yml\n", includes: fstest.MapFS{}, wantPath: "config/manifest.yml:2"},
		{name: "nested directive in include", sourceName: "config/manifest.yml", input: "#@build include parts.yml\n", includes: fstest.MapFS{"config/parts.yml": {Data: []byte("safe\n#@build if profile = release\nsecret\n#@build end\n")}}, wantPath: "config/parts.yml:2"},
		{name: "duplicate else", sourceName: "manifest.yml", input: "#@build if profile = release\na\n#@build else\nb\n#@build else\nc\n#@build end\n", wantPath: "manifest.yml:5"},
		{name: "unmatched else", sourceName: "manifest.yml", input: "value\n#@build else\n", wantPath: "manifest.yml:2"},
		{name: "unmatched end", sourceName: "manifest.yml", input: "value\n#@build end\n", wantPath: "manifest.yml:2"},
		{name: "unclosed if", sourceName: "manifest.yml", input: "value\n#@build if profile = release\nselected\n", wantPath: "manifest.yml:2"},
		{name: "unsupported condition", sourceName: "manifest.yml", input: "#@build if branch = release\n", wantPath: "manifest.yml:1"},
		{name: "unsupported directive", sourceName: "manifest.yml", input: "#@build choose release\n", wantPath: "manifest.yml:1"},
		{name: "empty directive", sourceName: "manifest.yml", input: "value\n#@build\n", wantPath: "manifest.yml:2"},
		{name: "invalid environment key", sourceName: "manifest.yml", input: "value\n", context: manifest.BuildContext{Env: map[string]string{"INVALID-KEY": "secret"}}, wantPath: "manifest.yml:0"},
		{name: "absolute include", sourceName: "manifest.yml", input: "#@build include /etc/passwd\n", wantPath: "manifest.yml:1"},
		{name: "escaping include", sourceName: "config/manifest.yml", input: "#@build include ../../outside.yml\n", wantPath: "config/manifest.yml:1"},
		{name: "backslash include", sourceName: "config/manifest.yml", input: "#@build include '..\\outside.yml'\n", includes: fstest.MapFS{"config/..\\outside.yml": {Data: []byte("must not load\n")}}, wantPath: "config/manifest.yml:1"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := manifest.Preprocess(context.Background(), test.sourceName, []byte(test.input), test.context, test.includes)
			if !errors.Is(err, lpkgo.ErrInvalidManifest) {
				t.Fatalf("Preprocess() error = %v; want INVALID_MANIFEST", err)
			}
			var structured *lpkgo.Error
			if !errors.As(err, &structured) {
				t.Fatalf("Preprocess() error type = %T; want *lpkgo.Error", err)
			}
			if structured.Path != test.wantPath {
				t.Fatalf("Preprocess() error Path = %q; want %q", structured.Path, test.wantPath)
			}
			for current := err; current != nil; current = errors.Unwrap(current) {
				if strings.Contains(current.Error(), "secret") || strings.Contains(current.Error(), "INVALID-KEY") {
					t.Fatalf("error chain leaked directive input through %T: %q", current, current.Error())
				}
			}
		})
	}
}

func TestPreprocessDoesNotCloseCallerFilesystem(t *testing.T) {
	t.Parallel()

	includes := &closeTrackingFS{MapFS: fstest.MapFS{
		"fragment.yml": {Data: []byte("enabled: true\n")},
	}}
	if _, err := manifest.Preprocess(context.Background(), "manifest.yml", []byte("#@build include fragment.yml\n"), manifest.BuildContext{}, includes); err != nil {
		t.Fatalf("Preprocess() error = %v", err)
	}
	if includes.closed {
		t.Fatal("Preprocess() closed the caller-owned filesystem")
	}
}

func TestPreprocessFileLoadsRelativeIncludesAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	filename := filepath.Join(directory, "manifest.yml")
	if err := os.WriteFile(filename, []byte("root:\n  #@build include fragment.yml\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "fragment.yml"), []byte("enabled: true\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(fragment) error = %v", err)
	}

	got, err := manifest.PreprocessFile(context.Background(), filename, manifest.BuildContext{})
	if err != nil {
		t.Fatalf("PreprocessFile() error = %v", err)
	}
	if want := "root:\n  enabled: true\n\n"; string(got) != want {
		t.Fatalf("PreprocessFile() = %q; want %q", got, want)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manifest.PreprocessFile(cancelled, filename, manifest.BuildContext{}); !errors.Is(err, context.Canceled) || !errors.Is(err, lpkgo.ErrCancelled) {
		t.Fatalf("PreprocessFile(cancelled) error = %v; want context.Canceled and CANCELLED", err)
	}
}

func TestPreprocessFileRejectsSymlinkIncludeEscape(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	root := filepath.Join(directory, "root")
	outside := filepath.Join(directory, "outside.yml")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("Mkdir(root) error = %v", err)
	}
	if err := os.WriteFile(outside, []byte("outside: true\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(outside) error = %v", err)
	}
	filename := filepath.Join(root, "manifest.yml")
	if err := os.WriteFile(filename, []byte("#@build include escaped.yml\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escaped.yml")); err != nil {
		t.Skipf("Symlink() unavailable: %v", err)
	}

	_, err := manifest.PreprocessFile(context.Background(), filename, manifest.BuildContext{})
	if !errors.Is(err, lpkgo.ErrInvalidManifest) {
		t.Fatalf("PreprocessFile(symlink escape) error = %v; want INVALID_MANIFEST", err)
	}
}

func TestPreprocessFileRejectsSymlinkSourceEscape(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	root := filepath.Join(directory, "root")
	outside := filepath.Join(directory, "outside.yml")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("Mkdir(root) error = %v", err)
	}
	if err := os.WriteFile(outside, []byte("outside: true\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(outside) error = %v", err)
	}
	filename := filepath.Join(root, "manifest.yml")
	if err := os.Symlink(outside, filename); err != nil {
		t.Skipf("Symlink() unavailable: %v", err)
	}

	_, err := manifest.PreprocessFile(context.Background(), filename, manifest.BuildContext{})
	if !errors.Is(err, lpkgo.ErrInvalidManifest) {
		t.Fatalf("PreprocessFile(symlink source escape) error = %v; want INVALID_MANIFEST", err)
	}
}

func TestPreprocessFileResolvesRelativeFilenameForReadsAndErrors(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	filename := filepath.Join(directory, "manifest.yml")
	if err := os.WriteFile(filename, []byte("#@build end\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	relativeFilename, err := filepath.Rel(workingDirectory, filename)
	if err != nil {
		t.Fatalf("filepath.Rel() error = %v", err)
	}

	_, err = manifest.PreprocessFile(context.Background(), relativeFilename, manifest.BuildContext{})
	var structured *lpkgo.Error
	if !errors.As(err, &structured) {
		t.Fatalf("PreprocessFile(relative) error = %v; want *lpkgo.Error", err)
	}
	wantPath := filepath.ToSlash(filename) + ":1"
	if structured.Path != wantPath {
		t.Fatalf("PreprocessFile(relative) error Path = %q; want %q", structured.Path, wantPath)
	}
}

type closeTrackingFS struct {
	fstest.MapFS
	closed bool
}

type panicOpenFS struct{}

func (panicOpenFS) Open(string) (fs.File, error) {
	panic("pre-cancelled Preprocess read its include filesystem")
}

func (filesystem *closeTrackingFS) Close() error {
	filesystem.closed = true
	return nil
}
