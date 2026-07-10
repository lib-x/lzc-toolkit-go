# lzc-toolkit-go

[English](README.md) | [简体中文](README.zh-CN.md)

基于 `@lazycatcloud/lzc-cli` `2.0.8` 实现的 LazyCat Go SDK，覆盖 LPK 读写、项目构建、镜像处理、开发者平台发布和远端项目生命周期。

本项目是库，不是对 lzc-cli 命令行的直接移植。各项能力按职责拆分为独立包。只需要解析 LPK、读取 Manifest、检查或签名时，不会引入 Docker、App Store、SSH、gRPC 等可选依赖。

## 兼容性

参考 CLI：

- 软件包：`@lazycatcloud/lzc-cli`
- 版本：`2.0.8`
- integrity：`sha512-CcH18fg1SBqTN4od7NCXMWYaAwjICgEuguphgNcb9Lp7v5+RDYa27+BEevC7faFFm8Zhjw3Rh/sinYc7fc39SA==`
- shasum：`af9fece8a9756a00e093f817b3c3083971cc171f`

SDK 以 lzc-cli 的文件格式和服务端协议语义为兼容目标，不要求生成字节完全相同的归档文件。

## 安装

```bash
go get github.com/lib-x/lzc-toolkit-go
```

## 基础用法

解析 Manifest：

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

把 LPK 写入任意 `io.Writer`：

```go
var out bytes.Buffer
result, err := lpk.Write(ctx, &out, lpk.WriteRequest{
    Layout: lpk.LayoutV2,
    Files:  os.DirFS("package-root"),
    Strict: true,
})
_ = result
```

从流中打开 LPK：

```go
reader, err := lpk.Open(ctx, bytes.NewReader(pkg))
if err != nil {
    return err
}
defer reader.Close()
effective, err := reader.EffectiveManifest(ctx)
```

已有随机访问能力时，可以用 `ReaderAt` 避免临时缓存整个输入流：

```go
reader, err := lpk.OpenReaderAt(ctx, bytes.NewReader(pkg), int64(len(pkg)))
if err != nil {
    return err
}
defer reader.Close()
```

文件便捷接口：

```go
result, err := lpk.WriteFile(ctx, "app.lpk", request)
info, err := inspect.File(ctx, "app.lpk")
_, _ = result, info
```

## 项目构建

直接把构建结果写入 `io.Writer`。除非请求明确允许，否则不会执行项目中的构建脚本：

```go
var output bytes.Buffer
result, err := build.Build(ctx, &output, build.Request{
    Root:       "./my-lzcapp",
    ConfigFile: "lzc-build.yml",
})
_ = result
```

`build.BuildFile` 使用原子替换写入文件。`build` 包兼容 lzc-cli 2.0.8 的项目收集逻辑，包括开发配置继承、Manifest 预处理、`contentdir`、`icon`、自动生成 `lzc-deploy-params.yml`、浏览器扩展、AI Pod 服务、`compose_override`、软件包字段覆盖和资源导出。

带镜像的项目必须显式配置本地或远端 `ImageBuilder`。未配置时，`build.Build` 返回 `INCOMPATIBLE_BACKEND`。

## OCI 和本地 Docker 镜像

检查解压后的 lzc-cli 镜像布局时，不需要引入 Docker：

```go
report, err := oci.Validate(ctx, os.DirFS("package-root"))
_ = report
```

`oci` 包还提供 `ReadLock`、`WriteLock`、`ReadIndex`、`WriteIndex` 和强类型 OCI 描述符。内嵌层的 blob 会通过流式 SHA-256 校验；上游层记录在 `images.lock` 中，不要求对应 blob 存在于 LPK 内。

镜像构建能力通过 `build.Request.ImageBuilder` 注入。适配器返回相对于软件包根目录的 `fs.FS`，`build` 使用 `oci.Validate` 校验，只复制 `images.lock` 和 `images/`，把 `embed:<alias>` 改写为最终镜像 ID，并将 `content.tar` 切换为 `content.tar.gz`。基础 `build` 包不会因此依赖 Docker 或远端传输实现。

