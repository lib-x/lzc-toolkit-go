# Local LazyCat Project Inspection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a stable local `project.Inspect` API and reusable non-executing Go Template Manifest analysis, then make project building use the same source analysis.

**Architecture:** `manifest.Analyze` lexically projects Go Template actions into YAML-safe markers, exposes normalized source facts, and restores exact actions without evaluating them. `build.ResolveConfig` centralizes the existing multi-pass effective build configuration, while `project.Inspect` combines that configuration with package and Manifest analysis into deterministic JSON-ready project information. `build` and `lpk` then replace raw brace detection and line-oriented fake metadata with the shared analysis.

**Tech Stack:** Go 1.25+, `go.yaml.in/yaml/v3`, existing `manifest.Document`, `build.LoadConfig`, `lpk.Layout`, table-driven tests, golden JSON fixtures.

## Global Constraints

- Never execute or render repository-provided Go Templates.
- Preserve exact standalone and inline template actions, including whitespace and trim markers.
- Support plain YAML, LPK v1, LPK v2, development profiles, and resource-only projects.
- Keep `manifest.Parse`, `build.LoadConfig`, `build.Build`, `project.Service.Info`, `inspect.Info`, and `lpk.WriteRequest` backward compatible.
- `project.Inspect` must never run build scripts, build images, write packages, contact devices, or call app stores.
- Do not expose environment values, deployment values, scripts, inject bodies, commands, setup scripts, or raw template actions in inspection results or errors.
- All public slices and maps returned by new APIs must be defensive copies and deterministically sorted.
- Project-controlled paths returned by inspection must remain beneath the resolved project root and reject existing symlink components.
- Package ID, version, and other identity fields must be static; never guess templated identity values.
- Use `schemaVersion: 1` for the initial `project.Inspection` JSON contract.

---

## File Structure

- `manifest/template.go`: lexical template scanner, marker projection, exact restoration, `Analysis`, and `TemplateInfo`.
- `manifest/template_test.go`: template projection, restoration, security, and malformed-input tests.
- `manifest/summary.go`: normalized package/application/service/image facts and diagnostics extracted from the projected YAML node tree.
- `manifest/summary_test.go`: deterministic summary and secret-exclusion tests.
- `build/project_config.go`: effective multi-pass config resolution shared by build and project inspection.
- `build/project_config_test.go`: environment precedence, metadata expansion, defensive-copy, and cancellation tests.
- `project/inspect.go`: local project path validation, preprocessing, effective metadata, layout prediction, and inspection assembly.
- `project/inspect_test.go`: v1/v2/resource-only/profile/template/project-kind/golden JSON tests.
- `project/types.go`: additive local inspection request/result types; existing remote lifecycle types remain unchanged.
- `internal/packageid/packageid.go`: one internal package-ID validator shared by build, lint, and project inspection.
- `internal/packageid/packageid_test.go`: valid/invalid package ID contract.
- `build/metadata.go`, `build/build.go`, `build/options.go`: consume shared analysis/config/layout helpers and delete heuristic template metadata extraction.
- `lpk/validate.go`: validate allowed templated Manifests through `manifest.Analyze` rather than raw brace detection.
- `README.md`, `README.zh-CN.md`: public local project inspection and template-analysis examples.

---

### Task 1: Add non-executing Manifest template projection

**Files:**
- Create: `manifest/template.go`
- Create: `manifest/template_test.go`

**Interfaces:**
- Produces: `func Analyze([]byte) (*Analysis, error)`.
- Produces: `func (*Analysis) Document() *Document`.
- Produces: `func (*Analysis) Template() TemplateInfo`.
- Produces: `func (*Analysis) Restore([]byte) ([]byte, error)`.
- Produces: `TemplateInfo` with JSON fields `present`, `controlCount`, `expressionCount`, `hasConditionalBlocks`, `hasInlineConditions`, and `actionKinds`.

- [ ] **Step 1: Write the failing projection and restoration tests**

