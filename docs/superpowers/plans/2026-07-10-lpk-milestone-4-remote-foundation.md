# LPK Milestone 4 Remote Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the lzc-cli 2.0.8 remote transport foundation required by CI/CD: typed backend capabilities, SSH execution, local blob caching, remote build-pack image construction, and ShellAPI discovery.

**Architecture:** `remote` contains dependency-light contracts and capability gates. `remote/ssh` and `remote/shellapi` are optional transport adapters, `remote/blobcache` owns verified local content-addressed storage, and `image/buildpack` implements `build.ImageBuilder` through a narrow remote backend interface. High-level deploy/start/exec/sync APIs are intentionally deferred to the next Milestone 4 plan so callers importing build or LPK packages do not pull gRPC or SSH dependencies.

**Tech Stack:** Go 1.25+, standard library `context`, `os/exec`, `io`, `io/fs`, `crypto/sha256`, generated Protocol Buffers, `google.golang.org/grpc`, and `httptest`/fake command runners for tests.

## Global Constraints

- Reference behavior is `@lazycatcloud/lzc-cli@2.0.8` `lib/debug_bridge.js`, `lib/shellapi.js`, `lib/shellapi.proto`, `lib/box/ssh_remote.js`, `lib/project_blob_cache.js`, and `lib/app/lpk_build_images.js`.
- Public APIs are context-aware, never prompt, never call `os.Exit`, and never mutate global logging state.
- Caller-owned readers and writers are never closed.
- Commands are executable plus argv arrays; no public API accepts a shell command string.
- Command output and gRPC response sizes are bounded and secrets are excluded from errors.
- Ordinary tests require no SSH server, LazyCat client, Docker daemon, remote box, or network.
- `build`, `lpk`, `archive`, `manifest`, `oci`, `image`, and `image/dockerarchive` must not import `remote/ssh`, `remote/shellapi`, gRPC, or SSH dependencies.
- Backend feature floors remain the exact lzc-cli 2.0.8 values: LPK v2 `1.0.0`, pending sync dev ID `1.0.4`, build-pack context cache `1.0.4`, and blob manifest transport `1.0.5`. The older general DebugBridge command floor `0.2.0` is separate and does not gate LPK v2.
- Completed batches are committed and pushed from the feature branch; no subagents are used.

---

### Task 1: Typed backend contracts and capability gates

**Files:**
- Create: `remote/types.go`
- Create: `remote/version.go`
- Create: `remote/version_test.go`
- Modify: `scripts/check-import-boundaries.sh`

**Interfaces:**
- Produces `remote.Platform`, `remote.ParsePlatform`, `remote.BackendInfo`, `remote.Command`, `remote.Result`, `remote.Runner`, `remote.Capability`, `remote.Supports`, and `remote.Require`.
- `Runner` signature is `Run(context.Context, Command) (Result, error)`.
- `Command` contains `Name string`, `Args []string`, `Stdin io.Reader`, `Stdout io.Writer`, `Stderr io.Writer`, and `TTY bool`.

- [ ] **Step 1: Write capability and validation tests**

  Add table-driven tests for versions `0.1.9`, `0.2.0`, `1.0.3`, `1.0.4`, and `1.0.5`; assert the four exact feature floors from `version.Current().Backend`, reject malformed versions, validate `linux/amd64` and `linux/arm64`, and reject platform strings outside `^[a-z0-9]+/[a-z0-9]+$`.

- [ ] **Step 2: Run the red test**

  Run `go test ./remote -run 'Test(Supports|ParsePlatform)' -count=1`; expect undefined public remote APIs.

- [ ] **Step 3: Implement dependency-light contracts**

  Implement semantic-version comparison without adding a dependency, return `INCOMPATIBLE_BACKEND` from `Require`, defensively copy argv in command constructors, and map context cancellation to `CANCELLED`.

- [ ] **Step 4: Enforce package boundaries**

  Add `./remote` to `scripts/check-import-boundaries.sh` and keep the forbidden list covering `remote/ssh`, `remote/shellapi`, gRPC, Docker SDK, and App Store packages.

- [ ] **Step 5: Verify and commit**

  Run `gofmt -w remote`, `go test ./remote -count=1`, `go vet ./remote`, and `bash scripts/check-import-boundaries.sh`; commit as `feat: add remote backend contracts`.

### Task 2: SSH transport adapter

**Files:**
- Create: `remote/ssh/target.go`
- Create: `remote/ssh/target_test.go`
- Create: `remote/ssh/runner.go`
- Create: `remote/ssh/runner_test.go`

