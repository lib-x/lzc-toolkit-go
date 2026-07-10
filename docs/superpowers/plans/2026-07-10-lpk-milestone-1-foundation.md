# LPK Milestone 1 Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** Build the dependency-light Go foundation that can stream-write, stream-read, safely extract, inspect, lint, sign, verify, and repack LPK v1 and v2 packages compatible with @lazycatcloud/lzc-cli@2.0.8.

**Architecture:** Keep the root lpkgo package limited to shared contracts. Pure format packages depend only on the Go standard library and go.yaml.in/yaml/v3. The lpk package composes archive and manifest without importing Docker, gRPC, SSH, App Store, project synchronization, or future lifecycle adapters.

**Tech Stack:** Go 1.25+, standard library archive/zip, archive/tar, crypto/ed25519, crypto/sha256, encoding/json, io/fs, os.Root, and go.yaml.in/yaml/v3 v3.0.4.

## Global Constraints

- Module path is github.com/lib-x/lpk-go.
- Declared Go language version is 1.25.0 because safe extraction requires
  os.Root.Chmod, os.Root.Link, os.Root.MkdirAll, and os.Root.Symlink; do not
  substitute host-path or platform-specific mutation fallbacks.
- Reference package is @lazycatcloud/lzc-cli@2.0.8.
- Reference integrity is sha512-CcH18fg1SBqTN4od7NCXMWYaAwjICgEuguphgNcb9Lp7v5+RDYa27+BEevC7faFFm8Zhjw3Rh/sinYc7fc39SA==.
- Reference shasum is af9fece8a9756a00e093f817b3c3083971cc171f.
- Root package lpkgo must not import build, image, appstore, remote, project, gRPC, SSH, or Docker packages.
- LPK construction must accept io.Writer and must not close it.
- LPK parsing must accept io.Reader and must not close it.
- io.ReaderAt plus a known size must avoid temporary spooling.
- File output helpers must use atomic replacement.
- Caller-owned readers and writers are never closed.
- Every blocking operation accepts context.Context.
- Public errors use stable machine-readable codes.
- No package may prompt, call os.Exit, or mutate global logging state.
- Passwords, tokens, private keys, and authorization headers never appear in errors or events.
- Ordinary go test ./... must not require Node.js, Docker, SSH, a LazyCat account, or network access.
- Each task is committed only after its focused tests pass.

---

### Task 1: Module, shared contracts, and compatibility version

**Files:**

- Create: go.mod
- Create: errors.go
- Create: errors_test.go
- Create: warning.go
- Create: version/version.go
- Create: version/version_test.go
- Modify: docs/superpowers/specs/2026-07-10-lpk-lifecycle-library-design.md

**Interfaces:**

- Produces lpkgo.Code, lpkgo.Error, lpkgo.Warning, lpkgo.Severity.
- Produces version.Current() version.Info.
- No production package produced here imports any later milestone package.

- [ ] **Step 1: Write failing shared-error tests**

Create errors_test.go with this content:

    package lpkgo

    import (
        "errors"
        "fmt"
        "testing"
    )

    func TestErrorMatchesStableCode(t *testing.T) {
        cause := fmt.Errorf("disk failed")
        err := &Error{Code: CodeIntegrityMismatch, Op: "archive.read", Path: "app.lpk", Cause: cause}

        if !errors.Is(err, ErrIntegrityMismatch) {
            t.Fatal("expected errors.Is to match the stable code")
        }
        if !errors.Is(err, cause) {
            t.Fatal("expected errors.Is to reach the wrapped cause")
        }
        if got := err.Error(); got != "INTEGRITY_MISMATCH" {
            t.Fatalf("unexpected error string: %q", got)
        }
    }

    func TestErrorDoesNotExposeSensitiveDetails(t *testing.T) {
        err := &Error{
            Code:  CodeUnauthenticated,
            Op:    "auth.login",
            Stage: "token=stage-secret",
            Path:  "/tmp/password=path-secret",
            Cause: errors.New("private-key=cause-secret"),
        }

        if got := err.Error(); got != "UNAUTHENTICATED" {
            t.Fatalf("unexpected error string: %q", got)
        }
    }

    func TestErrorDoesNotExposeSensitiveOperation(t *testing.T) {
        err := &Error{Code: CodePermissionDenied, Op: "token=operation-secret"}

        if got := err.Error(); got != "PERMISSION_DENIED" {
            t.Fatalf("unexpected error string: %q", got)
        }
    }

    func TestErrorWithoutCause(t *testing.T) {
        err := &Error{Code: CodeInvalidArgument, Op: "lpk.write"}
        if got := err.Error(); got != "INVALID_ARGUMENT" {
            t.Fatalf("unexpected error string: %q", got)
        }
    }

    func TestErrorDoesNotMatchTypedNilTarget(t *testing.T) {
        err := &Error{Code: CodeNotFound}
        var target *Error

        if errors.Is(err, target) {
            t.Fatal("expected a typed-nil target not to match")
        }
    }

    func TestErrorDoesNotMatchWrappedTarget(t *testing.T) {
        err := &Error{Code: CodeNotFound}
        target := fmt.Errorf("wrapped target: %w", ErrNotFound)

        if errors.Is(err, target) {
            t.Fatal("expected only a direct *Error target to match")
        }
    }

- [ ] **Step 2: Run the focused test and confirm it fails**

Run:

    go test .

Expected failure: no go.mod and undefined Error and Code symbols.

- [ ] **Step 3: Initialize the module and implement shared contracts**

Create go.mod:

    module github.com/lib-x/lpk-go

    go 1.25.0