Create `manifest/template_test.go` with public-package tests using this source:

```go
func TestAnalyzeProjectsAndRestoresLazyCatTemplates(t *testing.T) {
    t.Parallel()
    source := []byte(`{{ if .U.target }}
usage: "to {{ .U.target }}"
{{ else }}
usage: "netmap"
{{ end }}
application:
  subdomain: netmap
  ingress:
    - port: {{ index .U "listen.port" }}
services:
  app:
    image: registry.lazycat.cloud/example/app:1.0.0
    environment:
      - SECRET={{ stable_secret "app_secret" }}
      - URL={{if .U.base_url}}{{.U.base_url}}{{else}}https://{{.S.AppDomain}}{{end}}
`)

    analysis, err := manifest.Analyze(source)
    if err != nil {
        t.Fatal(err)
    }
    info := analysis.Template()
    if !info.Present || info.ControlCount != 3 || !info.HasConditionalBlocks || !info.HasInlineConditions {
        t.Fatalf("Template() = %#v", info)
    }
    projected, err := analysis.Document().Bytes()
    if err != nil {
        t.Fatal(err)
    }
    if bytes.Contains(projected, []byte(".U.target")) || bytes.Contains(projected, []byte("stable_secret")) {
        t.Fatalf("projection leaked template bodies:\n%s", projected)
    }
    restored, err := analysis.Restore(projected)
    if err != nil {
        t.Fatal(err)
    }
    for _, action := range [][]byte{
        []byte(`{{ if .U.target }}`),
        []byte(`{{ .U.target }}`),
        []byte(`{{ index .U "listen.port" }}`),
        []byte(`{{ stable_secret "app_secret" }}`),
        []byte(`{{if .U.base_url}}`),
        []byte(`{{.S.AppDomain}}`),
    } {
        if !bytes.Contains(restored, action) {
            t.Fatalf("restored source is missing %q:\n%s", action, restored)
        }
    }
}
```

Also add tests named:

- `TestAnalyzePlainYAMLReturnsIndependentDocument`
- `TestAnalyzePreservesTrimMarkersAndIndentation`
- `TestAnalyzeRejectsUnclosedTemplateActionWithoutLeakingBody`
- `TestAnalyzeRejectsReservedMarkerPrefix`
- `TestRestoreRejectsMissingAndDuplicatedMarkers`
- `TestAnalyzeHonorsQuotedClosingDelimiterInsideAction`

The quoted-delimiter case must use `value: {{ printf "%s}}" .U.value }}` and prove the scanner closes on the final delimiter, not the delimiter inside the quoted string.

- [ ] **Step 2: Run the template tests and verify RED**

Run:

```bash
go test ./manifest -run 'Analyze|Restore' -count=1
```

Expected: FAIL because `manifest.Analyze`, `Analysis`, and `TemplateInfo` do not exist.

- [ ] **Step 3: Implement the scanner, projection, and restoration**

Create `manifest/template.go` with these public types:

```go
package manifest

type TemplateInfo struct {
    Present              bool     `json:"present"`
    ControlCount         int      `json:"controlCount"`
    ExpressionCount      int      `json:"expressionCount"`
    HasConditionalBlocks bool     `json:"hasConditionalBlocks"`
    HasInlineConditions  bool     `json:"hasInlineConditions"`
    ActionKinds          []string `json:"actionKinds"`
}

type Analysis struct {
    document *Document
    template TemplateInfo
    markers  []templateMarker
    lineDepth map[int]int
}

func Analyze(data []byte) (*Analysis, error)
func (analysis *Analysis) Document() *Document
func (analysis *Analysis) Template() TemplateInfo
func (analysis *Analysis) Restore(encoded []byte) ([]byte, error)
```

Use reserved prefixes `lzc-toolkit-template-control-` and `lzc-toolkit-template-expression-`. Reject input containing either prefix.

