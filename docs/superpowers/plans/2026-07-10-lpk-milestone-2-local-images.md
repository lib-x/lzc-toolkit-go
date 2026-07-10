# LPK Milestone 2 Local Images Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add lzc-cli 2.0.8 compatible local image configuration, Docker archive conversion, and a Docker CLI adapter that implements `build.ImageBuilder` without adding Docker dependencies to the base packages.

**Architecture:** `image` normalizes the public `lzc-build.yml images` contract. `image/dockerarchive` converts an untrusted `docker image save` TAR stream into the existing `oci` layout and `images.lock` formats. `image/dockerlocal` owns Docker CLI execution and composes the other two packages behind `build.ImageBuilder`; ordinary tests inject fakes and never require Docker.

**Tech Stack:** Go 1.24+, standard library archive/tar, compress/gzip, crypto/sha256, encoding/json, io/fs, os/exec, and the existing `build`, `manifest`, and `oci` packages.

## Global Constraints

- Match `@lazycatcloud/lzc-cli@2.0.8` behavior rather than unused or imagined CLI features.
- `build`, `lpk`, `archive`, `manifest`, `oci`, `inspect`, `lint`, and `signature` must not import Docker CLI adapters or Docker SDK packages.
- Docker archive parsing treats paths, JSON, sizes, digests, and layer counts as untrusted input.
- Caller-owned readers and writers are never closed.
- Every blocking operation accepts `context.Context`.
- Command execution uses argument arrays, never a shell command string.
- Ordinary `go test ./...` requires no Docker daemon, Node.js, network, LazyCat account, or SSH access.
- Tests exercise public interfaces and use deterministic Docker archive fixtures.
- Each completed implementation batch is committed and pushed to `feature/lpk-foundation`.

---

### Task 1: Normalize the `images` configuration

**Files:**

- Create: `image/config.go`
- Create: `image/config_test.go`

**Interfaces:**

- Produces `image.Builder`, `image.Entry`, `image.Normalize(context.Context, string, manifest.Manifest, any) ([]Entry, error)`.
- `Entry` contains `Alias`, `Builder`, `ContextDir`, `DockerfilePath`, `DockerfileContent`, `ImageLabel`, and `UpstreamMatch`.

- [ ] **Step 1: Write a failing public behavior test**

Test a string shorthand, object configuration, default `builder=remote`, explicit `builder=local`, default `upstream-match=registry.lazycat.cloud`, package/version-derived labels, and sorted aliases.

- [ ] **Step 2: Verify the test fails**

Run `go test ./image -run TestNormalize -count=1`; expect undefined public types and functions.

- [ ] **Step 3: Implement boundary validation**

Decode the raw YAML-compatible map through `yaml.Node`, reject aliases outside `^[A-Za-z0-9][A-Za-z0-9._-]*$`, reject `upstream_match` and `dockerfile_content`, require exactly one Dockerfile source, require the Dockerfile to be inside its context, and accept only `local` or `remote` builders.

- [ ] **Step 4: Verify Task 1**

Run `go test ./image -count=1` and `go vet ./image`; expect success.

### Task 2: Convert Docker save TAR to OCI

**Files:**

- Create: `image/dockerarchive/types.go`
- Create: `image/dockerarchive/convert.go`
- Create: `image/dockerarchive/convert_test.go`

**Interfaces:**

- Produces `dockerarchive.Spec`, `dockerarchive.Request`, and `dockerarchive.Convert(context.Context, io.Reader, string, Request) (Result, error)`.
- `Spec` contains `Ref`, `Alias`, `ImageID`, `Upstream`, and `EmbeddedDiffIDs`.
- Output root contains only `images/oci-layout`, `images/index.json`, `images/blobs/sha256/*`, and `images.lock`.

- [ ] **Step 1: Write a deterministic Docker archive fixture test**

Construct TAR entries for `manifest.json`, a config JSON, and two layer TAR files. Assert one upstream layer is absent from OCI blobs, one embedded layer is reproducibly gzip-compressed, the config and image manifest blobs match their descriptors, and `oci.Validate` accepts the result.

- [ ] **Step 2: Verify the test fails**

Run `go test ./image/dockerarchive -run TestConvert -count=1`; expect undefined conversion API.