显式启用本地 Docker 适配器：

```go
result, err := build.Build(ctx, dst, build.Request{
    Root:         "./my-lzcapp",
    ImageBuilder: dockerlocal.New(nil),
})
```

`dockerlocal.New(nil)` 使用 lzc-cli 2.0.8 支持的 Docker CLI 流程：`docker buildx build --load`、`docker image inspect`、`docker image save` 和尽力清理。测试可以注入 `dockerlocal.Engine`。仅导入 `build`、`lpk`、`oci` 或 `image` 不会执行或引入 Docker 适配器。

默认目标平台是 `linux/amd64`，可以通过 `dockerlocal.WithPlatform` 修改。

## 开发者平台认证和镜像复制

可以直接提供 CI/CD token，也可以从 `LZC_CLI_TOKEN` 或显式 token 存储中读取：

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

`auth.Client.Login` 支持用用户名和密码登录。密码只会出现在登录表单请求中，不会保存。认证行为与 lzc-cli 2.0.8 一致：登录响应返回 `data.token`，token 校验使用 `X-User-Token`，App Store 请求同时使用 `X-User-Token` 和 `userToken` Cookie。

`CopyImage` 请求 LazyCat 开发者平台在服务端复制镜像，并轮询复制进度。它不会调用本地 Docker，也不会调用微服设备上的 Docker。返回值包含源镜像、目标平台、LazyCat Registry 镜像地址和最终分层进度，CI/CD 不需要解析日志。

## 提交 LPK

直接从 `io.Reader` 向开发者平台提交 LPK：

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

`Publish` 会在可配置的大小限制内缓存输入流，解析 LPK，执行官方 lint，确认应用是否存在，上传软件包，校验服务端返回的软件包标识，再提交审核。它不会关闭调用方传入的 Reader。

如果允许自动创建不存在的应用，需要显式设置 `CreateIfMissing` 并提供 `Application`。SDK 不会发起交互式询问。

无需 App Store 认证即可触发 Android APK Shell 接口：

```go
apk, err := client.TriggerAPK(ctx, appstore.APKRequest{
    AppID: "cloud.lazycat.apps.example",
    Names: map[string]string{"zh": "示例", "en": "Example"},
    Icon:  iconReader,
})
```

该接口与 lzc-cli 2.0.8 的匿名 multipart 接口一致，默认超时为 5 秒。强类型返回值会提供 HTTP 状态，并说明请求是已受理还是返回 `304 Not Modified`。

## 认证模型

设备生命周期访问和开发者平台发布使用两套不同的凭据，与 lzc-cli 2.0.8 保持一致：

- 本地 LazyCat 客户端发现使用 LightOS ShellAPI 配置文件 `shellapi_addr` 和 `shellapi_cred`。ShellAPI 返回默认微服、登录 UID 和客户端 ID。凭据只作为 `lzc-shellapi-cred` gRPC metadata 发送。
- build-remote 生命周期操作使用目标构建主机的 SSH 授权和 LazyCat UID。SDK 不会要求或保存微服账户密码，SSH 公钥授权由调用方管理。
- `Publish`、应用创建、Testflight 和镜像复制使用开发者平台 token。`auth.Client.Login` 可以用用户名和密码换取 `data.token`；配置存储后，只保存 token。CI 建议通过 `LZC_CLI_TOKEN` 或显式 token 存储注入凭据。
- APK 触发接口与 lzc-cli 一样，不需要认证。

在 CI/CD 中，开发者平台 token 足以完成 App Store 提交和 Registry 服务端镜像复制。部署到真实微服时，还需要独立授权的 ShellAPI 或 SSH 访问路径。

## ShellAPI 发现和 build-remote 传输