Implement a byte scanner with explicit states for ordinary content, double-quoted strings, single-quoted character literals, raw strings, and backslash escapes. Record each complete `{{...}}` span without parsing or executing its pipeline. Normalize the first action word after removing trim markers. Treat standalone `if`, `else`, `end`, `with`, and `range` actions as control markers; replace every other action with a scalar-safe expression marker. Inline `if`, `else`, and `end` actions remain expression markers and set `HasInlineConditions`.

Track conditional depth per source line. Reject unmatched `else`, unmatched `end`, duplicate `else`, and unclosed standalone blocks with `CodeInvalidManifest`. Use `Path` values such as `manifest.yml:4` only when a source name is available internally; never include action bodies in the error cause.

Parse projected bytes with the existing `Parse` function. `Document()` returns `analysis.document.Clone()`. `Template()` clones `ActionKinds`. `Restore` requires every marker exactly once and replaces control-marker lines or expression-marker substrings with the exact original bytes.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run:

```bash
go test ./manifest -run 'Analyze|Restore' -count=1
go test -race ./manifest -run 'Analyze|Restore' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the template projection**

```bash
git add manifest/template.go manifest/template_test.go
git commit -m "feat: analyze templated manifests safely"
```

---

### Task 2: Extract normalized Manifest source information

**Files:**
- Create: `manifest/summary.go`
- Create: `manifest/summary_test.go`
- Modify: `manifest/template.go`

**Interfaces:**
- Consumes: `Analyze`, `Analysis.Document`, template marker metadata, and source conditional depths from Task 1.
- Produces: `func (*Analysis) Summary() Summary`.
- Produces: `Summary`, `PackageSummary`, `ApplicationSummary`, `ServiceSummary`, `ImageSummary`, `Diagnostic`, and `Severity`.

- [ ] **Step 1: Write failing summary tests using real LazyCat shapes**

Add `manifest/summary_test.go` with a source containing:

```yaml
package: community.lazycat.app.summary
version: 1.2.3
name: Summary
locales:
  zh:
    name: 摘要
  en:
    name: Summary
application:
  subdomain: summary
  upstreams:
    - location: /
      backend: http://web:8080/
ext_config:
  permissions:
    - user.notify
services:
  db:
    image: postgres:16
    healthcheck:
      test: [CMD-SHELL, pg_isready]
  web:
    # upstream: ghcr.io/example/web:v1.2.3
    image: registry.lazycat.cloud/example/web:abc123
    depends_on: [db]
{{ if .U.enable_worker }}
  worker:
    image: {{ .U.worker_image }}
{{ end }}
```

Assert exact sorted package locale codes, permission IDs, service names, dependencies, image targets, upstream reference, embedded alias parsing, conditional flags, and `TEMPLATED_IMAGE` diagnostic. Assert the templated runtime reference is empty and `Editable` is false.

Add these tests:

- `TestSummaryRetainsMutuallyExclusiveDuplicateUsageKeys`
- `TestSummaryMarksDuplicateConditionalServiceAsAmbiguous`
- `TestSummaryDetectsApplicationExecAndImageForms`
- `TestSummaryReportsLegacyHealthCheck`
- `TestSummaryNeverExposesEnvironmentOrTemplateBodies`
- `TestSummaryReturnsDefensiveSortedCopies`

- [ ] **Step 2: Run summary tests and verify RED**

Run:

```bash
go test ./manifest -run Summary -count=1
```

Expected: FAIL because `Analysis.Summary` and summary types do not exist.

- [ ] **Step 3: Define the normalized summary contract**

Create `manifest/summary.go` with these public fields:

```go
type Severity string

const (
    SeverityInfo    Severity = "info"
    SeverityWarning Severity = "warning"
)

type Diagnostic struct {
    Severity Severity `json:"severity"`
    Code     string   `json:"code"`
    Path     string   `json:"path"`
    Message  string   `json:"message"`
}