- [ ] **Step 3: Implement bounded streaming conversion**

Spool the caller-owned TAR reader to a bounded temporary file, index regular entries without extracting paths, parse `manifest.json`, verify the selected config digest equals `Spec.ImageID`, map config `rootfs.diff_ids` to archive layers, gzip only selected embedded layers with zero timestamps, and write descriptors plus lock data through the `oci` writer APIs.

- [ ] **Step 4: Add abuse-case tests**

Reject duplicate TAR paths, non-local paths, missing entries, mismatched image IDs, mismatched layer counts, duplicate aliases, unknown refs, and upstream layers without an upstream reference.

- [ ] **Step 5: Verify Task 2**

Run `go test ./image/dockerarchive -count=1`, `go test -race ./image/dockerarchive`, and `go vet ./image/dockerarchive`; expect success.

### Task 3: Implement the local Docker adapter

**Files:**

- Create: `image/dockerlocal/engine.go`
- Create: `image/dockerlocal/cli.go`
- Create: `image/dockerlocal/builder.go`
- Create: `image/dockerlocal/builder_test.go`
- Modify: `scripts/check-import-boundaries.sh`

**Interfaces:**

- Produces `dockerlocal.Engine`, `dockerlocal.BuildResult`, `dockerlocal.Builder`, and `dockerlocal.New(Engine) *Builder`.
- `Builder` implements `build.ImageBuilder`.
- The default CLI engine exposes Docker buildx build, Docker image inspect, Docker image save, and best-effort image removal through direct argv execution.

- [ ] **Step 1: Write a fake-engine adapter test**

Provide two local image entries, deterministic inspect metadata, and a Docker TAR fixture. Assert build calls use `linux/amd64`, save receives deduplicated refs, cleanup runs, the returned artifact passes `oci.Validate`, and manifest aliases resolve to image IDs.

- [ ] **Step 2: Verify the test fails**

Run `go test ./image/dockerlocal -run TestBuilder -count=1`; expect undefined adapter API.

- [ ] **Step 3: Implement engine contracts and CLI execution**

Use `exec.CommandContext` with fixed executable and argv fields. Capture bounded stdout/stderr, validate Docker inspect JSON before use, merge explicit environment with the current process only inside the CLI adapter, and redact command output from stable SDK errors.

- [ ] **Step 4: Compose build, save, and conversion**

Normalize configuration through `image.Normalize`, reject remote entries with `INCOMPATIBLE_BACKEND`, build all local entries, derive embedded layers from inspected diff IDs, save the refs once, invoke `dockerarchive.Convert`, return an `os.DirFS` artifact, and remove temporary images and directories on every exit path.

- [ ] **Step 5: Enforce dependency boundaries**

Add `./image` and `./image/dockerarchive` to the dependency-light package list. Confirm neither imports `image/dockerlocal`, Docker SDK packages, App Store, SSH, or remote lifecycle packages.

- [ ] **Step 6: Verify Task 3**

Run `go test ./...`, `go test -race ./...`, `go vet ./...`, and `bash scripts/check-import-boundaries.sh`; expect success without Docker installed.

### Task 4: Documentation and optional Docker validation

**Files:**

- Modify: `README.md`
- Create: `scripts/validate-local-images.sh`

**Interfaces:**

- Documents explicit opt-in through `build.Request.ImageBuilder = dockerlocal.New(nil)`.
- The optional script checks Docker/buildx availability and builds a temporary single-layer fixture; it is never called by ordinary tests.

- [ ] **Step 1: Document package boundaries and usage**

Add an example importing `image/dockerlocal`, explain that the adapter invokes local Docker commands, and state that remote builders remain a later adapter.

- [ ] **Step 2: Add optional validation script**

The script must use `docker buildx version` as a prerequisite, create all temporary data under `mktemp -d`, clean it through `trap`, run a focused Go integration harness, and exit with a clear skip message when Docker is unavailable.

- [ ] **Step 3: Run final verification**

Run `gofmt`, `go mod tidy`, `go test ./...`, `go test -race ./...`, `go vet ./...`, import-boundary checks, shell syntax checks, and `git diff --check`.

- [ ] **Step 4: Commit and push**

Commit with `feat: add local image build adapter` and push `feature/lpk-foundation`; verify the remote head with `git ls-remote`.
