# README Tutorial Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite both README files as tutorial-first documentation with context, complete examples for local LPK work, local Docker images, LazyCat device preview, server-side image copy, and CI/CD publishing.

**Architecture:** Keep all onboarding material in `README.md` and `README.zh-CN.md`, with the same section order and equivalent facts. Start with a glossary and workflow map, then order examples by external dependency count. Preserve advanced API details as a compact reference after the tutorials.

**Tech Stack:** Markdown, Go 1.25 examples, lzc-toolkit-go public packages, lzc-cli 2.0.8 compatibility sources, punctuation gates, Go tests, GitHub Actions.

## Global Constraints

- Do not modify Go APIs or runtime behavior.
- Every public symbol in an example must exist in the current repository.
- Every configuration field must come from the current schema, tests, or lzc-cli 2.0.8 templates.
- Chinese and English README files must use the same section and example order.
- Keep the language switch at line 3 of both files.
- Never include real credentials, device addresses, tokens, passwords, or private keys.
- Distinguish developer-platform token, ShellAPI credential, and SSH authorization.
- State that `appstore.CopyImage` is server-side and requires no local Docker.
- State that `dockerlocal.New(nil)` requires a working local Docker CLI and `docker buildx`.
- State that DebugBridge runs the service on the LazyCat side and does not expose a local listening port to the device.
- Preserve the exact reference package `@lazycatcloud/lzc-cli` and version `2.0.8`.

---

### Task 1: Replace the API-first opening with an onboarding map

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`

**Interfaces:**
- Consumes: current compatibility metadata and package responsibilities.
- Produces: stable headings used by all later examples.

- [ ] **Step 1: Replace the opening section order in both files**

Use these equivalent headings:

```text
What this library does / 这个库解决什么问题
Installation / 安装
Core concepts / 先认识几个概念
How the pieces fit / 一次开发会经过哪些组件
Choose a package / 按任务选择包
Tutorials / 完整例子
Advanced reference / API 与高级能力参考
Compatibility / 兼容性
Security / 安全性
```

- [ ] **Step 2: Add a glossary table**

Cover LPK, Manifest, `lzc-build.yml`, LPK v1/v2, OCI image layout, `ImageBuilder`, official lint, developer-platform token, ShellAPI, DebugBridge, and `project`. Each description must answer what it is and when the reader needs it.

- [ ] **Step 3: Add the workflow map**

```text
local project
  -> build.Build / build.BuildFile
  -> LPK
     -> inspect / lint / signature       local processing
     -> project + DebugBridge             preview on a LazyCat device
     -> appstore.Publish                  submit to developer platform

project with images
  -> dockerlocal or buildpack
  -> OCI image layout
  -> build.Build
  -> LPK
```

- [ ] **Step 4: Add the package-choice table**

Map each user task to `lpk`, `manifest`, `build`, `image/dockerlocal`, `image/buildpack`, `auth`, `appstore`, `remote/shellapi`, `remote/ssh`, `remote/debugbridge`, `project`, and `workflow/project`.

- [ ] **Step 5: Verify the bilingual outline**

Run:

```bash
rg -n '^## ' README.md README.zh-CN.md
```

Expected: both files show the same major section count and matching order.

- [ ] **Step 6: Commit the onboarding structure**

```bash
git add README.md README.zh-CN.md
git commit -m "docs: explain LazyCat toolkit concepts"
```

### Task 2: Add the zero-dependency local LPK tutorial

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`

**Interfaces:**
- Consumes: `build.BuildFile`, `inspect.File`, `lpk.OpenFile`, `Reader.EffectiveManifest`.
- Produces: the first runnable example, requiring only Go and local files.

- [ ] **Step 1: Document this project tree**

```text
hello-lpk/
├── lzc-build.yml
├── lzc-manifest.yml
├── package.yml
└── content/
    └── html/
        └── index.html
```

- [ ] **Step 2: Add the exact configuration files**

`lzc-build.yml`:

