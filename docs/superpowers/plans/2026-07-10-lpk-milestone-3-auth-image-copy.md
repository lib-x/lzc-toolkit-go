# LPK Milestone 3 Authentication and Image Copy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the non-interactive lzc-cli 2.0.8 account authentication contract and App Store server-side image copy/list APIs for CI/CD use.

**Architecture:** `auth` owns token providers, stores, login, and validation against the account service. `auth/tokenfile` is the optional atomic 0600 persistence adapter. `appstore` accepts an `auth.TokenProvider`, applies the exact lzc-cli headers, and implements copy-image polling and my-images without importing Docker or prompting users.

**Tech Stack:** Go standard library `net/http`, `net/url`, `encoding/json`, `os`, `time`, and `httptest`.

## Global Constraints

- Reference behavior is `@lazycatcloud/lzc-cli@2.0.8` `lib/appstore/login.js`, `lib/appstore/index.js`, and `lib/config/env.js`.
- Login uses form fields `username` and `password` at `POST /api/login/signin`.
- Validation uses `GET /api/user/current` with `X-User-Token`.
- App Store requests use both `X-User-Token` and `Cookie: userToken=<token>`.
- CI/CD direct tokens and `LZC_CLI_TOKEN` are supported through explicit providers; no interactive prompt is implemented.
- Tokens and passwords never appear in errors, URLs, logs, events, or test failure messages.
- HTTP response bodies are bounded and validated as untrusted input.
- Ordinary tests use `httptest.Server` and no LazyCat account or network.

### Task 1: Token providers and stores

**Files:** Create `auth/types.go`, `auth/provider.go`, `auth/provider_test.go`, `auth/tokenfile/store.go`, and `auth/tokenfile/store_test.go`.

**Interfaces:** Produce `auth.TokenProvider`, `auth.TokenStore`, `auth.StaticToken`, `auth.EnvironmentToken`, `auth.StoreProvider`, `auth.Chain`, and atomic `tokenfile.Store`.

- [ ] Test provider precedence, missing-token `UNAUTHENTICATED`, defensive whitespace handling, atomic file replacement, 0600 permissions, and delete behavior.
- [ ] Implement context-aware providers and a token file containing a small JSON object with a `token` field.
- [ ] Run `go test ./auth/... -count=1` and `go vet ./auth/...`.

### Task 2: Account client

**Files:** Create `auth/client.go` and `auth/client_test.go`.

**Interfaces:** Produce `auth.Client`, `auth.ClientOptions`, `Login(context.Context, Credentials) (Session, error)`, and `Validate(context.Context, string) error`.

- [ ] Use `httptest.Server` to assert exact method, path, content type, form field names, and validation header.
- [ ] Reject non-2xx responses, `success=false`, missing `data.token`, malformed JSON, oversized bodies, empty credentials, and invalid tokens with stable codes.
- [ ] Save the token only after the login response is fully validated.

### Task 3: App Store image copy client

**Files:** Create `appstore/client.go`, `appstore/images.go`, and `appstore/images_test.go`.

**Interfaces:** Produce `appstore.Client`, `appstore.Options`, `CopyImageRequest`, `CopyImageResult`, `CopyProgress`, `LayerProgress`, `ImageRecord`, `CopyImage`, and `ListImages`.

- [ ] Assert exact v3 copy/progress/myimages paths, escaped query parameters, and both authentication mechanisms.
- [ ] Poll with a configurable interval and operation timeout, surface per-layer progress through a callback, return `lzc_image`, and map server `errmsg` to a structured remote error.
- [ ] Sort image records newest-first and retain records with server errors as typed data rather than printing a table.

### Task 4: Verification and delivery

**Files:** Modify `README.md`, `scripts/check-import-boundaries.sh`, and the compatibility design where implementation details require clarification.

- [ ] Document direct token, environment provider, token store, password login, and server-side copy-image use.
- [ ] Run `gofmt`, `go mod tidy`, `go test ./...`, `go test -race ./...`, `go vet ./...`, import-boundary checks, shell syntax checks, and `git diff --check`.
- [ ] Commit and push, then continue automatically with publish/testflight APIs.
