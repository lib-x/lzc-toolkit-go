# Task 6 Report: Manifest preprocessing and compatibility linting

## Status

Complete. The report is included in the requested commit
`feat: preprocess and lint LPK manifests`; the final hash is recorded in the
task handoff because a commit cannot contain its own stable hash.

Task 6 was implemented in `/home/czyt/code/go/lpk-go/.worktrees/lpk-foundation` only. The implementation adds no CLI, process-global state, external tool requirement, or dependency beyond the existing YAML module.

## Files

- Added `manifest/preprocess.go`
- Added `manifest/preprocess_test.go`
- Added `lint/manifest.go`
- Added `lint/manifest_test.go`
- Added `lint/resource.go`
- Added `lint/resource_test.go`
- Updated `docs/superpowers/plans/2026-07-10-lpk-milestone-1-foundation.md`
- Added `.superpowers/sdd/task-6-report.md`

The plan update records both resolved ingestion/API boundaries:

- `BuildContext.Env` is a map, so duplicate raw `KEY=VALUE` entries are deferred to the Milestone 2 build-configuration ingestion boundary;
- `IncludeFS` is the lightweight alias `type IncludeFS = fs.FS` rather than an unused parallel interface.

## Public API

### Manifest preprocessing

```go
type BuildContext struct {
    Profile string
    Env     map[string]string
}

type IncludeFS = fs.FS

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
```

Implemented behavior:

- profile equality/inequality and environment presence/equality/inequality;
- single- and double-quoted directive values;
- nested `if`/`else`/`end` blocks with parent activity preserved;
- include indentation and inactive-include lazy behavior;
- included files reject every build directive;
- absolute, lexical escape, backslash, and `PreprocessFile` symlink-escape includes are rejected;
- `PreprocessFile` checks cancellation before and after source/include reads;
- caller-owned `fs.FS` values are not closed;
- the environment map is copied before evaluation and invalid map keys are rejected.

All directive failures return `INVALID_MANIFEST`. The stable structured location expression is:

```text
lpkgo.Error.Path = "<slash-source-name>:<one-based-line>"
```

Environment-map validation has no source line, so it uses the explicit sentinel location `<source>:0`. Arbitrary directive/YAML input is not placed in `Error()` or in the unwrap cause.

### Manifest compatibility lint

```go
func Manifest(
    source *manifest.Document,
    typed manifest.Manifest,
) ([]lpkgo.Warning, error)
```

The raw `Document` is authoritative for source-level presence; the typed manifest is accepted alongside it for lifecycle callers. The known 2.0.8 schema is private and recursive. The linter emits `SeverityWarning` with these reference codes:

- `unknown-manifest-fields`
- `legacy-static-package-fields`
- `application-handlers-deprecated`
- `application-user-app-deprecated`
- `application-depends-on-deprecated`
- `service-health-check-deprecated`
- `ext-config-http-routing-deprecated`

Warnings follow reference category order. Unknown paths follow private schema traversal order, dynamic service names are sorted, and grouped warning paths use a stable comma-separated expression. Message text remains human-readable but is not the compatibility contract.

### Resource package lint

```go
func ResourcePackage(
    context.Context,
    fs.FS,
) ([]lpkgo.Warning, error)
```

Implemented the reference warning codes for package metadata, exports roots, kind limits, kind/ID naming and directory requirements, empty kinds, and empty payloads. Visible directory entries use slash paths and deterministic `fs.ReadDir` ordering. Dot-prefixed kinds/resources are ignored. Payload traversal uses `fs.WalkDir` and stops at the first regular payload file.

## TDD record

Implementation proceeded through public-interface vertical slices.

1. Profile branch tracer bullet.
   - RED: `go test ./manifest -run '^TestPreprocessSelectsProfileBranch$'`
   - Result: build failed because `manifest.Preprocess` and `manifest.BuildContext` were undefined.
   - GREEN: added the initial public preprocessing surface and branch evaluator; the focused test passed.
2. Full condition grammar.
   - RED: `go test ./manifest -run '^TestPreprocessEvaluatesConditionGrammar$'`
   - Result: quoted profile and all environment cases failed.
   - GREEN: added the reference profile/env grammar and quote normalization; the focused test passed.
3. Include rendering.
   - RED: `go test ./manifest -run '^TestPreprocessLoadsOnlyActiveIncludesWithDirectiveIndentation$'`
   - Result: the include directive remained in output.
   - GREEN: added active include loading, slash resolution, indentation, and inactive include skipping; the focused test passed.