```yaml
manifest: ./lzc-manifest.yml
contentdir: ./content
```

`package.yml`:

```yaml
package: cloud.lazycat.apps.hello
version: 0.1.0
name: Hello LPK
locales:
  zh:
    name: 你好 LPK
  en:
    name: Hello LPK
```

`lzc-manifest.yml`:

```yaml
application:
  subdomain: hello
  routes:
    - /=file:///lzcapp/pkg/content/html
```

- [ ] **Step 3: Add a complete Go program**

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/lib-x/lzc-toolkit-go/build"
    "github.com/lib-x/lzc-toolkit-go/inspect"
    "github.com/lib-x/lzc-toolkit-go/lpk"
)

func main() {
    ctx := context.Background()
    result, err := build.BuildFile(ctx, "hello.lpk", build.Request{
        Root:    "./hello-lpk",
        ForceV2: true,
        Strict:  true,
    })
    if err != nil {
        log.Fatal(err)
    }

    info, err := inspect.File(ctx, "hello.lpk")
    if err != nil {
        log.Fatal(err)
    }

    reader, err := lpk.OpenFile(ctx, "hello.lpk")
    if err != nil {
        log.Fatal(err)
    }
    defer reader.Close()

    effective, err := reader.EffectiveManifest(ctx)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("built %s %s as %s\n", result.Package, result.Version, result.Layout)
    fmt.Printf("archive: %s, size: %d bytes\n", info.Format, info.Size)
    fmt.Printf("subdomain: %s\n", effective.Manifest.Application.Subdomain)
}
```

- [ ] **Step 4: Explain inputs, output, and ownership**

State that `BuildFile` atomically writes `hello.lpk`, `ForceV2` selects the split `package.yml` and `manifest.yml` layout, and `OpenFile` owns its internal file resource until `Close`. Also show the `io.Writer` alternative with `build.Build` and state that the SDK does not close caller-owned readers or writers.

- [ ] **Step 5: Document common failures**

Explain missing `application.subdomain`, invalid package ID, missing Manifest, and build scripts being disabled unless `RunBuildScript` is true.

- [ ] **Step 6: Commit the local tutorial**

```bash
git add README.md README.zh-CN.md
git commit -m "docs: add local LPK tutorial"
```

### Task 3: Add the local Docker image tutorial

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`

**Interfaces:**
- Consumes: `build.Request.ImageBuilder`, `dockerlocal.New`, `dockerlocal.WithPlatform`.
- Produces: a complete image-bearing LPK build example.

- [ ] **Step 1: State the exact prerequisites**

Require a local `docker` executable, a running Docker daemon, and `docker buildx`. State that importing `build`, `lpk`, or `oci` alone does not invoke Docker.

- [ ] **Step 2: Add the image configuration**

`lzc-build.yml`:

```yaml
manifest: ./lzc-manifest.yml
images:
  app:
    builder: local
    context: ./app
    dockerfile: ./app/Dockerfile
```

`lzc-manifest.yml`:

```yaml
application:
  subdomain: docker-hello
  image: embed:app
```

`app/Dockerfile`:

```dockerfile
FROM alpine:3.20
CMD ["sh", "-c", "while true; do echo hello; sleep 30; done"]
```

- [ ] **Step 3: Add the complete build call**

```go
result, err := build.BuildFile(ctx, "docker-hello.lpk", build.Request{
    Root:         "./docker-hello",
    ForceV2:      true,
    ImageBuilder: dockerlocal.New(nil),
})
```

Also show `dockerlocal.New(nil, dockerlocal.WithPlatform("linux/arm64"))` and state that the default is `linux/amd64`.

- [ ] **Step 4: Explain image output**

Describe `images.lock`, `images/`, embedded layer blobs, upstream layers, and `Result.ResolvedImages`. Explain that `embed:app` is rewritten with the resolved image digest.

- [ ] **Step 5: Add failure guidance**

Map `INCOMPATIBLE_BACKEND` to a missing or mismatched image builder, `COMMAND_FAILED` to Docker CLI/build failures, and OCI validation errors to malformed image output.

