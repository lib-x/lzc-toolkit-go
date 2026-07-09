# LPK Lifecycle Go Library Design

Date: 2026-07-10

Status: Approved

Module: github.com/lib-x/lpk-go

Root package: lpkgo

## 1. Purpose

This project provides a reusable Go library for the complete LazyCat LPK
lifecycle. It is based on the behavior and file formats implemented by
@lazycatcloud/lzc-cli, but it is a library rather than a command-line
application.

The library covers:

- project initialization and templates;
- build configuration discovery and merging;
- manifest preprocessing, loading, migration, and linting;
- project build scripts and content collection;
- LPK v1 and v2 packaging;
- LPK parsing, inspection, and safe extraction;
- resource-only packages;
- Docker and OCI image build, packing, caching, copying, and embedding;
- Ed25519 key generation, package signing, re-signing, and verification;
- LazyCat account login and token persistence;
- App Store image copying, image listing, pre-publishing, and publishing;
- ShellAPI and SSH remote access;
- project deploy, start, sync, exec, copy, log, install, and uninstall;
- typed lifecycle workflow orchestration;
- SDK and upstream compatibility version reporting.

## 2. Upstream compatibility baseline

The implementation baseline is the npm distribution inspected on
2026-07-10:

- package: @lazycatcloud/lzc-cli
- version: 2.0.8
- integrity:
  sha512-CcH18fg1SBqTN4od7NCXMWYaAwjICgEuguphgNcb9Lp7v5+RDYa27+BEevC7faFFm8Zhjw3Rh/sinYc7fc39SA==
- shasum: af9fece8a9756a00e093f817b3c3083971cc171f
- Node engine declared by upstream: >= 18
- inspected JavaScript surface: 13,428 lines under lib

The implementation is based on the npm-published source files, especially:

- lib/app/lpk_build.js
- lib/app/lpk_build_images.js
- lib/app/lpk_build_images_local.js
- lib/app/lpk_build_images_pack_local.js
- lib/app/lpk_embed_images.js
- lib/app/manifest_build.js
- lib/app/manifest_lint.js
- lib/lpk/core.js
- lib/lpk/resource_lint.js
- lib/package_info.js
- lib/project_blob_cache.js
- lib/sig/core.js
- lib/appstore/login.js
- lib/appstore/index.js
- lib/appstore/publish.js
- lib/appstore/prePublish.js
- lib/debug_bridge.js
- lib/shellapi.js
- lib/shellapi.proto
- lib/box and lib/box_key.js
- lib/app/project_runtime.js
- lib/app/project_deploy.js
- lib/app/project_sync.js
- the remaining project lifecycle modules under lib/app

Compatibility means:

1. Go can parse, inspect, lint, verify, and modify LPK files created by the
   reference CLI.
2. The reference CLI and LazyCat services can parse and consume LPK files
   created by Go.
3. Manifest, package metadata, images.lock, OCI blobs, signatures, and remote
   protocol semantics are compatible.
4. Byte-for-byte archive identity is not required.

## 3. Goals

### 3.1 Stable library API

The public API must be idiomatic Go, context-aware, typed, and suitable for
use by applications, services, and other SDKs. It must not expose CLI-only
behavior such as terminal prompts, process exit codes, global logger state,
or translated console messages.

### 3.2 Responsibility-aligned packages

The source is split according to the responsibilities present in the
reference lib directory. The implementation must not be placed into a
single large package or divided only into generic technical layers.

### 3.3 Full lifecycle and composability

Callers can use a high-level lifecycle manager or call individual packages
directly. Build, package, inspect, image, authentication, publishing, and
remote capabilities remain independently testable.

### 3.4 Explicit compatibility metadata

The SDK reports its own version and the exact lzc-cli version used as the
behavioral reference. Upgrading the reference version must be an explicit,
reviewable change with compatibility tests.

## 4. Non-goals

- Reproducing the lzc-cli command-line parser or interactive prompts.
- Reproducing localized CLI output.
- Requiring byte-identical ZIP or TAR output.
- Exposing an arbitrary DAG workflow framework.
- Depending on Node.js at runtime.
- Requiring Docker, SSH, or live LazyCat services for ordinary unit tests.
- Copying the reference CLI global configuration behavior into hidden
  process-wide state.

## 5. Package architecture

