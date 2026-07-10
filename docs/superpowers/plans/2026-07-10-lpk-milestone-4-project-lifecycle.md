# LPK Milestone 4 Project Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the complete non-interactive lzc-cli 2.0.8 remote application and project lifecycle as composable Go library packages.

**Architecture:** `remote/debugbridge` implements the backend command protocol over an injected transport. The dependency-light `project` package owns typed lifecycle orchestration, while optional `project/rsync` owns local process execution and SSH tunnels. Streaming exec/log/copy operations use caller-owned readers and writers; no package owns terminal state, prompts, or global logging.

**Tech Stack:** Go 1.25+, existing `build`, `lpk`, `remote`, `remote/debugbridge`, `remote/ssh`, `remote/shellapi`, standard library process/stream APIs, and fake runners for ordinary tests.

## Global Constraints

- Match `@lazycatcloud/lzc-cli@2.0.8` `lib/debug_bridge.js` and `lib/app/project_*.js` behavior.
- Build-remote DebugBridge commands insert `--uid <uid>` immediately after the command name where lzc-cli does.
- Public methods accept `context.Context`; caller-owned readers/writers are never closed.
- No public API prompts, prints, changes terminal mode, calls `os.Exit`, or accepts a shell command string.
- Install, exec, copy, and log streams are bounded only for captured diagnostic data; caller output streams are not truncated.
- Ordinary tests require no remote box, SSH, rsync, Docker, LazyCat account, or network.
- Project target sync paths must remain under `/lzcapp/cache`.
- Destructive uninstall data deletion and rsync deletion are explicit booleans and never inferred.

---

### Task 1: Extend DebugBridge lifecycle and command APIs

**Files:**
- Create: `remote/lifecycle.go`
- Create: `remote/debugbridge/lifecycle.go`
- Create: `remote/debugbridge/lifecycle_test.go`
- Create: `remote/debugbridge/docker.go`
- Create: `remote/debugbridge/docker_test.go`

**Interfaces:**
- Produces `remote.LifecycleBackend`, `remote.AppInfo`, `remote.DeployInfo`, `remote.SyncDevIDRequest`, `remote.DockerRequest`, and `remote.StreamRequest`.
- `debugbridge.Client` adds `Install`, `Status`, `AppInfo`, `SyncDevID`, `IsDevshell`, `Pause`, `Resume`, `Uninstall`, `Docker`, `DockerCompose`, and `HostReadFile`.

- [x] Write fake-runner tests asserting exact argv and `--uid` placement for every command, streamed LPK stdin, `--pkgId`, `--delete-data`, `--dev-id`, `--userapp`, TTY propagation, JSON response parsing, and stable errors.
- [x] Implement client UID configuration through `debugbridge.WithUID`; reject UID-required calls when it is empty.
- [x] Validate app IDs, deploy/status JSON, Docker argv, and host paths at the boundary; never include remote stdout/stderr in error strings.
- [x] Run `go test ./remote/debugbridge -count=1`, race, vet, and commit as `feat: add DebugBridge lifecycle protocol`.

### Task 2: Project lifecycle service

**Files:**
- Create: `project/types.go`
- Create: `project/service.go`
- Create: `project/service_test.go`
- Create: `project/deploy.go`
- Create: `project/deploy_test.go`

**Interfaces:**
- Produces `project.Service`, `project.Options`, `project.DeployRequest`, `project.DeployResult`, `project.Info`, `project.Start`, `project.Stop`, `project.Uninstall`, and `project.Wait`.
- `Deploy` accepts an LPK `io.Reader`, explicit package ID, optional dev ID, and user-app flag; it installs, conditionally waits for readiness based on `CapabilityPendingSyncDevID`, and synchronizes the dev ID.

- [x] Test install/deploy ordering, pending sync capability behavior, terminal startup failures, context cancellation, pause/resume state transitions, uninstall with and without data deletion, and typed results.
- [x] Implement polling using configurable interval/timeout and actual status predicates rather than fixed sleeps.
- [x] Keep project package dependent only on `remote` contracts, not SSH, gRPC, rsync, or App Store packages.
- [x] Run package tests/race/vet and commit as `feat: add project lifecycle service`.

