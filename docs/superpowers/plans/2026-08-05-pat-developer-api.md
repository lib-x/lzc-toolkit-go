# PAT Developer API Integration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the LazyCat PAT developer API adapter into `lzc-toolkit-go`, release it, and make `lazycat-github-action` consume the released library instead of its internal implementation.

**Architecture:** Keep the existing `appstore.New` legacy lzc-cli session-token contract unchanged. Add an explicit `appstore.NewPAT` constructor and PAT HTTP client adapter for `/sdk/v3/developer` plus `X-API-Token`, and add official metadata-domain resolution to the anonymous `appstore/official` package. The Action retains only environment precedence and protocol selection, while all PAT request rewriting, response-envelope handling, and official metadata URL construction come from the toolkit.

**Tech Stack:** Go 1.25, `net/http`, `encoding/json`, Go modules, Git tags, GitHub Actions.

## Global Constraints

- `LZC_API_TOKEN` is a PAT for `/sdk/v3/developer` and `X-API-Token`.
- `LAZYCAT_TOKEN` remains the legacy lzc-cli session token for `/api/v3/developer`, `X-User-Token`, and `userToken` cookie.
- PAT wins when both environment variables are present; the values are never copied or synchronized.
- Existing `appstore.New` callers must remain source- and behavior-compatible.
- Preserve `/home/czyt/code/go/lazycat-github-action/docs/superpowers/plans/2026-07-28-lzc-cli-2.0.9-compatibility-and-build-errors.md` unchanged and uncommitted.
- Release `lzc-toolkit-go` as `v0.4.0`, then release the Action integration as `v1.2.1` and update floating `v1` through the existing release workflow.

---

### Task 1: Add the toolkit PAT developer API contract

**Files:**
- Create: `appstore/pat.go`
- Create: `appstore/pat_test.go`
- Modify: `appstore/client.go`

**Interfaces:**
- Consumes: existing `appstore.Options`, `appstore.Client`, and `auth.TokenProvider`.
- Produces: `appstore.DefaultPATBaseURL`, `appstore.ResolvePATBaseURL(string) (string, error)`, `appstore.NewPATHTTPClient(string, *http.Client) (*http.Client, error)`, and `appstore.NewPAT(Options) (*Client, error)`.

- [ ] **Step 1: Write failing PAT contract tests**

Add tests that require the PAT client to rewrite `/api/v3/developer/...` to `/sdk/v3/developer/...`, replace legacy headers and cookies with `X-API-Token`, unwrap `{errorCode,msg,data}`, map non-zero envelope errors to a failed HTTP status, reject unsafe hosts/base URLs, stop redirects, and avoid forwarding PAT headers to another origin.

- [ ] **Step 2: Run the focused tests and confirm the API is missing**

Run: `go test ./appstore -run 'PAT|ResolvePAT' -count=1`

Expected: FAIL because `NewPAT`, `NewPATHTTPClient`, and `ResolvePATBaseURL` do not exist.

- [ ] **Step 3: Implement the bounded PAT adapter**

Implement the additive constructor contract:

```go
const DefaultPATBaseURL = "https://appstore.api.lazycat.cloud"

func ResolvePATBaseURL(configuredHost string) (string, error)
func NewPATHTTPClient(baseURL string, base *http.Client) (*http.Client, error)
func NewPAT(options Options) (*Client, error)
```

The transport must clone each request, rewrite only same-origin developer API requests, strip `X-User-Token` and `Cookie`, set `X-API-Token`, disable redirects, and inspect SDK envelopes with a bounded read while preserving non-envelope responses.

- [ ] **Step 4: Run focused and package tests**

Run: `go test ./appstore -count=1`

Expected: PASS with legacy appstore tests still exercising `X-User-Token` and the new PAT tests exercising `X-API-Token`.

### Task 2: Move official metadata-domain construction into the toolkit

**Files:**
- Create: `appstore/official/config.go`
- Create: `appstore/official/config_test.go`

**Interfaces:**
- Consumes: `official.DefaultMetadataBaseURL`.
- Produces: `official.MetadataBaseURL(string) (string, error)` for a domain-only override.

- [ ] **Step 1: Write failing metadata URL tests**

Cover the empty default, a custom domain, and rejection of schemes, ports, paths, queries, fragments, whitespace, and control characters.

- [ ] **Step 2: Run the focused tests and confirm the helper is missing**

Run: `go test ./appstore/official -run MetadataBaseURL -count=1`

Expected: FAIL because `MetadataBaseURL` does not exist.

- [ ] **Step 3: Implement the domain-only resolver**

Implement:

```go
func MetadataBaseURL(domain string) (string, error)
```

Return `DefaultMetadataBaseURL` for an empty value and otherwise return `https://<domain>/appstore/metarepo` only after strict validation.

- [ ] **Step 4: Run official-package tests**