Implement errors.go with:

    package lpkgo

    type Code string

    const (
        CodeInvalidArgument     Code = "INVALID_ARGUMENT"
        CodeInvalidConfig       Code = "INVALID_CONFIG"
        CodeInvalidManifest     Code = "INVALID_MANIFEST"
        CodeUnsupportedFormat   Code = "UNSUPPORTED_FORMAT"
        CodeIncompatibleBackend Code = "INCOMPATIBLE_BACKEND"
        CodeUnauthenticated     Code = "UNAUTHENTICATED"
        CodePermissionDenied    Code = "PERMISSION_DENIED"
        CodeNotFound            Code = "NOT_FOUND"
        CodeConflict            Code = "CONFLICT"
        CodeCommandFailed       Code = "COMMAND_FAILED"
        CodeRemoteUnavailable   Code = "REMOTE_UNAVAILABLE"
        CodeIntegrityMismatch   Code = "INTEGRITY_MISMATCH"
        CodeCancelled           Code = "CANCELLED"
    )

    type Error struct {
        Code       Code
        Op         string
        Stage      string
        Path       string
        StatusCode int
        Retryable  bool
        Cause      error
    }

    func (e *Error) Error() string {
        if e == nil {
            return "<nil>"
        }
        return string(e.Code)
    }

    func (e *Error) Unwrap() error {
        if e == nil {
            return nil
        }
        return e.Cause
    }

    func (e *Error) Is(target error) bool {
        other, ok := target.(*Error)
        return e != nil && ok && other != nil && other.Code != "" && e.Code == other.Code
    }

    func Wrap(code Code, op string, cause error) error {
        if cause == nil {
            return nil
        }
        return &Error{Code: code, Op: op, Cause: cause}
    }

    var (
        ErrInvalidArgument     = &Error{Code: CodeInvalidArgument}
        ErrInvalidConfig       = &Error{Code: CodeInvalidConfig}
        ErrInvalidManifest     = &Error{Code: CodeInvalidManifest}
        ErrUnsupportedFormat   = &Error{Code: CodeUnsupportedFormat}
        ErrIncompatibleBackend = &Error{Code: CodeIncompatibleBackend}
        ErrUnauthenticated     = &Error{Code: CodeUnauthenticated}
        ErrPermissionDenied    = &Error{Code: CodePermissionDenied}
        ErrNotFound            = &Error{Code: CodeNotFound}
        ErrConflict            = &Error{Code: CodeConflict}
        ErrCommandFailed       = &Error{Code: CodeCommandFailed}
        ErrRemoteUnavailable   = &Error{Code: CodeRemoteUnavailable}
        ErrIntegrityMismatch   = &Error{Code: CodeIntegrityMismatch}
        ErrCancelled           = &Error{Code: CodeCancelled}
    )

Implement warning.go:

    package lpkgo

    type Severity string

    const (
        SeverityInfo    Severity = "INFO"
        SeverityWarning Severity = "WARNING"
        SeverityError   Severity = "ERROR"
    )

    type Warning struct {
        Code     string
        Severity Severity
        Path     string
        Message  string
    }

- [ ] **Step 4: Write failing version tests**

Create version/version_test.go:

    package version

    import (
        "slices"
        "testing"
    )

    func TestCurrentReferenceMetadata(t *testing.T) {
        got := Current()
        if got.SDKVersion != "0.1.0" {
            t.Fatalf("SDKVersion = %q", got.SDKVersion)
        }
        if got.ReferencePackage != "@lazycatcloud/lzc-cli" || got.ReferenceVersion != "2.0.8" {
            t.Fatalf("unexpected reference: %#v", got)
        }
        if got.ReferenceIntegrity != "sha512-CcH18fg1SBqTN4od7NCXMWYaAwjICgEuguphgNcb9Lp7v5+RDYa27+BEevC7faFFm8Zhjw3Rh/sinYc7fc39SA==" {
            t.Fatalf("ReferenceIntegrity = %q", got.ReferenceIntegrity)
        }
        if got.ReferenceShasum != "af9fece8a9756a00e093f817b3c3083971cc171f" {
            t.Fatalf("ReferenceShasum = %q", got.ReferenceShasum)
        }
        if !slices.Equal(got.LPKLayouts, []string{"v1", "v2"}) {
            t.Fatalf("LPKLayouts = %#v", got.LPKLayouts)
        }
        if !slices.Equal(got.ArchiveFormats, []string{"zip", "tar"}) {
            t.Fatalf("ArchiveFormats = %#v", got.ArchiveFormats)
        }
        if got.Backend.LPKV2 != "1.0.0" ||
            got.Backend.PendingSyncDevID != "1.0.4" ||
            got.Backend.BuildPackContextCache != "1.0.4" ||
            got.Backend.BlobManifestTransport != "1.0.5" {
            t.Fatalf("unexpected backend requirements: %#v", got.Backend)
        }
    }

    func TestCurrentReturnsFreshSlices(t *testing.T) {
        first := Current()
        first.LPKLayouts[0] = "mutated-layout"
        first.ArchiveFormats[0] = "mutated-format"

        second := Current()
        if !slices.Equal(second.LPKLayouts, []string{"v1", "v2"}) {
            t.Fatalf("LPKLayouts = %#v", second.LPKLayouts)
        }
        if !slices.Equal(second.ArchiveFormats, []string{"zip", "tar"}) {
            t.Fatalf("ArchiveFormats = %#v", second.ArchiveFormats)
        }
    }

Run:

    go test ./version

Expected failure: Current and Info are undefined.

- [ ] **Step 5: Implement version metadata**

Create version/version.go with constants and immutable copied slices:

    package version

    import "runtime/debug"

    const (
        SDKVersion            = "0.1.0"
        ReferenceCLIPackage   = "@lazycatcloud/lzc-cli"
        ReferenceCLIVersion   = "2.0.8"
        ReferenceCLIIntegrity = "sha512-CcH18fg1SBqTN4od7NCXMWYaAwjICgEuguphgNcb9Lp7v5+RDYa27+BEevC7faFFm8Zhjw3Rh/sinYc7fc39SA=="
        ReferenceCLIShasum    = "af9fece8a9756a00e093f817b3c3083971cc171f"
    )

    type BackendRequirements struct {
        LPKV2                 string
        PendingSyncDevID      string
        BuildPackContextCache string
        BlobManifestTransport string
    }

    type Info struct {
        SDKVersion       string
        ModuleVersion    string
        ReferencePackage string
        ReferenceVersion string
        ReferenceIntegrity string
        ReferenceShasum  string
        LPKLayouts       []string
        ArchiveFormats   []string
        Backend          BackendRequirements
    }

    func Current() Info {
        moduleVersion := ""
        if build, ok := debug.ReadBuildInfo(); ok {
            if build.Main.Path == "github.com/lib-x/lpk-go" {
                moduleVersion = build.Main.Version
            }
            for _, dep := range build.Deps {
                if dep.Path == "github.com/lib-x/lpk-go" {
                    moduleVersion = dep.Version
                    break
                }
            }
        }
        return Info{
            SDKVersion: SDKVersion,
            ModuleVersion: moduleVersion,
            ReferencePackage: ReferenceCLIPackage,
            ReferenceVersion: ReferenceCLIVersion,
            ReferenceIntegrity: ReferenceCLIIntegrity,
            ReferenceShasum: ReferenceCLIShasum,
            LPKLayouts: []string{"v1", "v2"},
            ArchiveFormats: []string{"zip", "tar"},
            Backend: BackendRequirements{
                LPKV2: "1.0.0",
                PendingSyncDevID: "1.0.4",
                BuildPackContextCache: "1.0.4",
                BlobManifestTransport: "1.0.5",
            },
        }
    }

