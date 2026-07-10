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