- [ ] **Step 6: Commit the Docker tutorial**

```bash
git add README.md README.zh-CN.md
git commit -m "docs: add local Docker image tutorial"
```

### Task 4: Add the LazyCat device preview tutorial

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`

**Interfaces:**
- Consumes: `shellapi.LoadConfig`, `shellapi.New`, `Client.DefaultBox`, `Client.ClientID`, `ssh.ParseTarget`, `ssh.New`, `debugbridge.New`, `buildpack.New`, `blobcache.New`, `project.New`, `Service.Deploy`, `Service.Start`, `Service.Logs`.
- Produces: an end-to-end remote build and deployment sequence.

- [ ] **Step 1: Define preview accurately**

State that source editing happens locally, while images and the service run on the LazyCat/build-remote side. State that DebugBridge does not reverse-proxy a local `localhost` port to the device.

- [ ] **Step 2: List prerequisites**

Require the LazyCat client ShellAPI files, a default device, Developer Tools, an SSH-authorized build-remote target, a LazyCat UID, and a compatible DebugBridge backend. Explain that App Store token is unrelated to this path.

- [ ] **Step 3: Add the transport construction**

```go
config, err := shellapi.LoadConfig(ctx, shellapi.ConfigOptions{})
shell, err := shellapi.New(ctx, shellapi.Options{Config: config})
defer shell.Close()

box, err := shell.DefaultBox(ctx)
devID, err := shell.ClientID(ctx)
target, err := ssh.ParseTarget(os.Getenv("LZC_BUILD_REMOTE_USER"), os.Getenv("LZC_BUILD_REMOTE_ADDR"))
runner := ssh.New(ssh.Options{Target: target})
bridge := debugbridge.New(
    runner,
    runner.BridgeCommand,
    debugbridge.WithUID(box.LoginUser),
    debugbridge.WithHostCommand(runner.HostCommand),
)
```

The final README program must check every returned error before continuing.

- [ ] **Step 4: Add remote build, deploy, start, and logs**

```go
remoteImages := buildpack.New(bridge, blobcache.New(projectDir))
built, err := build.BuildFile(ctx, packagePath, build.Request{
    Root:         projectDir,
    ConfigFile:   "lzc-build.dev.yml",
    ForceV2:      true,
    ImageBuilder: remoteImages,
})

packageFile, err := os.Open(packagePath)
defer packageFile.Close()
projects, err := project.New(project.Options{Backend: bridge})
_, err = projects.Deploy(ctx, project.DeployRequest{
    Package: packageFile, PackageID: built.Package, DevID: devID,
})
running, err := projects.Start(ctx, project.StartRequest{
    AppID: built.Package, LocalVersion: built.Version,
})
follow := false
_, err = projects.Logs(ctx, project.LogRequest{
    AppID: built.Package, Follow: &follow, Stdout: os.Stdout, Stderr: os.Stderr,
})
```

- [ ] **Step 5: Explain iterative development**

Describe `project/rsync.Sync`, `project.Exec`, `project.CopyTo`, and `project.Logs`. State that the caller chooses a filesystem watcher and restart policy.

- [ ] **Step 6: Commit the device tutorial**

```bash
git add README.md README.zh-CN.md
git commit -m "docs: add LazyCat device preview tutorial"
```

### Task 5: Add authentication, server-side image copy, and publishing tutorials

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`

**Interfaces:**
- Consumes: `auth.NewClient`, `Client.Login`, `auth.EnvironmentToken`, `auth.StoreProvider`, `tokenfile.Store`, `appstore.New`, `Client.CopyImage`, `Client.Publish`.
- Produces: separate examples for login, image copy, and LPK submission.

- [ ] **Step 1: Explain both token sources**

Document username/password exchange through `POST https://account.lazycat.cloud/api/login/signin` and direct token injection. For existing lzc-cli login, state the precedence `LZC_CLI_TOKEN` then `~/.config/lazycat/box-config.json` field `token`; mention `lzc-cli config get token` without encouraging logging secrets.