- [ ] **Step 6: Format and verify Task 1**

Run:

    go mod tidy
    gofmt -w errors.go errors_test.go warning.go version
    go test ./...
    go vet ./...

Expected: all tests and vet pass.

- [ ] **Step 7: Commit Task 1**

Run:

    git add go.mod errors.go errors_test.go warning.go version docs/superpowers/specs/2026-07-10-lpk-lifecycle-library-design.md docs/superpowers/plans/2026-07-10-lpk-milestone-1-foundation.md
    git commit -m "feat: add SDK contracts and compatibility version"

---

### Task 2: Typed workflow pipeline and observer events

**Files:**

- Create: workflow/workflow.go
- Create: workflow/workflow_test.go

**Interfaces:**

- Produces workflow.Stage, EventKind, Event, Observer, ObserverFunc.
- Produces Step[S], Pipeline[S], NewPipeline[S], and Pipeline.Run.
- Consumes only context and standard library types.

- [ ] **Step 1: Write failing pipeline tests**

Create workflow/workflow_test.go covering ordered execution, cancellation,
and event order:

    package workflow

    import (
        "context"
        "errors"
        "reflect"
        "testing"
    )

    type testState struct{ Values []string }

    type testStep struct {
        name Stage
        value string
    }

    func (s testStep) Name() Stage { return s.name }

    func (s testStep) Run(ctx context.Context, state *testState) error {
        if err := ctx.Err(); err != nil {
            return err
        }
        state.Values = append(state.Values, s.value)
        return nil
    }

    func TestPipelineRunsInOrder(t *testing.T) {
        state := &testState{}
        var kinds []EventKind
        p := NewPipeline[*testState](
            ObserverFunc(func(_ context.Context, event Event) {
                kinds = append(kinds, event.Kind)
            }),
            testStep{name: "one", value: "1"},
            testStep{name: "two", value: "2"},
        )
        if err := p.Run(t.Context(), state); err != nil {
            t.Fatal(err)
        }
        if !reflect.DeepEqual(state.Values, []string{"1", "2"}) {
            t.Fatalf("values = %#v", state.Values)
        }
        wantKinds := []EventKind{EventStarted, EventCompleted, EventStarted, EventCompleted}
        if !reflect.DeepEqual(kinds, wantKinds) {
            t.Fatalf("events = %#v", kinds)
        }
    }

    func TestPipelineStopsOnCancelledContext(t *testing.T) {
        ctx, cancel := context.WithCancel(t.Context())
        cancel()
        p := NewPipeline[*testState](nil, testStep{name: "one", value: "1"})
        err := p.Run(ctx, &testState{})
        if !errors.Is(err, context.Canceled) {
            t.Fatalf("error = %v", err)
        }
    }

- [ ] **Step 2: Run the test and confirm it fails**

Run:

    go test ./workflow

Expected failure: workflow package symbols are undefined.

- [ ] **Step 3: Implement the typed pipeline**

Implement workflow/workflow.go with:

    package workflow

    import (
        "context"
        "time"
    )

    type Stage string
    type EventKind string

    const (
        EventStarted   EventKind = "STARTED"
        EventProgress  EventKind = "PROGRESS"
        EventCompleted EventKind = "COMPLETED"
        EventFailed    EventKind = "FAILED"
    )

    type Event struct {
        Stage      Stage
        Kind       EventKind
        Operation  string
        Message    string
        Current    int64
        Total      int64
        Attributes map[string]string
        Time       time.Time
    }

    type Observer interface {
        OnEvent(context.Context, Event)
    }

    type ObserverFunc func(context.Context, Event)

    func (f ObserverFunc) OnEvent(ctx context.Context, event Event) {
        if f != nil {
            f(ctx, event)
        }
    }

    type Step[S any] interface {
        Name() Stage
        Run(context.Context, S) error
    }

    type Pipeline[S any] struct {
        observer Observer
        steps    []Step[S]
        now      func() time.Time
    }

    func NewPipeline[S any](observer Observer, steps ...Step[S]) *Pipeline[S] {
        copied := append([]Step[S](nil), steps...)
        return &Pipeline[S]{observer: observer, steps: copied, now: time.Now}
    }

    func (p *Pipeline[S]) Run(ctx context.Context, state S) error {
        for _, step := range p.steps {
            if err := ctx.Err(); err != nil {
                return err
            }
            p.emit(ctx, Event{Stage: step.Name(), Kind: EventStarted})
            if err := step.Run(ctx, state); err != nil {
                p.emit(ctx, Event{Stage: step.Name(), Kind: EventFailed, Message: "stage failed"})
                return err
            }
            p.emit(ctx, Event{Stage: step.Name(), Kind: EventCompleted})
        }
        return nil
    }

    func (p *Pipeline[S]) emit(ctx context.Context, event Event) {
        if p.observer == nil {
            return
        }
        event.Time = p.now()
        if event.Attributes != nil {
            event.Attributes = cloneAttributes(event.Attributes)
        }
        p.observer.OnEvent(ctx, event)
    }

    func cloneAttributes(input map[string]string) map[string]string {
        output := make(map[string]string, len(input))
        for key, value := range input {
            output[key] = value
        }
        return output
    }

- [ ] **Step 4: Verify and commit Task 2**

Run:

    gofmt -w workflow
    go test ./workflow
    go test ./...

Expected: all tests pass.

Commit:

    git add workflow
    git commit -m "feat: add typed lifecycle workflow pipeline"

---

### Task 3: Streaming archive reader and deterministic writer

**Files:**

- Create: archive/format.go
- Create: archive/limits.go
- Create: archive/types.go
- Create: archive/reader.go
- Create: archive/writer.go
- Create: archive/reader_test.go
- Create: archive/writer_test.go