Run: `go test ./appstore/official -count=1`

Expected: PASS.

### Task 3: Document and release lzc-toolkit-go v0.4.0

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `version/version.go`
- Modify: `version/version_test.go`
- Include: `docs/superpowers/plans/2026-08-05-pat-developer-api.md`

**Interfaces:**
- Consumes: the PAT and metadata APIs from Tasks 1-2.
- Produces: published Go module tag `v0.4.0`.

- [ ] **Step 1: Document explicit legacy and PAT constructors**

Show `appstore.New` with a legacy session token and `appstore.NewPAT` with a PAT, including the route/header distinction and the fact that the library does not synchronize credential values.

- [ ] **Step 2: Set toolkit version metadata to 0.4.0**

Change `version.SDKVersion` and its test expectation from `0.3.5` to `0.4.0`.

- [ ] **Step 3: Run toolkit verification**

Run: `gofmt -w appstore/pat.go appstore/pat_test.go appstore/official/config.go appstore/official/config_test.go`

Run: `go test -race ./...`

Run: `go vet ./...`

Run: `bash scripts/check-import-boundaries.sh`

Expected: every command exits 0.

- [ ] **Step 4: Commit, push, tag, and verify v0.4.0**

Commit message: `feat: add PAT developer API client`

Push `main`, create annotated tag `v0.4.0`, push the tag, wait for GitHub checks, and verify `origin/main` and `refs/tags/v0.4.0^{}` resolve to the release commit.

### Task 4: Replace the Action's internal PAT implementation

**Files:**
- Delete: `internal/platformapi/client.go`
- Delete: `internal/platformapi/client_test.go`
- Modify: `internal/platformauth/resolver.go`
- Modify: `internal/action/action.go`
- Modify: `internal/action/platform_auth_internal_test.go`
- Modify: `internal/publishflow/flow.go`
- Modify: `internal/publishflow/platform_auth_internal_test.go`
- Modify: `internal/store/official/publish.go`
- Modify: `internal/store/official/publish_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: `github.com/lib-x/lzc-toolkit-go v0.4.0`, `appstore.NewPAT`, `appstore.ResolvePATBaseURL`, `appstore.NewPATHTTPClient`, and `official.MetadataBaseURL`.
- Produces: unchanged Action environment precedence and observable authentication behavior without a local PAT transport or envelope decoder.

- [ ] **Step 1: Upgrade the module dependency**

Run: `go get github.com/lib-x/lzc-toolkit-go@v0.4.0`

Expected: `go.mod` requires exactly `v0.4.0` and `go.sum` is updated.

- [ ] **Step 2: Replace local PAT construction with toolkit calls**

Use `appstore.ResolvePATBaseURL` in the resolver, `appstore.NewPAT` for PAT image-copy clients, and `appstore.NewPATHTTPClient` inside the official publisher's SDK branch. Use `appstore/official.MetadataBaseURL` for anonymous metadata lookup.

- [ ] **Step 3: Delete the internal PAT API package and adapt tests**

Remove `internal/platformapi` after `rg` proves no imports remain. Keep tests for PAT precedence, immutable credential snapshots, legacy fallback, redirect blocking, route/header selection, and SDK response handling at the appropriate toolkit or Action integration layer.

- [ ] **Step 4: Run focused Action tests**

Run: `go test ./internal/platformauth ./internal/action ./internal/publishflow ./internal/store/official -count=1`

Expected: PASS.

### Task 5: Verify and release lazycat-github-action v1.2.1

**Files:**
- Modify: `action.yml`
- Modify: `.github/workflows/lazycat.yml`

**Interfaces:**
- Consumes: released toolkit `v0.4.0`.
- Produces: released Action `v1.2.1` and floating tag `v1` at the same commit.

- [ ] **Step 1: Synchronize Action version fields**

Set `LAZYCAT_ACTION_VERSION` and all self-references in `.github/workflows/lazycat.yml` to `v1.2.1`.

- [ ] **Step 2: Run full Action verification**

Run: `go test -race ./...`

Run: `go vet ./...`

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/*.yml examples/*/.github/workflows/*.yml`

Run: `bash -n scripts/run-action.sh scripts/run-action_test.sh examples/*/scripts/*.sh`

Run: `bash scripts/run-action_test.sh`

Run: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/lazycat-action`

Run: `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/lazycat-action`

Expected: every command exits 0.

- [ ] **Step 3: Commit and push only intended Action files**

Commit message: `refactor: use toolkit PAT developer API`

Re-read HEAD and the complete worktree before commit and push; leave the pre-existing untracked July 28 plan untouched.

- [ ] **Step 4: Tag and verify v1.2.1**

Create and push annotated tag `v1.2.1`, wait for CI and release workflows, download and verify release archives/checksums/attestations, and confirm both `v1.2.1^{}` and `v1^{}` resolve to the release commit.