- [ ] **Step 2: Add a complete server-side image copy program**

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/lib-x/lzc-toolkit-go/appstore"
    "github.com/lib-x/lzc-toolkit-go/auth"
)

func main() {
    ctx := context.Background()
    client := appstore.New(appstore.Options{
        Token: auth.EnvironmentToken{},
    })
    result, err := client.CopyImage(ctx, appstore.CopyImageRequest{
        Image:    "docker.io/library/alpine:3.20",
        Platform: "amd64",
        Timeout:  20 * time.Minute,
        OnProgress: func(progress appstore.CopyProgress) {
            fmt.Printf("finished=%v layers=%d\n", progress.Finished, len(progress.Layers))
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result.LazyCatImage)
}
```

State next to the code: local Docker is not required; only network access and developer-platform token are required. The request contains no source Registry username/password field, so callers cannot add private Registry credentials through this API.

- [ ] **Step 3: Add the password-login variant**

Show `auth.NewClient(auth.ClientOptions{Store: tokenfile.Store{Path: tokenPath}}).Login(...)`, state that only the returned token is stored, and recommend CI secrets instead of keeping account passwords in pipelines.

- [ ] **Step 4: Add the publish program**

Open the built LPK with `os.Open`, call `appstore.New` with `auth.EnvironmentToken{}`, and call `Publish` with a non-empty localized changelog. Explain `CreateIfMissing` and `Application` without enabling them by default.

- [ ] **Step 5: Explain the publish sequence**

Document stream spooling, LPK parsing, official lint, application existence check, upload, package identity validation, and review submission. State that `Publish` does not close the caller-owned LPK reader.

- [ ] **Step 6: Commit the platform tutorials**

```bash
git add README.md README.zh-CN.md
git commit -m "docs: add image copy and publishing tutorials"
```

### Task 6: Consolidate advanced reference and verify the complete rewrite

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `docs/superpowers/plans/2026-07-10-readme-tutorial.md`

**Interfaces:**
- Consumes: all tutorial sections.
- Produces: concise advanced reference, bilingual parity, and verified documentation.

- [ ] **Step 1: Move isolated snippets behind the tutorials**

Keep compact reference sections for Reader/Writer APIs, LPK v1/v2, lint profiles, signatures, rsync, workflow observers, Testflight, and APK trigger. Remove explanations already covered in the tutorials.

- [ ] **Step 2: Add a common-error table**

Cover `INVALID_CONFIG`, `INCOMPATIBLE_BACKEND`, `UNAUTHENTICATED`, `PERMISSION_DENIED`, `REMOTE_UNAVAILABLE`, `COMMAND_FAILED`, and `INTEGRITY_MISMATCH`, with the first check a reader should perform.

- [ ] **Step 3: Check every public identifier**

Run targeted `rg` searches for all symbols used in README examples. Expected: each symbol resolves in a non-README Go source file.

- [ ] **Step 4: Run prose and formatting gates**

```bash
bash /home/czyt/.cc-switch/skills/write/scripts/check-punctuation.sh --lang zh README.zh-CN.md
bash /home/czyt/.cc-switch/skills/write/scripts/check-punctuation.sh --lang en README.md
git diff --check
```

Expected: both punctuation commands print `punctuation: ok`; `git diff --check` exits zero.

- [ ] **Step 5: Run repository verification**

```bash
go test ./... -count=1
```

Expected: all packages pass.

- [ ] **Step 6: Mark this plan complete and commit**

Change completed checkboxes to `[x]`, then run:

```bash
git add README.md README.zh-CN.md docs/superpowers/plans/2026-07-10-readme-tutorial.md docs/superpowers/specs/2026-07-10-readme-tutorial-design.md
git commit -m "docs: complete tutorial-first README"
```

- [ ] **Step 7: Push and verify GitHub Actions**

Push `main`, then wait for Go 1.25, Go 1.26, race, and upstream interop jobs to complete successfully.