**Interfaces:**

- Produces archive.FormatZip and archive.FormatTar.
- Produces Limits, DefaultLimits, Entry, Reader, Open, OpenReaderAt, OpenFile.
- Produces OpenOption, WithLimits, WithTempDir, and WithFilenameHint.
- Produces Write, WriteOptions, WriteResult.
- Does not extract to the filesystem yet; Task 4 adds extraction.

- [ ] **Step 1: Write failing Reader and ReaderAt tests**

The tests must:

- build a small ZIP and TAR in memory;
- call Open with a non-seekable bytes.Buffer;
- call OpenReaderAt with bytes.Reader and its exact size;
- verify Format, Entries, and OpenEntry;
- verify the caller reader remains usable and is never closed;
- reject input larger than Limits.MaxInputBytes.

Use a close-tracking reader:

    type trackingReader struct {
        io.Reader
        closed bool
    }

    func (r *trackingReader) Close() error {
        r.closed = true
        return nil
    }

Assert trackingReader.closed remains false after archive.Reader.Close.

- [ ] **Step 2: Run focused archive tests and confirm failure**

Run:

    go test ./archive -run 'TestOpen|TestOpenReaderAt'

Expected failure: package and API do not exist.

- [ ] **Step 3: Implement formats, limits, and entry contracts**

Implement:

    package archive

    type Format string

    const (
        FormatZIP Format = "zip"
        FormatTAR Format = "tar"
    )

    type Limits struct {
        MaxInputBytes     int64
        MaxEntries        int
        MaxFileBytes      int64
        MaxTotalBytes     int64
        MaxPathBytes      int
        MaxDocumentBytes  int64
    }

    func DefaultLimits() Limits {
        return Limits{
            MaxInputBytes: 32 << 30,
            MaxEntries: 1_000_000,
            MaxFileBytes: 16 << 30,
            MaxTotalBytes: 64 << 30,
            MaxPathBytes: 4096,
            MaxDocumentBytes: 16 << 20,
        }
    }

    type EntryType string

    const (
        EntryDirectory EntryType = "directory"
        EntryRegular   EntryType = "regular"
        EntrySymlink   EntryType = "symlink"
        EntryHardlink  EntryType = "hardlink"
    )

    type Entry struct {
        Name     string
        Type     EntryType
        Mode     fs.FileMode
        Size     int64
        Linkname string
        ModTime  time.Time
    }

Normalize zero-valued limits by replacing each zero field with its default.
Reject negative values as lpkgo.CodeInvalidArgument.

- [ ] **Step 4: Implement Reader ownership and bounded spooling**

Reader stores Format, io.ReaderAt, size, limits, and an optional cleanup
function. Open copies the input into os.CreateTemp using a context-aware
limited reader and rejects MaxInputBytes overflow. OpenReaderAt retains the
caller reader without closing it. OpenFile checks context before any
filesystem I/O, after os.Open, immediately before Stat, and after Stat;
it owns and closes its os.File on every cancellation and error path.

Expose:

    type OpenOption func(*openConfig)

    func WithLimits(Limits) OpenOption
    func WithTempDir(string) OpenOption
    func WithFilenameHint(string) OpenOption

    func (r *Reader) Format() Format
    func (r *Reader) Size() int64
    func (r *Reader) Close() error

Format detection rules:

- first two bytes PK select ZIP;
- otherwise validate that TAR iteration can read its first header;
- invalid data returns UNSUPPORTED_FORMAT.

Entries reparses the archive on each call, sorts ZIP entries by archive order,
enforces entry and size limits, normalizes slash-separated names, and rejects
duplicate names. OpenEntry returns only regular-file content and checks
context before and during reads.

- [ ] **Step 5: Write failing deterministic writer tests**

The tests must call Write twice with the same fstest.MapFS and assert:

- byte-for-byte equality for ZIP;
- byte-for-byte equality for TAR;
- ZIP timestamps equal 1980-01-01 UTC;
- TAR uid, gid, uname, and gname are normalized;
- WriteResult.Size equals buffer length;
- WriteResult.SHA256 equals sha256.Sum256(buffer.Bytes());
- the caller writer is not closed.

- [ ] **Step 6: Implement Write**

Expose:

    type WriteOptions struct {
        Format       Format
        Reproducible bool
    }

    type WriteResult struct {
        Format Format
        Size   int64
        SHA256 [32]byte
    }

    func Write(
        ctx context.Context,
        dst io.Writer,
        source fs.FS,
        options WriteOptions,
    ) (WriteResult, error)

Walk source with fs.WalkDir, reject non-local names, collect and sort paths,
and write through io.MultiWriter(dst, sha256.Hash, countingWriter).

ZIP rules:

- directories end in slash;
- reproducible timestamp is 1980-01-01 UTC;
- regular files use deflate;
- mode is retained without platform ownership metadata.

TAR rules:

- slash paths;
- uid and gid zero;
- uname and gname empty;
- access and change times zero;
- reproducible modification time is Unix epoch;
- only directories and regular files from fs.FS are emitted.

Context cancellation must stop copying and return context.Canceled. All ZIP
and TAR output, including ZIP central-directory and TAR trailer finalization
performed by Close, must pass through a writer that checks context before and
after every underlying Write. Recheck context after archive Close before
returning success. If ZIP CreateHeader or TAR WriteHeader returns after
context cancellation, preserve CodeCancelled before applying any generic
CodeCommandFailed wrapper; non-cancellation header failures remain
CodeCommandFailed.

- [ ] **Step 7: Verify and commit Task 3**

Run:

    gofmt -w archive
    go test ./archive
    go test ./...

Expected: all tests pass without temporary files left in the test temp
directory.

Commit:

    git add archive
    git commit -m "feat: add streaming archive reader and writer"

---

### Task 4: Safe extraction and atomic file output

**Files:**

- Create: archive/extract.go
- Create: archive/extract_test.go
- Create: archive/atomic.go
- Create: archive/atomic_test.go

**Interfaces:**

- Adds Reader.Extract(context.Context, string) error.
- Adds Reader.ExtractSelected(context.Context, string, []string) error.
- Adds WriteFileAtomic(context.Context, string, fs.FS, WriteOptions).

- [ ] **Step 1: Write malicious archive tests**

