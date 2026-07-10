# README 教程化重构设计

## 状态

已确认。

## 背景

当前中英文 README 主要按已经实现的包和 API 排列。它能帮助熟悉项目的人查接口，但第一次接触 LazyCat、LPK 或 lzc-cli 的读者缺少以下信息：

- LPK、Manifest、构建配置、镜像布局、DebugBridge 和开发者平台分别是什么。
- 本地构建、设备调试和平台发布之间是什么关系。
- 每段代码需要哪些输入、外部工具和认证凭据。
- 调用成功后会得到什么结果，失败时应该先检查什么。
- 零 Docker、带 Docker、远端预览和 CI/CD 分别应该从哪里开始。

## 目标读者

主要读者是会使用 Go，但不了解 LazyCat 应用开发流程的开发者。次要读者是需要把 LPK 构建和发布接入 CI/CD 的平台工程师。

README 不假设读者已经安装 lzc-cli、配置 Docker、登录开发者平台或连接懒猫设备。每个需要外部条件的例子都必须单独说明。

## 文档结构

中英文 README 使用相同的信息结构：

1. 项目是什么，以及它与 lzc-cli 的关系。
2. 安装方式和 Go 版本要求。
3. 核心概念词典。
4. 一张开发流程图，区分本地检查、设备预览和平台发布。
5. 按依赖由少到多排列的完整示例。
6. 包选择指南和高级能力参考。
7. 兼容版本、安全边界和限制。

中文以自然表达为准，英文保持相同事实和示例，不要求逐句直译。

## 核心概念

README 首次出现以下概念时必须解释：

| 概念 | 要回答的问题 |
|------|--------------|
| LPK | 它是什么文件，由谁安装，SDK 能对它做什么 |
| Manifest | 它描述哪些应用信息，与 LPK 的关系是什么 |
| `lzc-build.yml` | 它如何把项目目录、Manifest、内容和镜像组织成 LPK |
| LPK v1 和 v2 | 为什么库支持两种布局，普通用户何时需要关心 |
| OCI 镜像布局 | 为什么镜像会进入 LPK，哪些层会内嵌 |
| `ImageBuilder` | 为什么带镜像项目必须显式选择本地或远端构建后端 |
| 官方 lint | 为什么官方平台规则是可选检查，而不是所有 LPK 的通用有效性规则 |
| 开发者平台 token | 它用于哪些操作，如何通过账号密码或本地 lzc-cli 登录状态取得 |
| ShellAPI | 它如何发现默认微服、登录 UID 和 `dev.id` |
| DebugBridge | 它在远端构建、安装和应用控制链路中的位置 |
| `project` | 它如何在 DebugBridge 之上提供部署、启停、日志、命令和文件复制 |

## 开发流程图

README 使用一张小型文本流程图表达三条链路：

```text
本地项目
  -> build.Build
  -> LPK
     -> inspect / lint / signature        本地处理
     -> project + DebugBridge              部署到懒猫预览
     -> appstore.Publish                   提交开发者平台

带镜像项目
  -> dockerlocal 或 remote/buildpack
  -> OCI 镜像布局
  -> build.Build
  -> LPK
```

流程图只表达组件关系，不代替每个场景的前置条件和代码。

## 完整示例

### 示例一：纯本地构建、解析和检查

目标是让读者在没有 Docker、LazyCat 账号和设备的情况下完成第一次成功调用。

内容包括：

- 最小项目目录结构。
- 最小 `lzc-build.yml` 和 Manifest。
- 一个包含完整 imports、`context` 和错误处理的 Go 程序。
- 使用 `build.BuildFile` 生成 LPK。
- 使用 `inspect.File` 读取包信息。
- 使用 LPK Reader 读取有效 Manifest。
- 说明预期生成的文件和关键结果字段。

配置示例必须来自当前 schema 或测试夹具，不凭空添加字段。

### 示例二：使用本地 Docker 构建带镜像的 LPK

内容包括：