### Task 3: Docker, Compose, exec, logs, and copy

**Files:**
- Create: `project/docker.go`
- Create: `project/docker_test.go`
- Create: `project/exec.go`
- Create: `project/exec_test.go`
- Create: `project/logs.go`
- Create: `project/copy.go`

**Interfaces:**
- Produces `project.ExecRequest`, `project.LogRequest`, `project.CopyRequest`, `Service.Exec`, `Service.Logs`, `Service.CopyTo`, `Service.Docker`, and `Service.Compose`.
- Exec defaults to service `app`, workdir `/lzcapp/cache/project-mirror`, and command `/bin/sh`; copy uses Docker `cp -` TAR stdin exactly like lzc-cli.

- [x] Test exact Compose project naming, service lookup, running-state checks, workdir creation, TTY and passthrough argv, log follow/tail/since flags, TAR copy streaming, and caller stream ownership.
- [x] Reject empty service names, unsafe container/workdir paths, invalid tail values, and unbounded captured JSON.
- [x] Run project tests/race/vet and commit as `feat: add project remote command APIs`.

### Task 4: Rsync synchronization adapter

**Files:**
- Create: `project/rsync/types.go`
- Create: `project/rsync/args.go`
- Create: `project/rsync/args_test.go`
- Create: `project/rsync/sync.go`
- Create: `project/rsync/sync_test.go`

**Interfaces:**
- Produces `rsync.Options`, `rsync.Target`, `rsync.Executor`, `rsync.Sync`, `rsync.BuildArgs`, and `rsync.BuildTunnelArgs`.
- Default target is `/lzcapp/cache/project-mirror`; password is the lzc-cli rsync daemon value and is passed only through the child environment.

- [x] Test IPv4/IPv6 destinations, user-app UID paths, `--delete`, `--dry-run`, source subdirectories, `.lzcdevignore`, exact SSH tunnel argv, rsync 3.2.0 version validation, and stable failure mapping.
- [x] Implement direct argv execution with injected executor; do not implement watch mode with a third-party watcher dependency. Expose repeated `Sync` calls so callers can attach their preferred watcher.
- [x] Validate the target stays under `/lzcapp/cache` and never expose the rsync password in errors or events.
- [x] Run package tests/race/vet and commit as `feat: add project rsync adapter`.

### Task 5: High-level workflow composition and documentation

**Files:**
- Modify: `workflow/workflow.go`
- Modify: `workflow/workflow_test.go`
- Modify: `README.md`
- Modify: `scripts/check-import-boundaries.sh`

**Interfaces:**
- Adds build→install→sync→start composition through injected build/project services while preserving the existing dependency-light workflow API.

- [x] Add orchestration tests for success, build failure, install failure, cancellation, cleanup, and observer events.
- [x] Document direct ShellAPI and SSH setup, DebugBridge construction, remote build-pack injection, lifecycle calls, exec/log/copy/sync, and CI/CD authentication/publishing flow.
- [x] Enforce that `project` does not import transport adapters and base packages do not import project/gRPC/SSH/App Store packages.
- [x] Run all Go tests, race, vet, import checks, shell checks, lzc-cli interoperability, and lazycat-contrib validation; commit and push.

### Task 6: Final module/repository migration and release gate

**Files:** all module imports, generated code, docs, CI, scripts, fixtures, and repository metadata.

- [x] Rename the GitHub repository from `lib-x/lpk-go` to `lib-x/lzc-toolkit-go` with `gh api` after all functionality above is green.
- [x] Change `go.mod` and every import to `github.com/lib-x/lzc-toolkit-go`; update Protobuf `go_package` and regenerate generated clients.
- [x] Update README, specs, ADR status, scripts, examples, compatibility checks, and validation repositories.
- [x] Run `go test ./...`, race, vet, import checks, shell checks, upstream interoperability, lazycat-contrib validation, and verify the renamed remote with `gh repo view` and `git ls-remote`.
- [ ] Commit, push, merge to `main`, wait for GitHub Actions success, and mark the development goal complete.