Create table-driven ZIP and TAR cases for:

- ../escape;
- /absolute;
- backslash traversal;
- NUL in path;
- symlink whose target escapes the destination;
- hardlink whose target escapes the destination;
- regular file written through an earlier escaping symlink;
- duplicate path with a changed file type;
- device, FIFO, and unsupported special entry;
- too many entries;
- single file too large;
- total extracted size too large.

Each test extracts into t.TempDir and asserts no file appears outside the
destination.

- [ ] **Step 2: Run extraction tests and confirm failure**

Run:

    go test ./archive -run 'TestExtract'

Expected failure: Extract methods are undefined.

- [ ] **Step 3: Implement path and link validation**

Use path.Clean on slash-separated archive names after replacing backslashes
with slashes. Reject empty names except the root directory, absolute names,
NUL, volume-like prefixes, cleaned dot-dot traversal, and paths longer than
MaxPathBytes.

Resolve symlink targets relative to path.Dir(entry.Name). Reject absolute
targets and any cleaned result outside the archive root.

Use os.OpenRoot on the destination. Create directories with Root.MkdirAll,
regular files with Root.OpenFile, links with Root.Symlink or Root.Link, and
never construct a writable host path by joining untrusted entry names.

Copy regular files through a context-aware reader, verify the exact declared
size, apply the permission bits masked to 0777, and reject unsupported entry
types.

- [ ] **Step 4: Write atomic output tests**

Tests must verify:

- a failed writer leaves an existing destination unchanged;
- a successful write atomically replaces the destination;
- temporary files are removed;
- destination parent directories are created;
- returned size and digest match the final file.

- [ ] **Step 5: Implement atomic file output**

WriteFileAtomic creates a temporary file in the destination directory, calls
Write, syncs and closes the temporary file, chmods it to 0644, and renames it
over the final destination. Every error path closes and removes the temporary
file. Context cancellation before rename leaves the original destination
unchanged.

- [ ] **Step 6: Verify and commit Task 4**

Run:

    gofmt -w archive
    go test ./archive
    go test ./...

Commit:

    git add archive
    git commit -m "feat: harden archive extraction and atomic output"

---

### Task 5: YAML document, manifest types, and package.yml semantics

**Files:**

- Create: manifest/document.go
- Create: manifest/document_test.go
- Create: manifest/types.go
- Create: manifest/package_info.go
- Create: manifest/package_info_test.go
- Modify: go.mod
- Create: go.sum

**Interfaces:**

- Produces Document.Parse, Decode, Bytes, Lookup, Set, Delete, Clone.
- Produces Manifest, PackageInfo, Application, Service, ExtConfig.
- Produces LoadEffective and SplitEffective.
- YAML node implementation remains private.

- [ ] **Step 1: Write failing document preservation tests**

Use YAML containing comments, known fields, and an unknown nested field.
Parse, Set application.subdomain, marshal, parse again, and assert:

- the unknown field still exists;
- the leading comment remains;
- subdomain changed;
- the original byte slice was not retained by reference;
- Clone mutations do not affect the original.

- [ ] **Step 2: Run manifest document tests and confirm failure**

Run:

    go test ./manifest -run 'TestDocument'

Expected failure: manifest package does not exist.

- [ ] **Step 3: Implement Document**

Add the pinned YAML dependency:

    go get go.yaml.in/yaml/v3@v3.0.4

Document stores an unexported yaml.Node. Parse uses yaml.Decoder with one
document and rejects trailing non-empty YAML documents. Bytes uses
yaml.Encoder with indent 2. Lookup walks mapping nodes by string key. Set
encodes the supplied value into a node and replaces or appends the target.
Delete removes a key/value pair. Clone deep-copies every node, including
comments, anchors, tags, values, aliases, and children.

Expose:

    func Parse(data []byte) (*Document, error)
    func (d *Document) Decode(target any) error
    func (d *Document) Bytes() ([]byte, error)
    func (d *Document) Lookup(path ...string) (any, bool, error)
    func (d *Document) Set(value any, path ...string) error
    func (d *Document) Delete(path ...string) bool
    func (d *Document) Clone() *Document

All parse and path errors use INVALID_MANIFEST.

- [ ] **Step 4: Define typed manifest contracts**

Define PackageInfo with YAML fields package, version, name, description,
author, license, homepage, min_os_version, unsupported_platforms, and
locales. Define Manifest with the static fields for v1 plus Usage,
ExtConfig, Application, and Services.

Application includes typed fields used by the 2.0.8 schema: image,
background_task, subdomain, secondary_domains, multi_instance, accelerator
flags, entries, routes, upstreams, injects, public_path, workdir, ingress,
environment, health_check, oidc_redirect_path, handlers, user_app, and
depends_on.

Service includes init, image, environment, entrypoint, command, tmpfs,
depends_on, healthcheck, health_check, user, CPU and memory controls,
network_mode, netadmin, setup_script, binds, and runtime.

Flexible fields whose upstream schema accepts multiple YAML shapes use any
inside the typed containing struct. Unknown fields remain in Document and
are not represented by an exported catch-all map.

- [ ] **Step 5: Write failing package metadata tests**

Cover:

- effective v1 manifest without package.yml;
- v2 manifest plus package.yml override;
- strict rejection when static fields remain in a v2 manifest;
- split moves all static fields to PackageInfo;
- removed fields are omitted;
- source documents remain unchanged.

- [ ] **Step 6: Implement package.yml semantics**

Expose a function that returns a defensive copy:

    func StaticPackageFields() []string

    type Effective struct {
        Manifest    Manifest
        Source      *Document
        PackageInfo *PackageInfo
        HasPackageFile bool
    }

    func LoadEffective(
        manifestDocument *Document,
        packageDocument *Document,
        strictStaticFields bool,
    ) (Effective, error)

    func SplitEffective(
        source *Document,
        packageInfo *PackageInfo,
        removedFields []string,
    ) (*Document, *Document, error)

Use cloned documents, never mutate caller documents, and preserve unknown
fields.

- [ ] **Step 7: Verify and commit Task 5**

Run:

    go mod tidy
    gofmt -w manifest
    go test ./manifest
    go test ./...

Commit:

    git add go.mod go.sum manifest
    git commit -m "feat: add manifest documents and package metadata"

---

### Task 6: Manifest build directives and compatibility linting

**Files:**

