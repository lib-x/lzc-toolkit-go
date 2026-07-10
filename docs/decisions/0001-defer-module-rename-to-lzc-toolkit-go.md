# ADR-0001: Rename the completed SDK to lzc-toolkit-go

## Status

Accepted — Implemented

## Date

2026-07-10

## Context

The SDK began as an LPK format and build library under the former module path
`github.com/lib-x/lpk-go`. Its implemented scope now also includes project
builds, OCI and Docker image handling, LazyCat account authentication, App
Store publishing, image copying, APK generation, and remote project lifecycle
operations.

Renaming the module during active development would create repeated import-path
churn across production code, tests, generated Protocol Buffer files,
documentation, compatibility fixtures, and downstream examples. The public
scope, however, is already broader than LPK alone, so retaining the original
name after completion would misrepresent the library.

## Decision

Keep `github.com/lib-x/lpk-go` throughout feature development. After all
planned lifecycle functionality is complete and before the first stable SDK
release, perform one atomic repository and module migration to:

`github.com/lib-x/lzc-toolkit-go`

The migration includes the GitHub repository name, `go.mod`, all internal and
documented import paths, generated Protocol Buffer `go_package` values,
examples, CI scripts, compatibility checks, and package metadata. Run the full
test, race, vet, import-boundary, lzc-cli interoperability, and downstream
sample-project validation suites after the rename.

## Implementation

Implemented on 2026-07-10 after Milestone 4 passed its unit, race, vet,
interoperability, import-boundary, and lazycat-contrib validation gates. The
GitHub repository and Go module are now `github.com/lib-x/lzc-toolkit-go`.

## Alternatives Considered

### Rename immediately

This would make the current scope clearer sooner, but every remaining feature
batch would be developed on top of a large mechanical rename and increase the
risk of stale imports or generated-code drift.

### Keep lpk-go permanently

This avoids migration work but gives users the wrong expectation that the SDK
only reads and writes LPK containers.

## Consequences

- Feature development used the former module path until completion.
- No compatibility alias module or dual import path will be maintained before
  the first stable release.
- The final rename is a release gate, not an optional cleanup task.
- Generated ShellAPI Go code was regenerated with the final module path.