**Interfaces:**
- Consumes `remote.Command`, `remote.Result`, and `remote.Runner`.
- Produces `ssh.Target`, `ssh.ParseTarget(loginUser, address string) (Target, error)`, `ssh.Executor`, `ssh.Options`, `ssh.New(Options) *Runner`, `Runner.Run`, and `Runner.BridgeCommand`.
- `Target` contains `BoxName`, `User`, `Host`, and `Port`; IPv6 addresses retain the unbracketed host and default port 22.

- [ ] **Step 1: Write target parsing tests**

  Cover `host`, `host:2222`, `[2001:db8::1]:2222`, raw IPv6, empty users, empty hosts, and ports outside 1..65535. Assert the box name matches lzc-cli: host alone for port 22, otherwise `host:port`.

- [ ] **Step 2: Write fake executor argv tests**

  Inject an executor with `Run(context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error`. Assert SSH uses `-p`, `BatchMode=yes`, no shell, optional `-t`, `user@host`, and remote argv as distinct arguments after the target; assert bridge execution prefixes `lzc-docker exec -i lazycat-developer-tools /lzcapp/pkg/content/backend/main`.

- [ ] **Step 3: Run the red tests**

  Run `go test ./remote/ssh -count=1`; expect undefined target and runner APIs.

- [ ] **Step 4: Implement bounded execution and stable errors**

  Default to the `ssh` executable, use `exec.CommandContext`, cap captured stdout/stderr at 4 MiB, never include captured remote output in `Error()`, return it only in typed `remote.Result`, and preserve caller-provided streaming writers.

- [ ] **Step 5: Verify and commit**

  Run `go test ./remote/ssh -count=1`, `go test -race ./remote/ssh`, and `go vet ./remote/ssh`; commit as `feat: add remote SSH transport`.

### Task 3: Verified project blob cache

**Files:**
- Create: `remote/blobcache/cache.go`
- Create: `remote/blobcache/cache_test.go`

**Interfaces:**
- Produces `blobcache.Cache`, `blobcache.New(root string) Cache`, `Has(context.Context, string) (bool, error)`, `Open(context.Context, string) (io.ReadCloser, error)`, `Put(context.Context, string, io.Reader) (Info, error)`, `PutFile(context.Context, string) (Info, error)`, `CopyTo(context.Context, string, io.Writer) error`, and `ImportOCI(context.Context, fs.FS) ([]Info, error)`.
- `Info` contains canonical `Digest string` and `Size int64`.

- [ ] **Step 1: Write cache behavior and abuse tests**

  Assert canonical `sha256:<64 hex>` paths under `blobs/sha256`, atomic writes, deduplication, digest verification, caller reader ownership, OCI blob import, cancellation, traversal rejection, uppercase normalization, mismatched content rejection, and no partially written blob after failure.

- [ ] **Step 2: Run the red test**

  Run `go test ./remote/blobcache -count=1`; expect the package API to be undefined.

- [ ] **Step 3: Implement content-addressed storage**

  Stream into a same-directory temporary file through SHA-256 and a size counter, compare against the requested digest, chmod files 0644, fsync and rename atomically, and use `os.Root`/validated digest-derived names so caller paths never reach filesystem joins.

- [ ] **Step 4: Verify and commit**

  Run `go test ./remote/blobcache -count=1`, `go test -race ./remote/blobcache`, and `go vet ./remote/blobcache`; commit as `feat: add verified remote blob cache`.

### Task 4: Remote build-pack image adapter

**Files:**
- Create: `image/buildpack/backend.go`
- Create: `image/buildpack/builder.go`
- Create: `image/buildpack/builder_test.go`
- Modify: `scripts/check-import-boundaries.sh`

**Interfaces:**
- Consumes `build.ImageBuilder`, `image.Normalize`, `oci`, and `remote/blobcache`.
- Produces `buildpack.Backend` with `Info`, `BuildPack`, `BlobCheck`, and `BlobGet`; `buildpack.Request`, `buildpack.Manifest`, `buildpack.Blob`, `buildpack.Builder`, and `buildpack.New(Backend, blobcache.Cache, ...Option) *Builder`.
- `BuildPack` accepts an image alias, tag, context TAR reader, and optional context digest; it returns image ID, diff IDs, optional base repo digest/base diff IDs, descriptors, and required blob digests.

- [ ] **Step 1: Write fake backend build tests**

  Cover remote-only image entries, deterministic build context TAR and digest, the `1.0.4` context-digest gate, the `1.0.5` blob-manifest path, cache hits, remote blob downloads, missing remote blobs, base-layer upstream selection, manifest alias rewriting, and final `oci.Validate` success.