- Create: manifest/preprocess.go
- Create: manifest/preprocess_test.go
- Create: lint/manifest.go
- Create: lint/manifest_test.go
- Create: lint/resource.go
- Create: lint/resource_test.go

**Interfaces:**

- Produces manifest.Preprocess and PreprocessFile.
- Produces lint.Manifest, lint.ResourcePackage, and stable warning codes.
- Consumes lpkgo.Warning and manifest types.

- [ ] **Step 1: Write directive matrix tests**

Tests cover:

- profile equality and inequality;
- env presence, equality, and inequality;
- quoted values;
- else branches;
- include indentation;
- inactive includes;
- missing include;
- nested directives in included files;
- duplicate else;
- unmatched else and end;
- unclosed if;
- invalid env key and duplicate env entries;
- exact source filename and line in errors.

- [ ] **Step 2: Implement preprocessing**

Expose:

    type BuildContext struct {
        Profile string
        Env     map[string]string
    }

    type IncludeFS interface {
        ReadFile(string) ([]byte, error)
    }

    func Preprocess(
        sourceName string,
        input []byte,
        context BuildContext,
        includes fs.FS,
    ) ([]byte, error)

    func PreprocessFile(
        context.Context,
        string,
        BuildContext,
    ) ([]byte, error)

Implement the exact #@build if, else, end, and include grammar documented in
the design. Included files are processed with directives disabled. Copy the
environment map before use and sort no externally observable map output.

- [ ] **Step 3: Write manifest lint tests**

Use typed and raw manifest documents to assert warning codes for:

- unknown-manifest-fields;
- legacy-static-package-fields;
- application-handlers-deprecated;
- application-user-app-deprecated;
- application-depends-on-deprecated;
- service-health-check-deprecated;
- ext-config-http-routing-deprecated.

Warnings must have stable order: schema traversal order followed by sorted
dynamic service paths.

- [ ] **Step 4: Implement manifest lint**

Represent the known 2.0.8 schema as private recursive schema nodes. Walk the
Document mapping without losing raw fields. Emit lpkgo.Warning values with
SeverityWarning and stable codes. Keep message text human-readable but test
codes and paths as the stable contract.

- [ ] **Step 5: Write resource package lint tests**

Create temporary package roots covering:

- missing package.yml;
- invalid package name;
- missing version;
- missing or empty exports;
- more than 100 kinds;
- invalid kind and resource ID names;
- non-directory kind or resource entries;
- empty payloads;
- a valid nested resource payload.

- [ ] **Step 6: Implement resource lint**

Expose:

    func ResourcePackage(ctx context.Context, root fs.FS) ([]lpkgo.Warning, error)

Use the exact reference warning codes from lib/lpk/resource_lint.js. Ignore
dot-prefixed entries when counting visible kinds and resources. Traverse
payloads with fs.WalkDir and stop at the first regular payload file.

- [ ] **Step 7: Verify and commit Task 6**

Run:

    gofmt -w manifest lint
    go test ./manifest ./lint
    go test ./...

Commit:

    git add manifest lint
    git commit -m "feat: preprocess and lint LPK manifests"

---

### Task 7: LPK Reader and Writer container API

**Files:**

- Create: lpk/layout.go
- Create: lpk/reader.go
- Create: lpk/writer.go
- Create: lpk/validate.go
- Create: lpk/reader_test.go
- Create: lpk/writer_test.go
- Create: lpk/testdata_test.go

**Interfaces:**

- Produces lpk.LayoutV1, LayoutV2, Write, WriteFile.
- Produces lpk.Open, OpenReaderAt, OpenFile, Reader, Reader.Close.
- Reader wraps archive.Reader and provides package-aware entry access.

- [ ] **Step 1: Write failing Writer API tests**

Build package roots with fstest.MapFS and assert:

- v1 requires manifest.yml and writes ZIP;
- v2 requires package.yml and writes TAR;
- resource-only v2 permits package.yml plus exports without manifest.yml;
- resource-only v1 is rejected;
- a v2 package with static fields in manifest.yml is rejected in strict mode;
- Write accepts bytes.Buffer;
- WriteFile is atomic;
- result reports layout, format, size, and digest.

- [ ] **Step 2: Implement layouts and Writer**

Expose:

    type Layout string

    const (
        LayoutV1 Layout = "v1"
        LayoutV2 Layout = "v2"
    )

    type WriteRequest struct {
        Layout Layout
        Files  fs.FS
        Strict bool
    }

    type WriteResult struct {
        Layout Layout
        Format archive.Format
        Size   int64
        SHA256 [32]byte
    }

    func Write(context.Context, io.Writer, WriteRequest) (WriteResult, error)
    func WriteFile(context.Context, string, WriteRequest) (WriteResult, error)

Validate the complete package root before writing. Delegate encoding to
archive.Write with Reproducible set to true for every LPK output.

- [ ] **Step 3: Write failing Reader tests**

For both layouts and resource-only packages, assert:

- Open accepts a non-seekable reader;
- OpenReaderAt accepts bytes.Reader and size;
- OpenFile accepts a path;
- detected layout and format are correct;
- Entry, OpenEntry, Extract, Manifest, and PackageInfo work;
- reader-owned temporary files disappear after Close;
- caller-provided readers remain open;
- repeated Close is safe.

- [ ] **Step 4: Implement Reader**

Reader owns an archive.Reader and caches parsed manifest and package
documents. Layout detection rules:

- TAR is v2;
- package.yml or images directory also imply v2;
- otherwise ZIP is v1.

Expose:

    func (r *Reader) Layout() Layout
    func (r *Reader) Format() archive.Format
    func (r *Reader) Entries(context.Context) ([]archive.Entry, error)
    func (r *Reader) OpenEntry(context.Context, string) (io.ReadCloser, error)
    func (r *Reader) Extract(context.Context, string) error
    func (r *Reader) Manifest(context.Context) (*manifest.Document, error)
    func (r *Reader) PackageInfo(context.Context) (*manifest.Document, error)
    func (r *Reader) EffectiveManifest(context.Context) (manifest.Effective, error)
    func (r *Reader) Close() error

Resource-only readers return fs.ErrNotExist from Manifest and still return
PackageInfo.

Re-export archive open options without importing adapters:

    type OpenOption = archive.OpenOption

    func WithLimits(archive.Limits) OpenOption
    func WithTempDir(string) OpenOption
    func WithFilenameHint(string) OpenOption