ShellAPI 是可选能力，单独放在 `remote/shellapi` 包中：

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
_, _ = box, clientID // box.LoginUser 是 LazyCat UID，clientID 是 dev.id
```

`remote/ssh` 实现 lzc-cli 的 build-remote 传输。它调用远端主机上的 `lzc-docker`，再在 `cloudlazycatdevelopertools-app-1` 容器中执行 `/lzcapp/pkg/content/debug.bridge`，不会调用本地 Docker：

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

所有传输构造器都允许注入执行器或拨号参数，普通单元测试不需要连接远端主机。SDK 不会弹出提示、修改终端模式或安装 SSH 密钥。

## 远端镜像构建和项目生命周期

DebugBridge 客户端可以直接作为远端镜像后端，不会让基础 `build` 包依赖 SSH：

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

远端构建器根据 Dockerfile 的 `COPY` 和 `ADD` 来源生成确定性的 Docker context，支持 `.dockerignore`，使用后端 blob 缓存，并将结果落成通过 `oci.Validate` 检查的 OCI 布局。

创建轻量的项目生命周期服务，并部署调用方持有的 LPK Reader：

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

后端版本低于 `1.0.4` 时，SDK 会等待应用容器就绪后再同步 dev.id；新版本后端使用 pending sync。`Start`、`Stop`、`Wait` 和 `Uninstall` 都有明确的状态和数据删除参数。

项目命令通过调用方持有的 Reader 和 Writer 传递：

```go
tty := false
_, err = projects.Exec(ctx, project.ExecRequest{
    AppID: built.Package, Service: "app",
    Command: []string{"/bin/sh", "-lc", "go test ./..."},
    Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, TTY: &tty,
})
_, err = projects.Logs(ctx, project.LogRequest{
    AppID: built.Package, Stdout: os.Stdout, // Follow 默认为 true
})
_, err = projects.CopyTo(ctx, project.CopyRequest{
    AppID: built.Package, SourcePath: "./config", Destination: "/opt/app/config",
})
```

`Exec` 默认使用 `app` 服务、`/bin/sh` 命令和 `/lzcapp/cache/project-mirror` 工作目录。`CopyTo` 将 TAR 流直接传给 `lzc-docker cp -`，不会先创建临时 TAR 文件。

## rsync 项目同步

可选的 `project/rsync` 包提供单次源码同步：

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

该适配器要求 rsync `3.2.0+`，会根据 lzc-cli 默认规则和 `.gitignore` 创建 `.lzcdevignore`，并且只通过子进程环境变量传递 rsync daemon 密码。`BuildTunnelArgs` 可以生成 build-remote rsync 所需的 SSH 本地端口转发参数。

SDK 不内置文件监视器。需要 watch 模式时，由调用方选择合适的文件监视器，并在变化后再次调用 `Sync`。

如果需要完整的构建、部署、同步和启动编排，可以导入可选的 `workflow/project` 包。它会把 LPK 暂存到临时文件，返回各阶段的强类型结果，在成功或失败后清理临时文件，并通过 Observer 发送不含敏感信息的 `workflow.Event`。

## Lint

检查解压后的软件包目录：

```go
warnings, err := lint.Package(ctx, os.DirFS("package-root"))
_ = warnings
```

只有准备提交 LazyCat 官方开发者平台时，才启用官方检查：

```go
officialWarnings, err := lint.Package(ctx, os.DirFS("package-root"), lint.WithOfficial())
_ = officialWarnings
```

`WithOfficial` 对应 lzc-cli 2.0.8 发布前的可选偏好，包括：镜像必须使用 `registry.lazycat.cloud`、`icon.png` 必须存在且为不超过 200 KiB 的 PNG、必须提供 `locales`、版本号必须符合 SemVer、不能包含 devshell，并检查内嵌镜像 blob。

这些规则默认不启用，因为它们是官方平台提交要求，不一定代表 LPK 本身无法安装。

## 签名和校验

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

## 安全性

除只写入本地密钥文件的密钥生成外，所有阻塞操作都接收 `context.Context`。流式 API 不会关闭调用方持有的 Reader 或 Writer。

归档解析和解压支持可配置的大小、路径和条目数量限制，并使用 Go 1.25 的 `os.Root` API 限制所有文件系统修改范围。

错误和工作流事件不会包含密码、token、私钥、认证请求头或携带凭据的原始响应。