- [ ] **Step 2: Run the red tests**

  Run `go test ./image/buildpack -count=1`; expect undefined adapter APIs.

- [ ] **Step 3: Implement build-pack composition**

  Normalize only entries with `builder: remote`, create reproducible context TAR streams, call `remote.Require` before optional protocol fields, validate all returned digests/descriptors as untrusted input, download only missing cache blobs, materialize a temporary OCI layout, write `images.lock`, and return an internal `build.ImageArtifact` whose `FS` is `os.DirFS(tempRoot)` and whose idempotent `Close` removes `tempRoot`.

- [ ] **Step 4: Enforce dependency boundaries**

  Permit `image/buildpack` to import `remote` and `remote/blobcache`, but keep all base image/build/LPK packages free of gRPC and SSH imports.

- [ ] **Step 5: Verify and commit**

  Run `go test ./image/buildpack -count=1`, `go test -race ./image/buildpack`, `go vet ./image/buildpack`, and the import boundary script; commit as `feat: add remote build-pack image adapter`.

### Task 5: ShellAPI generated client and discovery adapter

**Files:**
- Create: `remote/shellapi/shellapi.proto`
- Generate: `remote/shellapi/shellapi.pb.go`
- Generate: `remote/shellapi/shellapi_grpc.pb.go`
- Create: `remote/shellapi/config.go`
- Create: `remote/shellapi/client.go`
- Create: `remote/shellapi/client_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Produces `shellapi.Config`, `shellapi.LoadConfig`, `shellapi.Options`, `shellapi.Client`, `shellapi.New`, `Client.Boxes`, `Client.ShellCoreInfo`, `Client.DefaultBox`, `Client.SetDefaultBox`, and `Client.DialBoxService`.
- Config discovery order is explicit options, then platform config directory files `shellapi_addr` and `shellapi_cred`; environment fallback requires both `BOX_UID` and `BOX_NAME` and supports discovery-only operations without a fake gRPC connection.

- [ ] **Step 1: Copy and generate the exact protocol**

  Copy lzc-cli 2.0.8 `lib/shellapi.proto`, change only `go_package` to `github.com/lib-x/lpk-go/remote/shellapi;shellapi`, run `go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1`, and then run:

  `protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative remote/shellapi/shellapi.proto`

- [ ] **Step 2: Write in-memory gRPC protocol tests**

  Use `bufconn` to assert `lzc-shellapi-cred` metadata, box mapping, default-box selection, `QueryShellCoreInfo` client ID caching, `ModifyBoxConfig` request fields, and the first non-empty `DialBoxServiceReply.local_proxy_address` result. Test missing config and paired environment fallback without network.

- [ ] **Step 3: Run the red tests**

  Run `go test ./remote/shellapi -count=1`; expect undefined adapter APIs before implementation.

- [ ] **Step 4: Implement configuration and client lifecycle**

  Centralize Linux/macOS/Windows config paths, trim address and credential files, use insecure local gRPC credentials like lzc-cli, attach bounded deadlines and metadata per call, expose `Close`, and never log or return credential values.

- [ ] **Step 5: Verify and commit**

  Run `go test ./remote/shellapi -count=1`, `go test -race ./remote/shellapi`, `go vet ./remote/shellapi`, `go mod tidy`, and the import boundary script; commit as `feat: add ShellAPI discovery client`.

### Task 6: Milestone 4 remote foundation verification and delivery

**Files:**
- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-07-10-lpk-lifecycle-library-design.md` only if implementation evidence requires clarification.

**Interfaces:**
- Documents package boundaries, explicit SSH target setup, ShellAPI discovery, blob cache, remote build-pack injection, feature floors, and the fact that ordinary tests are fully offline.

- [ ] **Step 1: Add runnable library examples**

  Show `ssh.ParseTarget`, `ssh.New`, `shellapi.New`, `blobcache.New`, and `build.Request{ImageBuilder: buildpack.New(...)}` without any CLI flags, prompts, or global state.

- [ ] **Step 2: Run the complete verifier set**

  Run `gofmt`, `go mod tidy`, `go test ./...`, `go test -race ./...`, `go vet ./...`, import-boundary checks, shell syntax checks, `git diff --check`, and the existing lzc-cli 2.0.8 compatibility workflow.

- [ ] **Step 3: Validate remote state and push**

  Confirm the feature branch is clean, push it, verify the remote SHA with `git ls-remote`, and inspect the GitHub Actions run with `gh run watch --exit-status` before beginning the high-level project lifecycle plan.