type PackageSummary struct {
    Package              string   `json:"id"`
    Version              string   `json:"version"`
    Name                 string   `json:"name"`
    Description          string   `json:"description"`
    Author               string   `json:"author"`
    License              string   `json:"license"`
    Homepage             string   `json:"homepage"`
    MinOSVersion         string   `json:"minOsVersion"`
    UnsupportedPlatforms []string `json:"unsupportedPlatforms"`
    LocaleCodes          []string `json:"localeCodes"`
}

type ApplicationSummary struct {
    Subdomain        string   `json:"subdomain"`
    HasImage         bool     `json:"hasImage"`
    HasServices      bool     `json:"hasServices"`
    HasExecLaunch    bool     `json:"hasExecLaunch"`
    BackgroundTask   bool     `json:"backgroundTask"`
    MultiInstance    bool     `json:"multiInstance"`
    HasRoutes        bool     `json:"hasRoutes"`
    HasUpstreams     bool     `json:"hasUpstreams"`
    HasIngress       bool     `json:"hasIngress"`
    HasEntries       bool     `json:"hasEntries"`
    HasInjects       bool     `json:"hasInjects"`
    HasPublicPath    bool     `json:"hasPublicPath"`
    HasPermissions   bool     `json:"hasPermissions"`
    Permissions      []string `json:"permissions"`
}

type ServiceSummary struct {
    Name           string   `json:"name"`
    Conditional    bool     `json:"conditional"`
    HasImage       bool     `json:"hasImage"`
    HasHealthcheck bool     `json:"hasHealthcheck"`
    HasBinds       bool     `json:"hasBinds"`
    DependsOn      []string `json:"dependsOn"`
}

type ImageSummary struct {
    Target        string `json:"target"`
    Service       string `json:"service"`
    RuntimeRef    string `json:"runtimeRef"`
    UpstreamRef   string `json:"upstreamRef"`
    EmbeddedAlias string `json:"embeddedAlias"`
    Templated     bool   `json:"templated"`
    Conditional   bool   `json:"conditional"`
    Editable      bool   `json:"editable"`
    Reason        string `json:"reason"`
}

type Summary struct {
    Package     PackageSummary     `json:"package"`
    Application ApplicationSummary `json:"application"`
    Services    []ServiceSummary   `json:"services"`
    Images      []ImageSummary     `json:"images"`
    Template    TemplateInfo       `json:"template"`
    Diagnostics []Diagnostic       `json:"diagnostics"`
}
```

- [ ] **Step 4: Implement syntax-tree extraction without typed full-document decode**

Walk the private YAML nodes inside the `manifest` package. Do not call `Document.Decode(&Manifest{})` for templated input.

Implement helpers that:

- enumerate every mapping pair rather than silently choosing the first duplicate;
- read static scalar strings, booleans, and string lists only when they contain no expression marker;
- derive conditional status from the node's source line and `analysis.lineDepth`;
- detect `exec://` recursively in routes and non-empty `backend_launch_command` in upstreams;
- normalize `depends_on` from scalar, sequence, or mapping keys;
- recognize both `healthcheck` and legacy `health_check` while emitting `LEGACY_HEALTH_CHECK`;
- normalize scalar permission lists and emit `UNSUPPORTED_PERMISSIONS_SHAPE` for other present forms;
- parse `embed:<alias>` without accepting malformed aliases;
- read `# upstream: <reference>` from key or value head comments;
- mark duplicate services and duplicate target images non-editable;
- sort locale codes, platforms, permissions, dependencies, services, images, action kinds, and diagnostics.

`Summary()` must deep-copy every slice before returning.

- [ ] **Step 5: Run summary and complete manifest tests**

Run:

