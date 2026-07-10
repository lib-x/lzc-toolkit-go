# lpk-go

Go SDK for LazyCat LPK lifecycle primitives, based on `@lazycatcloud/lzc-cli`
`2.0.8`.

This repository is a library, not a CLI port. Packages are split by
responsibility so callers can import lightweight LPK, manifest, inspect, lint,
and signature APIs without pulling Docker, App Store, SSH, gRPC, or remote
lifecycle integrations.

## Compatibility

Reference CLI:

- package: `@lazycatcloud/lzc-cli`
- version: `2.0.8`
- integrity: `sha512-CcH18fg1SBqTN4od7NCXMWYaAwjICgEuguphgNcb9Lp7v5+RDYa27+BEevC7faFFm8Zhjw3Rh/sinYc7fc39SA==`
- shasum: `af9fece8a9756a00e093f817b3c3083971cc171f`

The goal is semantic compatibility with lzc-cli formats and service-facing
contracts. Byte-identical archive output is not required.

## Examples

Parse a manifest:

```go
doc, err := manifest.Parse(data)
if err != nil {
    return err
}
var typed manifest.Manifest
if err := doc.Decode(&typed); err != nil {
    return err
}
```

Write an LPK to a buffer:

```go
var out bytes.Buffer
result, err := lpk.Write(ctx, &out, lpk.WriteRequest{
    Layout: lpk.LayoutV2,
    Files:  os.DirFS("package-root"),
    Strict: true,
})
_ = result
```

Open an LPK from a stream:

```go
reader, err := lpk.Open(ctx, bytes.NewReader(pkg))
if err != nil {
    return err
}
defer reader.Close()
effective, err := reader.EffectiveManifest(ctx)
```

Open an LPK without stream spooling when random access is already available:

```go
reader, err := lpk.OpenReaderAt(ctx, bytes.NewReader(pkg), int64(len(pkg)))
if err != nil {
    return err
}
defer reader.Close()
```

Use file helpers:

```go
result, err := lpk.WriteFile(ctx, "app.lpk", request)
info, err := inspect.File(ctx, "app.lpk")
_, _ = result, info
```

Build a project directly to a writer. Project scripts are disabled unless the
request explicitly enables them:

```go
var output bytes.Buffer
result, err := build.Build(ctx, &output, build.Request{
    Root:       "./my-lzcapp",
    ConfigFile: "lzc-build.yml",
})
_ = result
```

`build.BuildFile` uses atomic replacement. The build package supports the
non-image project collection behavior of lzc-cli 2.0.8, including development
config inheritance, manifest preprocessing, `contentdir`, `icon`, automatic
`lzc-deploy-params.yml`, browser extensions, AI pod services,
`compose_override`, package overrides, and resource exports. Image-bearing
projects currently return `INCOMPATIBLE_BACKEND` until the separate OCI/image
stage is configured in Milestone 2.

Validate an extracted lzc-cli image layout without importing Docker support:

```go
report, err := oci.Validate(ctx, os.DirFS("package-root"))
_ = report
```

The `oci` package also exposes `ReadLock`/`WriteLock`,
`ReadIndex`/`WriteIndex`, and typed OCI descriptors. Embedded layer blobs are
verified by streaming sha256; upstream layers are represented by
`images.lock` and do not have to exist in the package.

Image production is injected into `build.Request.ImageBuilder`. The adapter
returns a package-relative `fs.FS`; build validates it with `oci.Validate`,
copies only `images.lock` and `images/`, rewrites `embed:<alias>` references to
the resolved image ID, and switches `content.tar` to `content.tar.gz`. This
keeps Docker and remote builders out of the base build package.

Opt into the local Docker adapter explicitly:

```go
result, err := build.Build(ctx, dst, build.Request{
    Root:         "./my-lzcapp",
    ImageBuilder: dockerlocal.New(nil),
})
```

`dockerlocal.New(nil)` uses the Docker CLI behavior supported by lzc-cli
2.0.8: `docker buildx build --load`, `docker image inspect`,
`docker image save`, and best-effort cleanup. Tests can inject
`dockerlocal.Engine`; importing `build`, `lpk`, `oci`, or `image` alone does
not import or execute the Docker adapter. The local target defaults to
`linux/amd64` and can be changed with `dockerlocal.WithPlatform`.

Use a CI/CD token directly, through `LZC_CLI_TOKEN`, or from an explicit
store:

```go
tokens := auth.Chain{
    auth.EnvironmentToken{},
    auth.StoreProvider{Store: tokenfile.Store{Path: tokenPath}},
}
client := appstore.New(appstore.Options{Token: tokens})
result, err := client.CopyImage(ctx, appstore.CopyImageRequest{
    Image:    "docker.io/example/app:1.0.0",
    Platform: "amd64",
})
if err != nil {
    return err
}
fmt.Println(result.LazyCatImage) // registry.lazycat.cloud/...
```

Username/password login is also available through `auth.Client.Login`; the
password is sent only in the form request and is never stored. Authentication
matches lzc-cli 2.0.8: login returns `data.token`, validation uses
`X-User-Token`, and App Store calls use both `X-User-Token` and the
`userToken` cookie. `CopyImage` starts a server-side copy at the LazyCat
developer platform and polls its progress—it does not use local Docker or a
remote LazyCat device Docker daemon. Its result also includes the submitted
source image, selected platform, LazyCat registry image, and final layer
progress so CI/CD callers do not need to parse logs or rely on callbacks.

Submit an LPK to the developer platform directly from an `io.Reader`:

```go
result, err := client.Publish(ctx, appstore.PublishRequest{
    Package:    packageReader,
    FileName:   "application.lpk",
    Changelogs: map[string]string{"en": "Fix startup handling."},
})
if err != nil {
    return err
}
fmt.Println(result.Upload.Package, result.Upload.Version)
```

`Publish` spools the stream with a configurable size limit, parses and runs
official lint checks on the LPK, checks that the application exists, uploads
the package, validates the returned package identity, and submits it for
review. It never closes the caller-owned reader. Set `CreateIfMissing` and
provide `Application` to explicitly permit application creation; the library
never prompts interactively.

Trigger the Android APK shell endpoint without requiring App Store auth:

```go
apk, err := client.TriggerAPK(ctx, appstore.APKRequest{
    AppID: "cloud.lazycat.apps.example",
    Names: map[string]string{"zh": "示例", "en": "Example"},
    Icon:  iconReader,
})
```

This matches lzc-cli 2.0.8's unauthenticated multipart endpoint and five-second
default timeout. The typed result exposes the HTTP status and whether the
request was accepted or returned `304 Not Modified`.

## Authentication model

Device lifecycle access and developer-platform publishing use different
credentials, matching lzc-cli 2.0.8:

- Local LazyCat client discovery uses the LightOS ShellAPI files
  `shellapi_addr` and `shellapi_cred`. ShellAPI returns the default box, login
  UID, and client ID; the credential is sent only as `lzc-shellapi-cred` gRPC
  metadata.
- Build-remote lifecycle operations use SSH access to the configured build
  host plus the target LazyCat UID. The SDK never asks for or stores a device
  account password. SSH key authorization remains the caller's responsibility.
- Developer-platform operations (`Publish`, application creation, Testflight,
  and image copy) use a platform token. `auth.Client.Login` can exchange a
  username/password for `data.token`; when a store is configured, only the
  token is persisted. CI should inject an existing token through
  `LZC_CLI_TOKEN` or an explicit token store.
- APK triggering is the same unauthenticated shell endpoint used by lzc-cli.

This separation matters for CI/CD: a platform token is sufficient for App
Store submission and registry-side image copy, while deployment to a real box
also requires an independently authorized ShellAPI/SSH path.

## ShellAPI discovery and build-remote transport

ShellAPI discovery is optional and isolated in `remote/shellapi`:

```go
config, err := shellapi.LoadConfig(ctx, shellapi.ConfigOptions{})
if err != nil {
    return err
}
shell, err := shellapi.New(ctx, shellapi.Options{Config: config})
if err != nil {
    return err
}
defer shell.Close()

box, err := shell.DefaultBox(ctx)
clientID, err := shell.ClientID(ctx)
_, _ = box, clientID // box.LoginUser is the LazyCat UID; clientID is dev.id
```

`remote/ssh` implements lzc-cli's build-remote transport. It calls the remote
host's `lzc-docker`, which in turn executes
`/lzcapp/pkg/content/debug.bridge` inside
`cloudlazycatdevelopertools-app-1`. It does not use a local Docker daemon:

```go
target, err := ssh.ParseTarget("developer", "build-host.example:22")
if err != nil {
    return err
}
runner := ssh.New(ssh.Options{Target: target})
bridge := debugbridge.New(
    runner,
    runner.BridgeCommand,
    debugbridge.WithUID(box.LoginUser),
    debugbridge.WithHostCommand(runner.HostCommand),
)
```

All transport constructors accept injected executors or dial options for tests.
The SDK does not prompt, alter terminal state, or install SSH keys.

## Remote image build and project lifecycle

Use the DebugBridge client as the remote image backend without importing SSH
into the base `build` package:

```go
cache := blobcache.New(projectDir)
remoteImages := buildpack.New(bridge, cache)
var packageData bytes.Buffer
built, err := build.Build(ctx, &packageData, build.Request{
    Root:         projectDir,
    ConfigFile:   "lzc-build.dev.yml",
    ForceV2:      true,
    ImageBuilder: remoteImages,
})
```

The remote builder creates deterministic Docker contexts from Dockerfile
`COPY`/`ADD` sources, honors `.dockerignore`, uses the backend blob cache, and
materializes an OCI layout verified by `oci.Validate`.

Create the dependency-light project service and deploy a caller-owned LPK
reader:

```go
projects, err := project.New(project.Options{Backend: bridge})
if err != nil {
    return err
}
deployed, err := projects.Deploy(ctx, project.DeployRequest{
    Package:   bytes.NewReader(packageData.Bytes()),
    PackageID: built.Package,
    DevID:     clientID,
    UserApp:   false,
})
if err != nil {
    return err
}
running, err := projects.Start(ctx, project.StartRequest{
    AppID: built.Package, LocalVersion: built.Version,
})
_, _ = deployed, running
```

Backends before `1.0.4` wait for the app container before dev.id sync; newer
backends use pending sync. `Start`, `Stop`, `Wait`, and `Uninstall` expose
explicit state and deletion semantics.

Project command APIs stream through caller-owned readers and writers:

```go
tty := false
_, err = projects.Exec(ctx, project.ExecRequest{
    AppID: built.Package, Service: "app",
    Command: []string{"/bin/sh", "-lc", "go test ./..."},
    Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, TTY: &tty,
})
_, err = projects.Logs(ctx, project.LogRequest{
    AppID: built.Package, Stdout: os.Stdout, // follow defaults to true
})
_, err = projects.CopyTo(ctx, project.CopyRequest{
    AppID: built.Package, SourcePath: "./config", Destination: "/opt/app/config",
})
```

`Exec` defaults to service `app`, command `/bin/sh`, and workdir
`/lzcapp/cache/project-mirror`. `CopyTo` streams a TAR directly to
`lzc-docker cp -`; it does not create a temporary TAR file.

Use the optional `project/rsync` package for one-shot source synchronization:

```go
syncResult, err := projectrsync.Sync(ctx, projectrsync.Options{
    RootDir: projectDir,
    Target: projectrsync.Target{
        UID: box.LoginUser, Host: rsyncHost,
        PackageID: built.Package, UserApp: false,
        Directory: projectrsync.DefaultTarget,
    },
    Delete: true,
    Stdout: os.Stdout,
})
```

The adapter requires rsync `3.2.0+`, creates `.lzcdevignore` from lzc-cli's
defaults and `.gitignore`, and passes the daemon password only in the child
environment. `BuildTunnelArgs` produces the exact SSH local-forward argv for a
build-remote rsync target. Watch mode is intentionally caller-owned: invoke
`Sync` again from the filesystem watcher appropriate for your application.

For complete build → deploy → sync → start orchestration, import the optional
`workflow/project` package. It spools the LPK to a temporary file, returns
typed results for every completed stage, cleans the artifact on success and
failure, and emits safe `workflow.Event` values through an observer.

Lint an extracted package root:

```go
warnings, err := lint.Package(ctx, os.DirFS("package-root"))
_ = warnings
```

Enable LazyCat official/developer-platform checks only when preparing an
official submission:

```go
officialWarnings, err := lint.Package(ctx, os.DirFS("package-root"), lint.WithOfficial())
_ = officialWarnings
```

`WithOfficial` mirrors the optional lzc-cli `2.0.8` pre-publish preferences:
`registry.lazycat.cloud` image refs, `icon.png` presence/PNG/200 KiB limit,
`locales`, semver versions, devshell rejection, and embedded image blob checks.
Those warnings are not enabled by default because they do not necessarily mean
the LPK is uninstallable.

Sign and verify:

```go
signed, err := signature.SignFile(ctx, "app.signed.lpk", "app.lpk", signature.SignRequest{
    PrivateKeyPEM: privatePEM,
    PublicKeyPEM:  publicPEM,
    KeyID:         "dev",
})
_ = signed
verified, err := signature.VerifyFile(ctx, "app.signed.lpk", signature.VerifyRequest{KeyID: "dev"})
_ = verified
```

## Security

All blocking operations accept `context.Context` except key generation, which
only writes local key files. Stream APIs do not close caller-owned readers or
writers. Archive parsing and extraction enforce configurable size/path/count
limits and use Go 1.25 `os.Root` APIs for safe extraction.

Errors and workflow events must not contain passwords, tokens, private keys,
authorization headers, or raw credential-bearing responses.