4. Directive state/error matrix.
   - RED: `go test ./manifest -run '^TestPreprocessReportsStructuredDirectiveLocations$'`
   - Result: included directives, duplicate/unmatched/unclosed blocks, and invalid environment keys were not rejected.
   - GREEN: added stack-based processing and sanitized structured `INVALID_MANIFEST` errors; the focused test passed.
5. Manifest lint tracer.
   - RED: `go test ./lint -run '^TestManifestReportsCompatibilityWarningsInStableOrder$'`
   - Result: `lint` had no non-test implementation.
   - GREEN: added the private recursive 2.0.8 schema and compatibility warning collectors; the focused test passed.
6. Resource lint tracer and metadata.
   - RED: `go test ./lint -run '^TestResourcePackageReportsMissingRequiredRoots$'` failed because `lint.ResourcePackage` was undefined.
   - GREEN: added the public resource lint surface and root checks.
   - RED: `go test ./lint -run '^TestResourcePackageValidatesPackageMetadata$'` returned no metadata warnings.
   - GREEN: added package parsing, package-name validation, and version validation.
7. Export tree traversal.
   - RED: `go test ./lint -run '^TestResourcePackageValidatesVisibleExportEntries$'` returned no export warnings.
   - GREEN: added visible-entry traversal, naming/directory checks, kind limit, and first-regular-file payload discovery.
8. Review hardening.
   - RED: `go test ./lint -run '^TestResourcePackageRejectsNilFilesystem$' -count=1` reproduced a nil-filesystem panic.
   - GREEN: nil filesystems now return `INVALID_ARGUMENT`.
   - RED: the backslash include and `PreprocessFile` symlink-escape focused tests both accepted escaped content.
   - GREEN: slash-only names are enforced and `PreprocessFile` uses `os.Root` for rooted include access.

## Verification

Fresh verification before report creation:

```text
gofmt -w manifest lint

go test ./manifest ./lint -count=1
ok github.com/lib-x/lpk-go/manifest 0.003s
ok github.com/lib-x/lpk-go/lint     0.014s

go test ./... -count=1
ok github.com/lib-x/lpk-go
ok github.com/lib-x/lpk-go/archive
ok github.com/lib-x/lpk-go/lint
ok github.com/lib-x/lpk-go/manifest
ok github.com/lib-x/lpk-go/version
ok github.com/lib-x/lpk-go/workflow

go test -race ./... -count=1
ok github.com/lib-x/lpk-go
ok github.com/lib-x/lpk-go/archive
ok github.com/lib-x/lpk-go/lint
ok github.com/lib-x/lpk-go/manifest
ok github.com/lib-x/lpk-go/version
ok github.com/lib-x/lpk-go/workflow

go vet ./...
exit 0

git diff --check
exit 0
```

## Self-review

- Scope is on target: every changed source/test file maps directly to Task 6; the only documentation change is the requested plan correction.
- No public YAML implementation type escapes `manifest.Document`; linting uses `Document.Decode` only.
- No new Go dependency was added.
- Directive errors are code-only in `Error()` and use constant safe causes; source/line remain available through `errors.As` and `lpkgo.Error.Path`.
- Include resolution uses `path`, `fs.ValidPath`, slash-only input, lexical root checks, and `os.Root` for `PreprocessFile` symlink containment.
- Inactive includes are skipped before any path resolution or filesystem read.
- Caller-provided filesystem values are never closed; the internally created `os.Root` is owned and closed by `PreprocessFile`.
- Manifest warning ordering and all machine-consumed code/path fields are deterministic.
- Resource lint ignores hidden entries at the correct two levels and still counts invalid visible entries toward the 100-kind limit, matching the reference.
- Resource payload traversal does not follow symlink directories and stops on the first regular file.
- Context cancellation is represented by both `context.Canceled` and `lpkgo.ErrCancelled`.

Review sign-off:

```text
files changed:    8 task files/docs
scope:            on target
review depth:     deep
hard stops:       3 found, 3 fixed, 0 deferred
specialists:      security, architecture, adversarial (sequential local review)
new tests:        13 top-level tests plus condition/error subtests
doc debt:         none
verification:     focused, full, race, vet, diff-check -> pass
```

## Concerns

No blocking concerns. The selected aggregate `Warning.Path` expression is deliberately documented and tested because the reference CLI has one warning per category rather than one warning per individual field.