```bash
go test ./manifest -count=1
go test -race ./manifest -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit normalized Manifest analysis**

```bash
git add manifest/summary.go manifest/summary_test.go manifest/template.go
git commit -m "feat: summarize LazyCat manifest sources"
```

---

### Task 3: Centralize effective build configuration and package identity validation

**Files:**
- Create: `build/project_config.go`
- Create: `build/project_config_test.go`
- Create: `internal/packageid/packageid.go`
- Create: `internal/packageid/packageid_test.go`
- Modify: `build/build.go`
- Modify: `build/metadata.go`
- Modify: `build/options.go`
- Modify: `lint/resource.go`

**Interfaces:**
- Produces: `build.ConfigRequest`, `build.ResolvedConfig`, and `build.ResolveConfig(context.Context, ConfigRequest) (ResolvedConfig, error)`.
- Produces: `build.PredictLayout(forceV2 bool, loaded LoadedConfig, packageExists, resourceOnly bool) lpk.Layout`.
- Produces: `internal/packageid.Valid(string) bool`.

- [ ] **Step 1: Write failing effective-config tests**

Create `build/project_config_test.go` proving:

- release config expands `${package}` and `${version}` from a plain Manifest;
- a templated Manifest obtains the same static package/version values through `manifest.Analyze`;
- `lzc-build.dev.yml` inherits its release parent and reports `ProfileDevelopment`;
- `Environment` overrides inherited process values;
- returned maps are defensive copies;
- nil and cancelled contexts retain `INVALID_ARGUMENT` and `CANCELLED` behavior;
- `PredictLayout` matches current v1/v2 rules including `ForceV2`, `package.yml`, images, envs, package overrides, and resource-only projects.

Use this public contract in the test:

```go
resolved, err := build.ResolveConfig(context.Background(), build.ConfigRequest{
    Root: root,
    ConfigFile: "lzc-build.yml",
    Environment: map[string]string{"CHANNEL": "stable"},
})
if err != nil {
    t.Fatal(err)
}
if resolved.Root != root || resolved.Loaded.Config.ContentDir != "community.lazycat.app.demo" {
    t.Fatalf("resolved = %#v", resolved)
}
```

- [ ] **Step 2: Run focused tests and verify RED**

Run:

```bash
go test ./build ./internal/packageid -run 'ResolveConfig|PredictLayout|PackageID' -count=1
```

Expected: FAIL because the new packages and functions do not exist.

- [ ] **Step 3: Add the shared package ID validator**

Create `internal/packageid/packageid.go`:

```go
package packageid

import "regexp"

var pattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*(\.[a-z][a-z0-9]*(-[a-z0-9]+)*)*$`)

func Valid(value string) bool {
    return pattern.MatchString(value)
}
```

Move build and resource-lint checks to this helper without changing error or warning text.

- [ ] **Step 4: Implement effective config resolution**

Create `build/project_config.go`:

```go
type ConfigRequest struct {
    Root               string
    ConfigFile         string
    Environment        map[string]string
    InheritEnvironment bool
}

type ResolvedConfig struct {
    Root   string
    Loaded LoadedConfig
}

func ResolveConfig(ctx context.Context, request ConfigRequest) (ResolvedConfig, error)
func PredictLayout(forceV2 bool, loaded LoadedConfig, packageExists, resourceOnly bool) lpk.Layout
```

Move the existing environment collection and three-pass load sequence from `prepare` into `ResolveConfig`. Replace `loadManifestTemplateValues` with a helper that reads the selected Manifest, applies `manifest.PreprocessFile` when possible, calls `manifest.Analyze`, and copies only static top-level package fields into the config substitution map. Merge static `package.yml` values afterward, preserving its current precedence.

Return a cleaned absolute root and a `LoadedConfig` whose `Raw`, `BuildEnv`, `ComposeOverride`, and other maps are independent copies.

Change `prepare` to call `ResolveConfig`, then keep its separate command environment for the explicitly enabled build script. Change `selectLayout` into the public `PredictLayout` helper and update build callers.

- [ ] **Step 5: Run build and lint tests**

Run:

```bash
go test ./build ./lint ./internal/packageid -count=1
go test -race ./build ./lint ./internal/packageid -count=1
```

Expected: PASS with unchanged existing build and lint behavior.

- [ ] **Step 6: Commit shared project configuration**

```bash
git add build/project_config.go build/project_config_test.go build/build.go build/metadata.go build/options.go lint/resource.go internal/packageid
git commit -m "refactor: share effective project configuration"
```