Reader also exposes Size() int64 for stream inspection.

- [ ] **Step 5: Verify import boundaries**

Run:

    go list -deps ./lpk ./manifest ./archive

Assert output does not contain grpc, ssh, docker, appstore, remote, project,
or lifecycle package paths.

- [ ] **Step 6: Verify and commit Task 7**

Run:

    gofmt -w lpk
    go test ./lpk
    go test ./...

Commit:

    git add lpk
    git commit -m "feat: add stream-based LPK reader and writer"

---

### Task 8: Package inspection and image summary

**Files:**

- Create: inspect/types.go
- Create: inspect/inspect.go
- Create: inspect/images.go
- Create: inspect/inspect_test.go
- Create: inspect/images_test.go

**Interfaces:**

- Produces inspect.Reader, Stream, ReaderAt, and File.
- Produces Info, ImageInfo, ImageDetail.
- Consumes lpk.Reader and manifest.Effective.

- [ ] **Step 1: Write inspection fixture tests**

Create in-memory v1, v2, resource-only, signed-marker, and image-bearing
packages. images.lock cases include:

- two aliases;
- duplicate shared embedded digest;
- upstream and embedded layers;
- one missing embedded blob;
- empty upstream for a fully embedded image.

Assert package ID, version, layout, format, signed metadata presence, image
counts, per-alias bytes, unique total bytes, and missing blob counts.

- [ ] **Step 2: Implement typed inspection results**

Define:

    type Info struct {
        Size           int64
        Format         archive.Format
        Layout         lpk.Layout
        Signed         bool
        ResourceOnly   bool
        PackageID      string
        AppVersion     string
        HasManifest    bool
        HasPackageInfo bool
        HasImagesDir   bool
        HasImagesLock  bool
        Images         ImageInfo
    }

    type ImageDetail struct {
        Alias                     string
        ImageID                   string
        Upstream                  string
        EmbeddedLayerCount        int
        UpstreamLayerCount        int
        UniqueEmbeddedLayerCount  int
        EmbeddedBytes             int64
        MissingEmbeddedLayerCount int
    }

    type ImageInfo struct {
        Aliases                        []string
        Details                        []ImageDetail
        TotalEmbeddedLayerCount        int
        TotalEmbeddedBytes             int64
        TotalMissingEmbeddedLayerCount int
    }

- [ ] **Step 3: Implement stream and file inspection**

Expose:

    func Reader(context.Context, *lpk.Reader) (Info, error)
    func Stream(context.Context, io.Reader, ...lpk.OpenOption) (Info, error)
    func ReaderAt(context.Context, io.ReaderAt, int64, ...lpk.OpenOption) (Info, error)
    func File(context.Context, string, ...lpk.OpenOption) (Info, error)

Stream, ReaderAt, and File open and close their internal lpk.Reader. Reader
does not close the caller-owned lpk.Reader.

Parse images.lock with private typed YAML structs. Normalize sha256 digests
to lowercase, sort aliases, count shared embedded blobs once globally, and
read blob sizes from images/blobs/sha256.

Signed means META/release.lock exists or META/signatures contains a .sig
entry. It does not imply cryptographic validity.

- [ ] **Step 4: Verify and commit Task 8**

Run:

    gofmt -w inspect
    go test ./inspect
    go test ./...

Commit:

    git add inspect
    git commit -m "feat: inspect LPK metadata and image layers"

---

### Task 9: Ed25519 signing and verification

**Files:**

- Create: signature/types.go
- Create: signature/key.go
- Create: signature/metadata.go
- Create: signature/sign.go
- Create: signature/verify.go
- Create: signature/key_test.go
- Create: signature/sign_test.go
- Create: signature/verify_test.go

**Interfaces:**

- Produces GenerateKeyPair, Sign, SignFile, Verify, VerifyFile.
- Stream APIs never close caller readers or writers.
- Consumes lpk Reader/Writer and archive limits.

- [ ] **Step 1: Write key generation tests**

Generate into t.TempDir and assert:

- private key filename ends .ed25519.private.pem;
- public key filename ends .ed25519.public.pem;
- private mode is 0600;
- PEM parses as PKCS8 Ed25519 private key;
- PEM parses as SPKI Ed25519 public key;
- existing files fail unless Force is true.

- [ ] **Step 2: Implement key generation**

Expose:

    type GenerateKeyRequest struct {
        Directory string
        Name      string
        Force     bool
    }

    type KeyPair struct {
        PrivateKeyPath string
        PublicKeyPath  string
    }

    func GenerateKeyPair(GenerateKeyRequest) (KeyPair, error)

Use crypto/ed25519.GenerateKey, x509.MarshalPKCS8PrivateKey,
x509.MarshalPKIXPublicKey, pem.Encode, atomic file writes, and private mode
0600.

- [ ] **Step 3: Write stream signing tests**

Sign both v1 and v2 in-memory packages and assert:

- output keeps the original layout and archive format;
- META/release.lock exists;
- release.lock schema, appid, version, sorted paths, sizes, and digests match;
- META/keys/dev.pub exists;
- META/signatures/dev.sig uses ed25519 and signed_file META/release.lock;
- source reader and destination writer are not closed;
- signing an already signed package returns CONFLICT;
- Resign replaces all old META data.

- [ ] **Step 4: Implement signing**

Expose:

    type SignRequest struct {
        PrivateKeyPEM []byte
        PublicKeyPEM  []byte
        KeyID         string
        Resign        bool
        Limits        archive.Limits
    }

    type SignResult struct {
        Layout lpk.Layout
        Format archive.Format
        Size   int64
        SHA256 [32]byte
        KeyID  string
    }

    func Sign(context.Context, io.Writer, io.Reader, SignRequest) (SignResult, error)
    func SignFile(context.Context, string, string, SignRequest) (SignResult, error)

Open and safely extract the source to a temporary root. Hash every regular
file outside META in sorted slash path order. Marshal release.lock with
json.MarshalIndent and no trailing newline. Sign the exact bytes with
ed25519.Sign. Write metadata, then call lpk.Write with the original layout.
SignFile permits identical input and output paths through atomic replacement.

- [ ] **Step 5: Write verification and tamper tests**

Assert successful verification, then separately tamper:

- a listed file;
- file size;
- release.lock object digest;
- signature base64;
- embedded public key;
- key ID;
- signed_file;
- add an unexpected non-META file;
- remove a listed file.

Each tamper case returns INTEGRITY_MISMATCH and a typed VerifyResult with no
false success.

- [ ] **Step 6: Implement verification**

Expose:

    type VerifyRequest struct {
        TrustedPublicKeyPEM []byte
        KeyID               string
        Limits              archive.Limits
    }

    type VerifyResult struct {
        Valid      bool
        KeyID      string
        AppID      string
        Version    string
        ObjectCount int
    }

    func Verify(context.Context, io.Reader, VerifyRequest) (VerifyResult, error)
    func VerifyFile(context.Context, string, VerifyRequest) (VerifyResult, error)

Verify schemas, algorithm, key ID, signed file, exact release.lock signature,
every object path, size, and digest, and the absence of unexpected non-META
regular files. Use the trusted key when supplied; otherwise use the embedded
key.

- [ ] **Step 7: Verify and commit Task 9**

Run:

    gofmt -w signature
    go test ./signature
    go test ./...

Commit:

    git add signature
    git commit -m "feat: sign and verify LPK packages"

---

### Task 10: Upstream fixtures, import checks, CI, and Milestone 1 verification

**Files:**

- Create: testdata/upstream/2.0.8/README.md
- Create: testdata/upstream/2.0.8/v1-simple.lpk
- Create: testdata/upstream/2.0.8/v2-simple.lpk
- Create: testdata/upstream/2.0.8/resource-only.lpk
- Create: testdata/upstream/2.0.8/signed-v2.lpk
- Create: internal/compat/upstream_test.go
- Create: scripts/regenerate-upstream-fixtures.sh
- Create: scripts/check-import-boundaries.sh
- Create: .github/workflows/test.yml
- Create: README.md
- Modify: docs/superpowers/plans/2026-07-10-lpk-milestone-1-foundation.md

**Interfaces:**

- Produces no new public API.
- Locks interoperation with @lazycatcloud/lzc-cli@2.0.8.
- Enforces small-package dependency boundaries.

- [ ] **Step 1: Add the fixture regeneration script**

The script must:

- use set -euo pipefail;
- create a temporary npm project;
- install exactly @lazycatcloud/lzc-cli@2.0.8;
- create minimal v1, v2, and resource-only projects;
- call the installed lzc-cli project build command to build each package;
- generate an Ed25519 key and sign v2 with the reference implementation;
- copy only the four resulting LPK files into testdata/upstream/2.0.8;
- print sha256sum values;
- clean its temporary directory with trap.

Use these exact command forms after npm install:

    npx --no-install lzc-cli project build "$V1_DIR" -f lzc-build.yml -o "$OUT/v1-simple.lpk"
    npx --no-install lzc-cli project build "$V2_DIR" -f lzc-build.yml -o "$OUT/v2-simple.lpk"
    npx --no-install lzc-cli project build "$RESOURCE_DIR" -f lzc-build.yml -o "$OUT/resource-only.lpk"
    npx --no-install lzc-cli sig gen-key "$KEY_DIR" --name upstream
    npx --no-install lzc-cli lpk sign "$OUT/v2-simple.lpk" \
        --private-key "$KEY_DIR/upstream.ed25519.private.pem" \
        --public-key "$KEY_DIR/upstream.ed25519.public.pem" \
        --key-id upstream \
        --output "$OUT/signed-v2.lpk"

README.md records package version, integrity, shasum, generation command, and
fixture sha256 values.

- [ ] **Step 2: Write upstream fixture tests**

internal/compat/upstream_test.go opens every fixture through lpk.Open,
inspects it, lints resource packages where applicable, and verifies the
signed fixture. Tests use repository-relative fixture lookup based on the
test file directory, not the process working directory.

- [ ] **Step 3: Add import boundary enforcement**

scripts/check-import-boundaries.sh runs go list -deps for:

- ./archive
- ./manifest
- ./lpk
- ./inspect
- ./lint
- ./signature

It fails if dependency output contains:

- google.golang.org/grpc;
- golang.org/x/crypto/ssh;
- github.com/docker;
- github.com/lib-x/lpk-go/appstore;
- github.com/lib-x/lpk-go/remote;
- github.com/lib-x/lpk-go/project;
- github.com/lib-x/lpk-go/lifecycle.

- [ ] **Step 4: Add CI**

.github/workflows/test.yml contains:

- a Go unit job on Go 1.25 and Go 1.26;
- go test ./...;
- go vet ./...;
- import boundary script;
- a race job running go test -race ./... on Go 1.26;
- an upstream interop job on Node 20 and Go 1.26 that regenerates fixtures
  into a temporary checkout, runs Go compatibility tests, builds simple Go
  v1 and v2 packages, and runs lzc-cli lpk info and lpk lint on them.

Pin GitHub actions to major stable versions and grant contents: read only.

- [ ] **Step 5: Document Milestone 1 APIs**

README.md includes:

- reference version and semantic compatibility statement;
- import examples showing manifest-only and lpk-only usage;
- bytes.Buffer Write example;
- bytes.Reader Open example with defer reader.Close;
- OpenReaderAt example;
- file helper example;
- signature example;
- package dependency statement;
- security limits statement.

- [ ] **Step 6: Run the complete verification suite**

Run:

    gofmt -w $(find . -type f -name '*.go')
    go mod tidy
    go test ./...
    go test -race ./...
    go vet ./...
    bash scripts/check-import-boundaries.sh
    git diff --check

Expected:

- every command exits zero;
- no temporary fixture directories remain;
- git status contains only intended milestone files;
- no dependency-boundary violations appear.

- [ ] **Step 7: Commit Milestone 1**

Run:

    git add README.md .github scripts testdata internal docs/superpowers/plans/2026-07-10-lpk-milestone-1-foundation.md
    git commit -m "test: verify lzc-cli 2.0.8 format compatibility"

- [ ] **Step 8: Milestone checkpoint**

Record:

- all commit IDs for Tasks 1 through 10;
- go test, race, vet, import-boundary, and interop results;
- any deliberate compatibility differences;
- the clean git status.

Do not push yet. Milestones 2 through 4 will receive separate detailed plans.
The user-requested push occurs after all production code and unit tests for
the complete approved scope pass final verification.