- Docker 和 `docker buildx` 前置条件。
- 带镜像项目的最小配置和 Dockerfile 关系。
- 通过 `dockerlocal.New(nil)` 注入 `ImageBuilder`。
- 默认 `linux/amd64` 行为和切换平台的方法。
- 解释 Docker 只在显式导入适配器后使用。
- 说明 `INCOMPATIBLE_BACKEND`、Docker 不可用和 OCI 校验失败分别意味着什么。

### 示例三：部署到懒猫设备预览

内容包括：

- 预览的准确含义是本地编辑源码，在懒猫容器中运行服务。
- ShellAPI 配置、目标主机 SSH 授权、Developer Tools 和后端版本等前置条件。
- ShellAPI 获取默认设备、UID 和 `dev.id`。
- SSH Runner 和 DebugBridge 的构造关系。
- 远端镜像构建、LPK 部署、启动、日志和停止的完整顺序。
- 可选 rsync 同步路径，以及库不内置文件监视器和自动重启策略。
- 明确当前 DebugBridge 不会把本机监听端口反向暴露给懒猫。

### 示例四：CI/CD 复制镜像并提交 LPK

内容包括：

- 账号密码换 token 和直接 token 两种认证方式。
- 本地已登录 lzc-cli 时，环境变量和配置文件的读取位置。
- CI Secret 到 `LZC_CLI_TOKEN` 的数据流。
- 一个可以单独运行的 `CopyImage` 完整例子，包含超时、分层进度回调和 LazyCat Registry 返回地址。
- 明确镜像复制只需要网络和开发者平台 token，本机不需要安装 Docker，也不需要连接懒猫设备、ShellAPI 或 SSH。
- 说明当前 lzc-cli 2.0.8 和 Go SDK 的镜像复制请求没有源 Registry 用户名或密码字段。需要认证的私有源不能通过该接口额外传入凭据，调用方必须确认开发者平台能够拉取源镜像。
- `Publish` 从 `io.Reader` 上传 LPK，并执行官方 lint。
- 说明镜像复制由开发者平台服务端完成，不调用本地或设备 Docker。
- 不在示例、日志或错误中打印 token 和密码。

## 每个示例的写法

每个完整示例按以下顺序组织：

1. 这个例子解决什么问题。
2. 运行前需要准备什么。
3. 项目或配置文件长什么样。
4. 可以复制使用的完整 Go 代码。
5. 成功后会得到什么。
6. 常见错误及对应检查项。

短 API 片段只能放在完整示例之后，不能再承担入门说明职责。

## 包选择指南

README 增加按任务选择包的表格。例如：

| 任务 | 导入包 |
|------|--------|
| 读写 LPK | `lpk` |
| 解析 Manifest | `manifest` |
| 构建项目 | `build` |
| 本地 Docker 镜像 | `image/dockerlocal` |
| 开发者平台发布 | `auth`、`appstore` |
| 设备发现 | `remote/shellapi` |
| DebugBridge 传输 | `remote/ssh`、`remote/debugbridge` |
| 应用生命周期 | `project` |
| 完整开发编排 | `workflow/project` |

表格解释职责，不重复列出所有函数。

## 事实和安全边界

- 所有示例以当前 Go API 和 lzc-cli 2.0.8 源码为依据。
- 不展示尚未实现的命令行封装、反向代理、文件监听器或交互式登录流程。
- 不把开发者平台 token、ShellAPI 凭据和 SSH 授权混为一类。
- 不暗示 DebugBridge 可以直接预览本机运行的 HTTP 服务。
- 不在 README 中加入真实账号、设备地址、token、密码或私钥。
- Reader 和 Writer 的所有权、是否关闭以及流式行为保持与 API 实现一致。

## 验证

重构完成后执行：

- 中英文标点检查。
- 中英文标题、概念和示例顺序一致性检查。
- Markdown 内部链接和语言切换链接检查。
- 示例中公开标识符的全仓库存在性检查。
- `git diff --check`。
- `go test ./... -count=1`。
- GitHub Actions 中的 Go 1.25、Go 1.26、race 和上游 lzc-cli 互操作检查。

## 不在本次范围内

- 修改 Go API 或实现新的 DebugBridge 能力。
- 增加 CLI。
- 增加真实 LazyCat 账号或设备的在线集成测试。
- 把 README 变成完整的 lzc-cli 或 LazyCat 平台手册。