---

### Task 4: Add local `project.Inspect`

**Files:**
- Modify: `project/types.go`
- Create: `project/inspect.go`
- Create: `project/inspect_test.go`
- Create: `project/testdata/inspection-v2.json`

**Interfaces:**
- Consumes: `build.ResolveConfig`, `build.PredictLayout`, `manifest.PreprocessFile`, `manifest.Analyze`, and `internal/packageid.Valid`.
- Produces: `project.Inspect(context.Context, InspectRequest) (Inspection, error)`.
- Preserves: `project.Info`, `project.InfoRequest`, `project.Service.Info`, and all lifecycle methods.

- [ ] **Step 1: Add failing API and golden JSON tests**

Extend `project/types.go` references in `project/inspect_test.go` with this request:

```go
inspection, err := project.Inspect(context.Background(), project.InspectRequest{
    Root: root,
    ConfigFile: "lzc-build.yml",
})
if err != nil {
    t.Fatal(err)
}
if inspection.SchemaVersion != 1 || inspection.Kind != project.KindService || inspection.Package.Package != "community.lazycat.app.demo" {
    t.Fatalf("inspection = %#v", inspection)
}
```

Create fixtures in tests for:

- legacy v1 static project;
- v2 service project with package metadata, locale codes, content, icon, deploy params, resource exports, dependencies, and upstream image comments;
- exec project using `backend_launch_command`;
- resource-only project;
- development config inheriting `lzc-build.yml`;
- templated project with conditional duplicate `usage`, conditional service, bare `index` port, and inline conditional scalar;
- `VersionOverride` and `ForceV2`;
- configured path escape and symlink rejection;
- templated package identity rejection;
- nil/cancelled context;
- JSON secret exclusion.

Marshal the representative v2 result with `json.MarshalIndent`, append one newline, and compare it to `project/testdata/inspection-v2.json`.

- [ ] **Step 2: Run project tests and verify RED**

Run:

```bash
go test ./project -run 'Inspect|Inspection' -count=1
```

Expected: FAIL because local inspection types and `project.Inspect` do not exist.

- [ ] **Step 3: Define additive local inspection types**

Append to `project/types.go` without changing existing remote types:

```go
type Kind string

const (
    KindStatic  Kind = "static"
    KindExec    Kind = "exec"
    KindService Kind = "service"
)

type InspectRequest struct {
    Root               string
    ConfigFile         string
    Environment        map[string]string
    InheritEnvironment bool
    VersionOverride    string
    ForceV2            bool
}

type FileInfo struct {
    Root             string `json:"root"`
    BuildConfig      string `json:"buildConfig"`
    BuildParent      string `json:"buildParent"`
    PackageFile      string `json:"packageFile"`
    ManifestFile     string `json:"manifestFile"`
    PackageOutputDir string `json:"packageOutputDir"`
    LPKPath          string `json:"lpkPath"`
    ContentDir       string `json:"contentDir"`
    Icon             string `json:"icon"`
    DeployParams     string `json:"deployParams"`
}

type BuildInfo struct {
    Profile                build.Profile        `json:"profile"`
    HasBuildScript         bool                 `json:"hasBuildScript"`
    HasContent             bool                 `json:"hasContent"`
    HasComposeOverride     bool                 `json:"hasComposeOverride"`
    ConfiguredImageAliases []string             `json:"configuredImageAliases"`
    ResourceExports        []ResourceExportInfo `json:"resourceExports"`
}

type ResourceExportInfo struct {
    Kind   string `json:"kind"`
    Source string `json:"source"`
}

type Inspection struct {
    SchemaVersion int                         `json:"schemaVersion"`
    Kind          Kind                        `json:"kind"`
    Layout        lpk.Layout                  `json:"layout"`
    ResourceOnly  bool                        `json:"resourceOnly"`
    Files         FileInfo                    `json:"files"`
    Package       manifest.PackageSummary     `json:"package"`
    Build         BuildInfo                   `json:"build"`
    Application   manifest.ApplicationSummary `json:"application"`
    Services      []manifest.ServiceSummary   `json:"services"`
    Images        []manifest.ImageSummary     `json:"images"`
    Template      manifest.TemplateInfo       `json:"template"`
    Diagnostics   []manifest.Diagnostic       `json:"diagnostics"`
}
```