The module uses the following public package structure:

    github.com/lib-x/lpk-go
    ├── lpkgo lightweight shared contracts
    ├── version
    ├── workflow
    ├── lpk
    ├── project
    │   ├── template
    │   └── sync
    ├── manifest
    │   └── migrate
    ├── build
    ├── image
    │   ├── cache
    │   ├── docker
    │   └── remotebuild
    ├── archive
    ├── inspect
    ├── lint
    ├── signature
    ├── auth
    │   └── tokenfile
    ├── appstore
    ├── remote
    │   ├── shellapi
    │   └── ssh
    └── lifecycle
        ├── release
        ├── publish
        └── deploy

Internal packages contain implementation details that are not stable public
contracts, including archive helpers, YAML node merging, OCI fixture tools,
redaction, atomic files, command execution, and generated protocol glue.

The root lpkgo package contains only shared error, warning, and small common
contract types. It does not import build, image, App Store, gRPC, SSH, Docker,
or project lifecycle implementations.

Heavy or optional integrations live in leaf adapter packages. Importing
manifest, archive, lpk, inspect, or signature must not transitively import
gRPC, SSH, Docker adapters, App Store clients, or project synchronization
implementations.

### 5.1 Dependency direction

Dependencies are one-way:

    lifecycle/* -> workflow, build, appstore, project, remote
    build       -> manifest, lpk, image interfaces, lint
    inspect     -> lpk, manifest
    signature   -> lpk, archive
    adapters    -> their small parent interface packages
    leaf APIs   -> lpkgo shared contracts where required

The workflow package contains no build, network, archive, or project logic.
Lower-level format packages never depend on lifecycle packages. Parent
interface packages do not import their adapter subpackages.

### 5.2 Mapping from reference source

| Reference source | Go responsibility |
| --- | --- |
| app/lpk_build.js | build and workflow |
| manifest_build.js, manifest_lint.js, package_info.js | manifest and lint |
| lpk_build_images files, lpk_embed_images.js, project_blob_cache.js | image |
| lpk/core.js, resource_lint.js, archive helpers in utils.js | lpk, archive, inspect, lint |
| sig/core.js and sig/index.js | signature |
| appstore/login.js and config token behavior | auth |
| appstore publish, prePublish, image copy, APK generation | appstore |
| debug_bridge.js, shellapi files | remote |
| box, box_key, build_remote | remote/ssh and remote/shellapi |
| project runtime, deploy, start, exec, copy, log, sync | project using remote |
| lpk_create files and template directory | project/template |
| migrate/index.js | manifest/migrate |
| docker command wrappers | image and remote Docker capabilities |

CLI registration, i18n, and global loglevel behavior are not ported into the
core library.

## 6. Modular public APIs

There is no all-capabilities Manager in the root package. Callers import only
the responsibility they need:

    import "github.com/lib-x/lpk-go/manifest"
    import "github.com/lib-x/lpk-go/lpk"
    import "github.com/lib-x/lpk-go/inspect"

A caller that only parses an LPK therefore does not compile or link App
Store, gRPC, SSH, Docker, template, or synchronization code.

Focused APIs are provided by:

- project and project/template for project creation;
- manifest and manifest/migrate for configuration documents;
- build for project builds;
- lpk for constructing, opening, and extracting LPK containers;
- inspect for metadata and image summaries;
- lint for package and store warnings;
- signature for keys and signatures;
- image for pure image and OCI contracts;
- image/docker for a local Docker CLI adapter;
- image/remotebuild for a remote build adapter;
- auth and auth/tokenfile for sessions and token persistence;
- appstore for image copy, image listing, publishing, and APK generation;
- remote for transport-neutral remote contracts;
- remote/shellapi and remote/ssh for concrete remote transports;
- project/sync for watch and rsync synchronization.

Complete workflow facades are opt-in packages:

    lifecycle/release
    lifecycle/publish
    lifecycle/deploy

Each lifecycle constructor validates its own dependencies. For example,
lifecycle/release accepts build, image, signing, and observer dependencies
but does not require authentication or App Store dependencies.

## 7. Contract design rules

- Every blocking operation accepts context.Context.
- Input and output types are separate.
- Public operations return typed results rather than printing.
- Public APIs do not call os.Exit.
- Public APIs do not prompt on stdin or write terminal control sequences.
- Optional future fields are added without changing existing field meanings.
- External responses are treated as untrusted and validated at the boundary.
- Stable error and warning codes are the machine-readable contract.
- Error message wording is not a stable contract.
- LPK construction is based on io.Writer.
- LPK parsing accepts io.Reader.
- ReaderAt plus a known size is supported to avoid spooling when random
  access is already available.
- File-path APIs are convenience wrappers rather than the only API.
- The library never closes caller-owned readers or writers.
- Path-based output helpers use atomic replacement. A caller-provided writer
  can contain partial output when an operation fails.

## 8. Workflow design

The workflow package provides a typed linear pipeline:

    type Stage string

    type Step[S any] interface {
        Name() Stage
        Run(context.Context, S) error
    }

    type Pipeline[S any] struct {
        // ordered steps and observer
    }

    func NewPipeline[S any](steps ...Step[S]) *Pipeline[S]
    func (p *Pipeline[S]) Run(context.Context, S) error

Specific workflows define specific state types:

- ReleaseState
- PublishState
- DeployState
- InstallState

Different workflow states cannot be mixed. A caller can replace, insert, or
skip a stage only when using the matching state type.

The framework is intentionally linear. It does not provide reflection-based
registration, arbitrary graph scheduling, distributed execution, or an
untyped shared state map.

### 8.1 Events

Events are synchronous passive notifications:

    type Event struct {
        Stage      Stage
        Kind       EventKind
        Operation  string
        Message    string
        Current    int64
        Total      int64
        Attributes map[string]string
        Time       time.Time
    }

    type Observer interface {
        OnEvent(context.Context, Event)
    }

Events never contain passwords, tokens, private keys, authorization headers,
or raw credential-bearing responses. Cancellation is controlled only by the
context, not by observer return values.

## 9. Version and compatibility reporting

The version package exposes:

    const SDKVersion = "0.1.0"
    const ReferenceCLIPackage = "@lazycatcloud/lzc-cli"
    const ReferenceCLIVersion = "2.0.8"
    const ReferenceCLIIntegrity = "sha512-CcH18fg1SBqTN4od7NCXMWYaAwjICgEuguphgNcb9Lp7v5+RDYa27+BEevC7faFFm8Zhjw3Rh/sinYc7fc39SA=="
    const ReferenceCLIShasum = "af9fece8a9756a00e093f817b3c3083971cc171f"

    func Current() Info

Info reports:

- SDK source version;
- Go module version when available from build information;
- reference package and version;
- integrity and shasum;
- supported LPK layouts and archive formats;
- backend capability requirements.

The known backend requirements are:

- LPK v2 support: backend >= 1.0.0
- pending sync dev ID: backend >= 1.0.4
- build pack context cache: backend >= 1.0.4
- blob manifest transport: backend >= 1.0.5

An upstream compatibility document records which source files and tests map
to each Go package. Changing ReferenceCLIVersion requires updating that
document and passing the bidirectional compatibility suite.

## 10. Manifest and package metadata

### 10.1 Documents

The manifest package exposes a typed Manifest model and a Document wrapper.
Document retains an internal YAML syntax tree so unknown fields, comments
where feasible, and unsupported future fields survive parse-modify-marshal
operations.

The YAML implementation type is not exposed in the public API. This avoids
making a YAML dependency part of the public contract.

### 10.2 package.yml

The static package fields are:

- package
- version
- name
- description
- author
- license
- homepage
- min_os_version
- unsupported_platforms
- locales

For LPK v2 these fields are stored in package.yml and removed from
manifest.yml. Loading an effective manifest merges them for consumers.
Legacy v1 top-level fields remain readable.

The library supports:

- loading manifest and package files independently;
- loading an effective merged manifest;
- splitting an effective manifest back into v2 files;
- package_override with top-level replacement semantics;
- the legacy pkg_id and pkg_name aliases as compatibility inputs;
- reporting deprecated static fields as warnings.

### 10.3 Build directives

Manifest preprocessing supports the reference syntax:

- #@build if profile = value
- #@build if profile != value
- #@build if env.NAME
- #@build if env.NAME = value
- #@build if env.NAME != value
- #@build else
- #@build end
- #@build include path

Included files cannot contain nested build directives, matching the
reference behavior. Invalid, duplicated, unmatched, or unclosed directives
produce INVALID_MANIFEST errors with source file and line information.

### 10.4 Unknown fields and linting

Unknown fields are preserved but produce warnings where the reference CLI
would warn. Deprecated application, service health-check, and ext_config
fields keep their reference warning codes and semantics.

## 11. Build configuration and project creation

### 11.1 Configuration discovery

Release builds use lzc-build.yml by default. Development project operations
prefer lzc-build.dev.yml when present.

When the selected build configuration is not lzc-build.base.yml, the library
looks for lzc-build.base.yml in the same directory. Base and selected
configuration maps use top-level replacement semantics, with the selected
configuration taking precedence.

### 11.2 Build configuration support

The build package supports the reference fields, including:

- buildscript
- manifest
- contentdir
- pkgout
- lpkPath
- icon
- deploy_params
- browser-extension
- ai-pod-service
- compose_override
- envs
- package_override
- images
- resource_exports

Removed reference fields such as embed_images, embed_all_images, and the
old global upstream settings return explicit compatibility errors.

### 11.3 Environment behavior

Library callers provide build environment variables explicitly. The build
request can opt into inheriting the current process environment and can
provide a LocalIP resolver for reference-compatible template variables.

LZC_VERSION behavior is represented by an explicit VersionOverride field.
Reading LZC_VERSION from the process environment only occurs when process
environment inheritance is enabled.

### 11.4 Build scripts

Build scripts run through an injected CommandRunner. The default runner uses
context-aware subprocess execution and preserves the reference shell
semantics:

- sh -c on Unix-like systems;
- cmd /c on Windows.

The caller can replace the runner to sandbox commands, collect output, or
disable command execution.

### 11.5 Project templates

The project/template package supports the reference project types and uses
embedded templates derived from the published template directory:

- blank
- golang
- python
- springboot
- vue
- vue-minidb
- gui-vnc

Template creation is non-interactive. The request specifies the template,
destination, project name, package name, and variables.

## 12. LPK packaging

### 12.1 Layout selection

The builder uses LPK v2 when any of these conditions is true:

- ForceV2 is enabled;
- package.yml exists;
- images are configured;
- build env entries are configured;
- package metadata overrides are configured;
- the package is resource-only.

Otherwise it can create the legacy LPK v1 layout.

### 12.2 LPK v1

- archive format: ZIP;
- static package fields may remain in manifest.yml;
- reproducible ZIP entry date: 1980-01-01T00:00:00Z;
- output remains readable by reference lzc-cli.

### 12.3 LPK v2

- archive format: TAR;
- package.yml is required;
- static metadata is removed from manifest.yml;
- images are represented as an OCI image layout under images;
- image metadata is stored in images.lock;
- resource-only packages omit manifest.yml and contain package.yml plus
  exports.

### 12.4 Collected package content

The builder supports:

- content.tar for contentdir;
- content.tar.gz when image-bearing v2 behavior requires compression;
- icon.png;
- deploy_params.yml;
- extension.zip;
- ai-pod-service;
- compose.override.yml;
- exports for resource-only or resource-export packages.

Resource export kind and ID names use lowercase letters, digits, period,
hyphen, and underscore. Hidden names, files in place of resource
directories, empty payloads, duplicates, and more than 100 kinds are
rejected or warned consistently with the reference rules.

### 12.5 Reproducibility

Archive entries are sorted. Timestamps, owners, groups, and platform-specific
metadata are normalized where the format permits. Building identical input
twice must produce the same digest in Go, even though that digest is not
required to match the Node archive byte-for-byte.

### 12.6 Writer-based construction

LPK construction is centered on io.Writer:

    func Write(
        context.Context,
        io.Writer,
        WriteRequest,
    ) (WriteResult, error)

    func WriteFile(
        context.Context,
        string,
        WriteRequest,
    ) (WriteResult, error)

WriteRequest contains:

- the requested v1 or v2 layout;
- an fs.FS containing the completed package root;
- archive and reproducibility options;
- validation policy.

The lpk package validates required files and layout rules before writing as
far as possible. WriteResult reports the selected layout, archive format,
byte count, and sha256 digest.

Write never closes the caller-provided writer. A writer can receive partial
data before an error is discovered. WriteFile stages the output and atomically
renames it only after successful validation and archive completion.

The build package exposes matching writer-first operations:

    func Build(
        context.Context,
        io.Writer,
        Request,
    ) (Result, error)

    func BuildFile(
        context.Context,
        string,
        Request,
    ) (Result, error)

Build prepares the package root and delegates final container encoding to
lpk.Write.

## 13. Archive and inspection

The archive package provides:

- format detection using filename hints and magic bytes;
- ZIP and TAR packing to io.Writer;
- safe full extraction;
- safe selected-entry extraction;
- atomic output replacement;
- configurable extraction limits.

### 13.1 Reader-based parsing

The lpk package accepts any io.Reader:

    func Open(
        context.Context,
        io.Reader,
        ...OpenOption,
    ) (*Reader, error)

    func OpenReaderAt(
        context.Context,
        io.ReaderAt,
        int64,
        ...OpenOption,
    ) (*Reader, error)

    func OpenFile(
        context.Context,
        string,
        ...OpenOption,
    ) (*Reader, error)

Open consumes the input stream and, when random access is required, spools it
to a bounded temporary file. It does not require the complete package to be
held in memory. OpenReaderAt avoids that copy when the caller already has
random access and a known size.

The returned lpk.Reader provides:

- detected archive format and LPK layout;
- entry listing and selected entry opening;
- effective manifest and package metadata loading;
- safe extraction;
- access for inspect, lint, signature, and image operations.

The lpk.Reader owns any temporary spool or file handle it creates and
implements io.Closer. It never closes the io.Reader or io.ReaderAt supplied by
the caller.

Open applies configured maximum input, entry count, path, and extracted-size
limits while spooling and parsing.

### 13.2 Inspection

The inspect package reports:

- resolved package path and size;
- ZIP or TAR format;
- LPK v1 or v2;
- effective package ID and application version;
- presence of manifest.yml and package.yml;
- resource-only status;
- presence of META signature data;
- images directory and images.lock status;
- aliases, image IDs, upstream references, layer sources, layer counts,
  embedded bytes, missing embedded blobs, and unique blob totals.

The initial Signed field means signature metadata exists, matching upstream
inspection behavior. Cryptographic validity is reported separately by
signature.Verify.

Inspection accepts an existing lpk.Reader and also provides io.Reader,
io.ReaderAt, and file-path convenience functions. The functions returning a
fully materialized Info value close their internal temporary reader before
returning.

## 14. Image lifecycle

### 14.1 Image configuration

Each image alias supports:

- builder: local or remote;
- context;
- dockerfile;
- dockerfile-content;
- upstream-match;
- other reference-compatible build inputs discovered in the 2.0.8 source.

The manifest refers to packaged images with:

    embed:alias

After building, the reference becomes:

    embed:alias@sha256:<image-id>

Unresolved aliases are errors.

### 14.2 Local building

The local image engine:

1. verifies Docker Buildx compatibility;
2. obtains the target linux platform from the selected remote when needed;
3. builds the image;
4. reads local builder metadata to identify parent and upstream layers;
5. saves selected images as a Docker archive;
6. converts selected image data into an OCI layout;
7. compresses embedded layers;
8. writes images.lock;
9. removes temporary images when cleanup is enabled.

### 14.3 Remote building

The remote image engine:

1. creates a Docker build context using Docker ignore behavior;
2. uploads or streams the context through the remote backend;
3. uses backend build and pack operations;
4. applies backend feature gates;
5. transfers blobs using the best supported transport;
6. writes the same OCI and images.lock representation as local building.

### 14.4 OCI and images.lock

The OCI layout contains:

- images/oci-layout
- images/index.json
- images/blobs/sha256/<digest>

images.lock records each alias, image_id, upstream, and layers. Each layer
records a sha256 digest and source value:

- embed
- upstream

Only embedded blobs must be present locally. Missing embedded blobs are
errors for build and publish checks.

### 14.5 Upstream matching and embedding

upstream-match walks the parent image chain to find a matching upstream.
Layers above the matched image are embedded. If no match exists, the image
is fully embedded.

Embedding an existing LPK can target selected aliases or all aliases.
Missing upstream blobs are fetched through the remote image runtime. The
operation rewrites images.lock and repacks the archive atomically.

Embedding supports stream and file forms:

    func Embed(
        context.Context,
        io.Writer,
        io.Reader,
        EmbedRequest,
    ) (EmbedResult, error)

    func EmbedFile(
        context.Context,
        string,
        string,
        EmbedRequest,
    ) (EmbedResult, error)

The stream form does not close caller-owned inputs or outputs. It uses the
same bounded spooling behavior as lpk.Open. The file form supports atomic
in-place replacement and distinct input and output paths.

### 14.6 Blob cache

The project cache location remains compatible:

    <project>/.lzc-cli-cache/blobs/sha256/<digest>

Cache writes use a temporary file and atomic rename. Blob digests are
validated before creating filesystem paths. Existing valid blobs are reused.

## 15. Signature lifecycle

The signature package supports Ed25519.

The primary signing and verification APIs accept streams:

    func Sign(
        context.Context,
        io.Writer,
        io.Reader,
        SignRequest,
    ) (SignResult, error)

    func Verify(
        context.Context,
        io.Reader,
        VerifyRequest,
    ) (VerifyResult, error)

File helpers provide atomic in-place signing and direct file verification.
The stream functions do not close caller-owned readers or writers.

Key generation writes:

- <name>.ed25519.private.pem using PKCS8 and file mode 0600;
- <name>.ed25519.public.pem using SPKI.

Signing:

1. detects the existing package archive format;
2. extracts the package safely;
3. rejects an existing signature unless Resign is true;
4. removes META when re-signing;
5. hashes every regular file outside META in sorted POSIX path order;
6. writes META/release.lock;
7. signs the exact release.lock bytes;
8. writes META/keys/<key-id>.pub;
9. writes META/signatures/<key-id>.sig;
10. repacks using the original archive format.

release.lock uses schema lazycat.lpk.release-lock/v1.
Signature metadata uses schema lazycat.lpk.signature/v1 and algorithm
ed25519.

Verification checks:

- release.lock schema;
- listed file paths, sizes, and sha256 digests;
- missing and unexpected non-META files;
- key ID and public key;
- signature schema and signed_file;
- Ed25519 signature validity.

## 16. Authentication

The auth package provides:

    type Credentials struct {
        Username string
        Password string
    }

    type TokenStore interface {
        Load(context.Context) (string, error)
        Save(context.Context, string) error
        Delete(context.Context) error
    }

Provided stores:

- in-memory store;
- atomic file store with mode 0600.

The password exists only in the login request and is never stored in Session.

Default account endpoint:

    https://account.lazycat.cloud

Login behavior:

- POST /api/login/signin
- application/x-www-form-urlencoded username and password
- validate success and token fields before storing

Session validation:

- GET /api/user/current
- X-User-Token header

Authenticated App Store requests use:

- X-User-Token header;
- userToken cookie.

Pre-publish requests additionally use:

- Authorization: Bearer <token>

The library never recursively prompts for credentials. Missing or invalid
credentials return UNAUTHENTICATED.

## 17. App Store and image copying

Default App Store endpoint:

    https://appstore.api.lazycat.cloud

Default pre-publish endpoint:

    https://testflight.lazycat.cloud/api

All endpoints can be overridden for testing or private deployments.

### 17.1 Image copy

CopyImage starts a server-side copy using the reference v3 endpoint and
polls the progress endpoint for the same source image and platform.

The request includes:

- source image;
- platform, including amd64 and arm64;
- polling interval;
- optional operation timeout.

Progress events report per-layer hash and percentage. The result returns the
LazyCat registry image reference. Server-reported copy errors are returned as
structured remote errors.

ListImages calls the reference myimages endpoint and returns typed image
records sorted by update time without printing a table.

### 17.2 Publishing

Publish performs:

1. LPK extraction and store lint checks;
2. rejection of devshell packages;
3. icon.png presence, PNG validation, and size checks;
4. manifest, package version, locales, image registry, and embedded blob
   checks;
5. authenticated application existence check;
6. optional application creation using explicit request metadata;
7. multipart LPK upload;
8. validation of the upload response;
9. submission of review metadata and localized changelogs;
10. typed publish result.

The request must include changelogs. It must also include application
creation metadata when CreateIfMissing is enabled. The library never prompts
for missing information.

### 17.3 Pre-publishing

PrePublish:

- lists available test groups;
- requires an explicit group ID in the publish request;
- uploads the LPK and changelog as multipart data;
- uses the stored token as a Bearer token;
- returns the parsed server result.

### 17.4 APK generation

The appstore package exposes the APK generation capability implemented by
the reference apkshell module. It accepts explicit package information and
icon input and returns a typed job or result rather than printing progress.

## 18. Remote runtime

The remote package exposes high-level capabilities while keeping transport
details replaceable.

Transport implementations:

- remote/shellapi for the ShellCore gRPC API and DialBoxService;
- remote/ssh for direct SSH access to lzcos and the debug bridge.

The remote service supports:

- box selection and platform discovery;
- backend version discovery;
- developer-tools availability checks;
- install and uninstall;
- deploy status and application information;
- pending sync dev ID;
- pause and resume;
- development shell;
- remote Docker and Docker Compose;
- image pull, save, inspect, build, and pack;
- blob check, put, and get;
- remote file reads;
- project sync support.

SSH key handling:

- uses a managed Ed25519 key pair;
- private keys are mode 0600;
- key comments are sanitized;
- remote command arguments use a tested shell escaping function;
- multiplexing sockets are cleaned up.

The ShellAPI protobuf contract is represented by generated Go code. Generated
code is not hand-edited.

## 19. Project runtime

The project package owns project-oriented operations and depends on remote
capabilities rather than implementing transports.

It supports:

- selecting development or release build configuration;
- resolving the project working directory;
- creating a force-v2 development package;
- deploy and startup-state polling;
- reporting deployment and container status;
- starting or resuming a project;
- executing commands in a selected service and work directory;
- copying files using TAR streams;
- streaming logs;
- initial and watch-mode synchronization;
- ignore rules from .gitignore and .lzcdevignore;
- rsync and SSH tunnel operation through injected runners;
- ensuring the project is deployed and running before dependent actions.

Watch synchronization batches changes and supports cancellation through the
context. It does not leak goroutines after cancellation.

## 20. Errors and warnings

All packages use a shared structured error type:

    type Error struct {
        Code       Code
        Op         string
        Stage      workflow.Stage
        Path       string
        StatusCode int
        Retryable  bool
        Cause      error
    }

Stable codes include:

- INVALID_ARGUMENT
- INVALID_CONFIG
- INVALID_MANIFEST
- UNSUPPORTED_FORMAT
- INCOMPATIBLE_BACKEND
- UNAUTHENTICATED
- PERMISSION_DENIED
- NOT_FOUND
- CONFLICT
- COMMAND_FAILED
- REMOTE_UNAVAILABLE
- INTEGRITY_MISMATCH
- CANCELLED

The type supports errors.Is, errors.As, and Unwrap.

Lint and compatibility issues use:

    type Warning struct {
        Code     string
        Severity Severity
        Path     string
        Message  string
    }

Warnings do not become execution errors unless a request explicitly enables
strict handling.

## 21. Intentional library differences

The following differences from the CLI are required:

- thrown strings, undefined returns, and process.exit become Go errors;
- invalid login never triggers recursive interactive prompting;
- missing changelogs, group IDs, or creation metadata return validation
  errors;
- an application-existence network failure returns an error instead of
  silently assuming the application exists;
- Signed reports signature metadata presence, while Verify reports
  cryptographic validity;
- archive extraction is hardened;
- overwrite, signing, and embedding use atomic replacement;
- automatic retries are limited to idempotent reads and progress polling;
- observer events, errors, and logs are redacted;
- process environment and CLI global configuration are not read unless the
  caller opts in.

These differences do not change the LPK format or LazyCat service protocols.

## 22. Security and resource limits

### 22.1 Archive safety

Extraction rejects:

- absolute paths;
- parent traversal;
- Windows drive and UNC escapes;
- NUL-containing paths;
- symlinks or hard links escaping the extraction root;
- duplicate entries that would change file type unexpectedly.

Configurable limits cover:

- maximum entry count;
- maximum individual file size;
- maximum total extracted size;
- maximum manifest and JSON document size;
- maximum path length.

### 22.2 Files and credentials

- sensitive files use mode 0600;
- outputs use temporary files and atomic rename;
- temporary directories are removed on success, error, and cancellation;
- private keys, passwords, tokens, cookies, and authorization headers are
  redacted;
- digest-derived paths require validated sha256 values.

### 22.3 Network safety

- HTTP response bodies have size limits;
- JSON and status fields are validated before use;
- redirects use the caller-provided HTTP client policy;
- retry policies use bounded attempts and context-aware backoff;
- non-idempotent login, upload, publish, install, and deploy actions are not
  retried automatically.

### 22.4 Command execution

- subprocesses use context-aware execution;
- arguments are passed separately except for explicit compatibility shell
  scripts;
- shell execution is visible in request options and events;
- callers can replace the runner with a sandboxed implementation;
- cancellation terminates child processes and cleans up tunnels.

## 23. Testing strategy

### 23.1 Unit tests

Unit tests cover:

- build configuration merge behavior;
- template environment handling;
- manifest directives with file and line errors;
- effective manifest and package.yml split/merge;
- unknown-field preservation;
- v1 and v2 layout selection;
- resource-only packages;
- deterministic ZIP and TAR output;
- archive safety limits;
- OCI layout and images.lock;
- local Docker archive conversion;
- upstream layer selection;
- blob cache atomicity and validation;
- signature generation and verification;
- writer-based LPK construction;
- reader and ReaderAt based LPK parsing;
- stream-based signing and image embedding;
- stable error codes and event ordering;
- credential redaction.

### 23.2 Bidirectional reference compatibility

CI installs exactly @lazycatcloud/lzc-cli@2.0.8 and validates:

    Node creates LPK -> Go inspects, lints, and verifies
    Go creates LPK   -> Node lpk info and lpk lint accept it

Fixtures cover:

- LPK v1 ZIP;
- LPK v2 TAR;
- package.yml;
- no-image v2;
- mixed upstream and embedded image layers;
- fully embedded image;
- resource-only package;
- signed package;
- manifest directives;
- unknown manifest fields.

Ordinary Go tests use committed fixtures under:

    testdata/upstream/2.0.8

Node is required only for the fixture regeneration and interoperability CI
job, not for library use or normal Go unit tests.

### 23.3 Protocol tests

- httptest.Server covers login, token validation, image copy, image listing,
  application checks, creation, upload, review submission, pre-publish, and
  APK generation.
- An in-memory gRPC server covers ShellAPI behavior.
- A fake command runner covers SSH, Docker, Docker Compose, and rsync
  argument construction and error mapping.
- OCI fixtures replace real Docker in unit tests.

### 23.4 Integration and fuzz testing

Required checks:

- go test ./...
- go test -race ./...
- fuzz manifest YAML;
- fuzz images.lock;
- fuzz ZIP and TAR parsers;
- fuzz App Store JSON responses;
- fuzz remote command argument escaping.

Docker, SSH, and live LazyCat integration tests require explicit environment
variables and are excluded from the default test command.

### 23.5 Version tests

Tests assert that version.Current reports:

- SDK source version 0.1.0;
- reference package @lazycatcloud/lzc-cli;
- reference version 2.0.8;
- the exact integrity and shasum in this document;
- the four known backend capability minimum versions.

## 24. Delivery sequence

The implementation is delivered in independently testable milestones.

### Milestone 1: Format foundation

- module setup;
- shared errors and observers;
- version metadata;
- archive;
- manifest and package metadata;
- inspect and lint;
- signature;
- upstream format fixtures.

This milestone can parse, inspect, lint, sign, verify, and safely repack LPK
files without Docker or network access.

### Milestone 2: Build and images

- project templates;
- build configuration;
- build scripts and content collection;
- OCI image processing;
- local and remote image interfaces;
- blob cache;
- LPK build and release workflows.

This milestone can create compatible v1, v2, resource-only, and image-bearing
LPK packages.

### Milestone 3: Authentication and App Store

- token stores;
- login and session validation;
- image copy and image listing;
- pre-publish;
- publish;
- APK generation;
- HTTP protocol compatibility tests.

### Milestone 4: Remote project lifecycle

- ShellAPI generated client;
- SSH provider;
- backend capability gates;
- install and uninstall;
- project deploy, start, info, exec, copy, logs, and sync;
- remote Docker and blob transport;
- end-to-end workflow composition.

Each milestone must pass all earlier milestone tests.

## 25. Acceptance criteria

The project is complete when:

1. All public methods are context-aware and documented.
2. Package responsibilities match this design.
3. Go parses all committed upstream 2.0.8 fixtures.
4. Reference lzc-cli accepts Go-generated v1 and v2 fixtures.
5. Image-bearing packages have valid OCI layouts and images.lock data.
6. Resource-only LPK files pass reference-compatible linting.
7. Signatures produced by Go verify in Go, and signatures from compatible
   fixtures verify in Go.
8. Login, image copy, pre-publish, and publish pass mock protocol tests.
9. ShellAPI, SSH, Docker, and sync behavior pass transport tests.
10. Default tests require no live LazyCat account, Docker daemon, or box.
11. Race tests pass.
12. Security tests reject archive escape and resource exhaustion fixtures.
13. version.Current reports the approved SDK and reference metadata.
14. No public library operation prompts, exits the process, or mutates global
    logging state.
15. LPK construction works with bytes.Buffer and an ordinary io.Writer
    without requiring a destination path.
16. LPK parsing works with bytes.Reader, a non-seekable io.Reader, an
    io.ReaderAt plus size, and a file path.
17. Importing manifest, archive, lpk, inspect, or signature does not pull in
    gRPC, SSH, Docker adapter, App Store, or project synchronization packages.
18. All implementation code and unit tests are committed and pushed to the
    configured origin after the complete verification suite passes.

## 26. Version control handoff

The approved implementation workflow ends with:

1. complete all production code;
2. complete all unit, compatibility, race, and applicable integration tests;
3. run the final verification suite and inspect the repository status;
4. commit the implementation and tests with clear commit messages;
5. push the current branch to the configured origin;
6. report the pushed branch, commit IDs, and verification commands.

No completion claim is made before the push succeeds.