Use normal imports in `project/types.go`; no aliases may change the existing remote `Info` JSON behavior.

- [ ] **Step 4: Implement project path and source inspection**

Create `project/inspect.go` and implement this sequence:

1. Validate context.
2. Call `build.ResolveConfig` with the inspection request.
3. Confine the effective config and every reported configured path beneath the resolved root using `filepath.Rel`.
4. Reject existing symlink components with `os.Lstat`; allow a missing optional final path.
5. Resolve default Manifest name `lzc-manifest.yml` and fixed v2 package name `package.yml`.
6. Detect Manifest/package existence and resource-only status.
7. Preprocess an existing Manifest with profile and effective build env, then call `manifest.Analyze`.
8. Parse `package.yml` through `manifest.Analyze` as plain static YAML.
9. Select v2 package metadata when present, otherwise use legacy top-level Manifest metadata.
10. Apply `package_override`, `pkg_id`, `pkg_name`, and `VersionOverride` using the same precedence as `build.applyMetadataOverrides`.
11. Require a valid static package ID and non-empty static version.
12. Require static `application.subdomain` for non-resource-only projects.
13. Derive kind from `Application.HasServices` and `Application.HasExecLaunch`.
14. Predict layout with `build.PredictLayout`.
15. Resolve configured image aliases from map keys or sequence IDs without exposing their raw build definitions.
16. Sort and defensively copy all output slices.

Use `*lpkgo.Error` with operation prefixes `project.inspect`, `project.inspect.path`, `project.inspect.package`, and `project.inspect.manifest`. Do not include environment values or raw templates in error causes.

- [ ] **Step 5: Run project tests and verify GREEN**

Run:

```bash
go test ./project -count=1
go test -race ./project -count=1
```

Expected: PASS, including the golden JSON fixture.

- [ ] **Step 6: Commit local project inspection**

```bash
git add project/types.go project/inspect.go project/inspect_test.go project/testdata/inspection-v2.json
git commit -m "feat: inspect local LazyCat projects"
```

---

### Task 5: Replace build and LPK template heuristics with shared analysis

**Files:**
- Modify: `build/metadata.go`
- Modify: `build/build.go`
- Modify: `build/build_test.go`
- Modify: `lpk/validate.go`
- Modify: `lpk/writer_test.go`

**Interfaces:**
- Consumes: `manifest.Analyze`, `Analysis.Document`, `Analysis.Summary`, and `TemplateInfo.Present`.
- Removes: `isGoTemplate`, `fakeManifestValues`, and raw-brace `isManifestTemplate` detection.
- Preserves: original templated source bytes in output LPKs.

- [ ] **Step 1: Add failing build regressions for documented template forms**

Extend `build/build_test.go` with:

- a v2 templated Manifest containing duplicate conditional `usage`, bare `index` port, `stable_secret`, and inline conditional scalar; assert build succeeds and all original template actions remain in `manifest.yml`;
- a templated v1 Manifest whose static package/version/name/subdomain are correctly extracted without `fakeManifestValues`;
- a templated Manifest with configured embedded images in an unambiguous static service; assert the image builder receives the static service image structure;
- a templated Manifest where the required embedded image target is conditional or templated; assert a structured `INVALID_MANIFEST` error instead of silently building from an incomplete fake document;
- text containing only one brace delimiter; assert it is not accepted as a valid template Manifest.

Extend `lpk/writer_test.go` so `AllowManifestTemplate` accepts a valid analyzed template and rejects malformed delimiters even when both raw brace strings occur.

- [ ] **Step 2: Run focused tests and verify RED**

Run:

```bash
go test ./build ./lpk -run 'Template|Templated' -count=1
```

Expected: at least the duplicate-key/bare-scalar build and malformed-template validation cases fail under the heuristic implementation.

- [ ] **Step 3: Integrate analysis into build metadata preparation**

Change the templated branch of build preparation to:

```go
analysis, err := manifest.Analyze(processedManifest)
if err != nil {
    return fail(err)
}
manifestDocument = analysis.Document()
prepared.templated = analysis.Template().Present
manifestSummary := analysis.Summary()
```

For templated input, assemble effective static package/application data from `manifestSummary` and static `package.yml`, while retaining `processedManifest` as the bytes written into the LPK. Do not decode a duplicate-key projection into the full typed Manifest.

When an image builder is configured, construct the typed `manifest.Manifest` only from unambiguous static application and service image records plus effective package metadata. Reject templated/conditional/duplicate embedded targets before invoking the builder.

Delete `isGoTemplate`, `fakeManifestValues`, and the old compatibility-document construction after all callers use analysis.

- [ ] **Step 4: Harden `lpk.Write` template validation**

Replace raw `bytes.Contains("{{") && bytes.Contains("}}")` checks in `lpk/validate.go` with `manifest.Analyze`. Compatibility acceptance requires `analysis.Template().Present == true`; malformed templates return the analysis error. Plain invalid YAML with brace-like text remains invalid.

- [ ] **Step 5: Run build, LPK, image, and compatibility suites**

Run:

```bash
go test ./build ./lpk ./image/... ./internal/compat -count=1
go test -race ./build ./lpk ./image/... ./internal/compat -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit shared build template analysis**

```bash
git add build/metadata.go build/build.go build/build_test.go lpk/validate.go lpk/writer_test.go
git commit -m "fix: use structured templated manifest analysis"
```

---

### Task 6: Document the local project API and run release gates

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `docs/superpowers/plans/2026-07-11-local-project-inspection.md`

**Interfaces:**
- Documents: `manifest.Analyze`, `project.Inspect`, schema version, non-execution guarantee, and the difference between local inspection, completed-LPK `inspect`, and remote `project.Service.Info`.

- [ ] **Step 1: Add bilingual API examples**

Add a package-selection row for local project inspection and a runnable example equivalent to:

```go
inspection, err := project.Inspect(ctx, project.InspectRequest{
    Root: ".",
    ConfigFile: "lzc-build.yml",
})
if err != nil {
    log.Fatal(err)
}
fmt.Printf("%s %s %s services=%d images=%d templated=%t\n",
    inspection.Package.Package,
    inspection.Package.Version,
    inspection.Kind,
    len(inspection.Services),
    len(inspection.Images),
    inspection.Template.Present,
)
```

Document that inspection does not execute `buildscript` or Go Templates, does not return secrets, and uses `SchemaVersion == 1` for stable JSON consumers.

- [ ] **Step 2: Run documentation and focused contract checks**

Run:

```bash
rg -n "manifest\.Analyze|project\.Inspect|SchemaVersion|Go Template|never execute|不执行" README.md README.zh-CN.md
go test ./manifest ./build ./project ./lpk -count=1
git diff --check
```

Expected: all searches find the new documentation, tests pass, and diff check reports no errors.

- [ ] **Step 3: Run the complete verification gate**

Run fresh:

```bash
go test -count=1 -race ./...
go vet ./...
bash scripts/check-import-boundaries.sh
bash scripts/validate-local-images.sh
bash scripts/validate-lazycat-contrib.sh
go mod verify
git diff --check
```

Expected: every command exits 0.

- [ ] **Step 4: Record final evidence in the plan**

Under this task, append the exact command list, exit status, and any intentionally skipped external check with its reason. Do not mark a check complete without fresh output.

- [ ] **Step 5: Commit documentation and verification evidence**

```bash
git add README.md README.zh-CN.md docs/superpowers/plans/2026-07-11-local-project-inspection.md
git commit -m "docs: document local project inspection"
```
