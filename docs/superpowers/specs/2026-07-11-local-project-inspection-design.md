# Local LazyCat Project Inspection and Go Template Analysis Design

## Problem

`lzc-toolkit-go` can build a local LazyCat project, inspect a completed LPK,
and manage an installed application remotely. It does not yet expose one API
that describes an existing source project before building it.

The missing boundary forces callers such as `lazycat-github-action` to repeat
project path resolution, build-config loading, package metadata decoding,
Manifest classification, service discovery, and image extraction.

Go Template compatibility is also incomplete. `build/metadata.go` currently
detects any source containing `{{` and `}}`, then uses line-oriented
`fakeManifestValues` extraction for only `package`, `version`, `name`, and
`subdomain`. This preserves simple templates in the output LPK, but it cannot
reliably understand documented LazyCat Manifest forms such as:

```yaml
{{ if .U.target }}
usage: "to {{ .U.target }}"
{{ else }}
usage: "netmap"
{{ end }}

port: {{ index .U "listen.port" }}

- URL={{if .U.base_url}}{{.U.base_url}}{{else}}https://{{.S.AppDomain}}{{end}}
```

The library needs a reusable, non-executing analysis layer and a first-class
local project inspection API.

## Goals

1. Add `project.Inspect` for local source projects without changing the
   existing remote `project.Service.Info` API.
2. Return deterministic package, build, application, service, image, template,
   and diagnostic information suitable for direct JSON encoding.
3. Replace heuristic templated-Manifest metadata extraction with a reusable
   `manifest.Analyze` API.
4. Accept common LazyCat Go Template controls and scalar expressions without
   evaluating templates or requiring deployment values.
5. Make `build` consume the same analysis path used by project inspection.
6. Keep environment values, secrets, inject bodies, scripts, and raw template
   expressions out of inspection results and error messages.

## Non-goals

- Render or execute a Manifest Go Template.
- Enumerate all possible conditional renderings.
- Validate deployment-time template functions or parameter values.
- Automatically update Manifest images in this repository.
- Replace the existing completed-LPK `inspect` package.
- Change the remote lifecycle meaning of `project.Info` or
  `project.Service.Info`.

## Package Boundaries

### `manifest`

Owns lexical Go Template analysis and a YAML-safe projection because template
syntax is a property of the Manifest source, not of GitHub Actions or project
lifecycle code.

New API:

```go
func Analyze(data []byte) (*Analysis, error)

type Analysis struct { /* private source and projection state */ }

func (analysis *Analysis) Document() *Document
func (analysis *Analysis) Template() TemplateInfo
func (analysis *Analysis) Summary() Summary
func (analysis *Analysis) Restore(encoded []byte) ([]byte, error)
```

`Document` returns an independent clone of the YAML-safe projection.
`Template`, `Summary`, and their slices also return defensive copies.
`Restore` replaces projection markers with the exact original template actions
and is useful to downstream source-preserving editors.

Plain YAML is valid input and produces `TemplateInfo.Present == false`.

### `project`

Keeps remote lifecycle methods and adds a free function for local source
inspection:

```go
func Inspect(context.Context, InspectRequest) (Inspection, error)
```

The request is intentionally aligned with the non-executing part of
`build.Request`:

```go
type InspectRequest struct {
    Root               string
    ConfigFile         string
    Environment        map[string]string
    InheritEnvironment bool
    VersionOverride    string
    ForceV2            bool
}
```

Inspection never runs `buildscript`, builds images, writes an LPK, contacts a
LazyCat device, or accesses an app store.

### `build`

Continues to own `lzc-build.yml` parsing and build semantics. Its effective
multi-pass config resolution is extracted into a shared exported helper used by
both `build` and `project.Inspect`. The existing low-level `LoadConfig` API
remains compatible.

`build` replaces `fakeManifestValues` and `parseManifestForBuild` heuristics
with `manifest.Analyze`. Templated source is still preserved in the packaged
LPK.

## Manifest Template Analysis

### Lexical scanner

The scanner locates `{{ ... }}` actions without invoking `text/template`.
It recognizes:

- optional trim markers `{{-` and `-}}`;
- quoted and raw string content inside an action;
- standalone `if`, `else`, `end`, `with`, and `range` controls;
- inline value and function expressions including `.U`, `.INTERNAL`, `.S`,
  `index`, and `stable_secret`;
- several actions embedded in one YAML scalar;
- inline `if/else/end` sequences that produce one scalar value.

Unknown function names remain valid opaque template expressions. The scanner
does not claim they will render successfully on a device.

Unclosed delimiters, reserved marker collisions, duplicated markers, and lost
markers fail with `INVALID_MANIFEST`. Errors include a source line where useful
but never include the complete template body.

### YAML projection

Standalone controls become indentation-preserving YAML comments. Inline
actions become scalar-safe marker tokens.

```yaml
{{ if .U.target }}
port: {{ index .U "listen.port" }}
{{ end }}
```

projects to the equivalent of:

```yaml
# lzc-toolkit-template-control-0
port: lzc-toolkit-template-expression-0
# lzc-toolkit-template-control-1
```

The projection records the exact original action, line, standalone/inline
form, normalized action kind, and conditional depth. It is parsed as a
`manifest.Document` without decoding the complete unrendered source into the
typed `manifest.Manifest` model.

Duplicate YAML keys are retained in the syntax tree. This is required for
mutually exclusive branches that each define fields such as `usage`.

### Summary extraction

`manifest.Summary` exposes normalized static facts only:

```go
type Summary struct {
    Package     PackageSummary
    Application ApplicationSummary
    Services    []ServiceSummary
    Images      []ImageSummary
    Template    TemplateInfo
    Diagnostics []Diagnostic
}
```

The summary does not expose raw `yaml.Node`, arbitrary YAML values,
environment entries, commands, setup scripts, inject bodies, or template
source.

Collections are sorted deterministically. Values containing a template action
are marked templated and are not returned as known static values.

## Local Project Inspection Contract

`project.Inspection` is directly JSON serializable and starts with an explicit
schema version:

```go
type Inspection struct {
    SchemaVersion int                `json:"schemaVersion"`
    Kind          Kind               `json:"kind"`
    Layout        lpk.Layout         `json:"layout"`
    Files         FileInfo           `json:"files"`
    Package       PackageInfo        `json:"package"`
    Build         BuildInfo          `json:"build"`
    Application   ApplicationInfo    `json:"application"`
    Services      []ServiceInfo      `json:"services"`
    Images        []ImageInfo        `json:"images"`
    Template      manifest.TemplateInfo `json:"template"`
    Diagnostics   []Diagnostic       `json:"diagnostics"`
}
```

`SchemaVersion` is `1`. Compatible additions do not change the number;
incompatible interpretation changes require a new schema version.

### Files

File information includes resolved project root, effective build config,
development parent config, package file, Manifest file, output LPK, content
directory, icon, and deploy-params paths. Empty optional paths remain empty
strings so the JSON shape is stable.

Every configured path is cleaned and confined beneath the project root before
it is returned. Existing files with symlink components are rejected. Missing
optional files are represented as absent rather than followed outside the
project boundary.

### Package information

Package information includes:

- package ID and version;
- name, description, author, license, homepage, and minimum OS version;
- sorted unsupported platforms and locale codes.

LPK v2 metadata comes from `package.yml`. Legacy v1 metadata comes from static
top-level Manifest fields. Effective `pkg_id`, `pkg_name`, `package_override`,
build-environment substitution, and `VersionOverride` semantics match
`build.Build`.

Package ID and version remain required. Metadata needed for identity must be a
static value; a templated identity is rejected instead of guessed.

### Build information

Build information reports profile, buildscript presence, content presence,
compose override presence, configured image-build aliases, and resource
exports. It never returns buildscript text or build environment values.

The predicted LPK layout uses exactly the same decision function as
`build.Build`, including `ForceV2`.

### Application and project kind

Project kind is backward-compatible with the Action vocabulary:

- `service` when a statically discoverable services mapping exists;
- `exec` when no services exist and a static `exec://` route or non-empty
  `backend_launch_command` exists;
- `static` otherwise.

Application information reports static subdomain and image presence plus
feature booleans for services, exec launch, background task, multi-instance,
routes, upstreams, ingress, entries, injects, public paths, and
`ext_config.permissions` presence. Static scalar permission IDs are returned
as a sorted list. Other permission shapes remain present but opaque and emit a
diagnostic instead of being coerced into an invented required/optional model.

Feature presence is reported separately from `Kind`, so mixed projects do not
lose information.

### Services and images

Services are sorted by name and report structural facts only: conditional
status, image presence, healthcheck presence, bind presence, and static
dependency names.

Image records include application or service target, service name, static
runtime reference, upstream comment reference, embedded alias, templated and
conditional flags, and whether an automated source editor may safely target
the field.

A templated image value, duplicated conditional service, or multiple image
occurrences for the same target is marked non-editable and emits a diagnostic.

## Diagnostics

Diagnostics use a stable shape:

```go
type Diagnostic struct {
    Severity Severity `json:"severity"`
    Code     string   `json:"code"`
    Path     string   `json:"path"`
    Message  string   `json:"message"`
}
```

Initial machine-readable codes include:

- `TEMPLATED_FIELD`
- `TEMPLATED_IMAGE`
- `CONDITIONAL_IMAGE`
- `DUPLICATE_SERVICE`
- `DUPLICATE_IMAGE_TARGET`
- `LEGACY_HEALTH_CHECK`
- `UNSUPPORTED_PERMISSIONS_SHAPE`
- `UNKNOWN_TEMPLATE_ACTION`

Fatal path, syntax, identity, and configuration failures remain structured
`*lpkgo.Error` values. Diagnostics describe uncertainty in an otherwise safe
inspection result.

## Build Integration

The build path uses `manifest.Analyze` after existing `#@build` preprocessing.

For a plain Manifest, behavior remains unchanged.

For a templated Manifest:

1. The original preprocessed source remains authoritative for package output.
2. Static metadata and subdomain come from the analysis summary, not line
   splitting.
3. The projected document supplies the static structure needed by effective
   metadata and image validation.
4. Duplicate or templated structures required by an embedded-image build fail
   explicitly rather than silently using an incomplete fake Manifest.
5. Version overrides and removal of v2 static fields continue to preserve all
   template actions.

`lpk.Write` keeps `AllowManifestTemplate` as an explicit compatibility option.
Its detection is changed from a raw brace check to the same validated template
analysis, so arbitrary text containing braces is not automatically accepted as
a templated Manifest.

## Security and Compatibility

- Go Templates are never executed.
- `InspectRequest.InheritEnvironment` defaults to false.
- Inspection results never contain build environment values, deployment
  parameter values, environment entries, scripts, or raw template actions.
- Existing public `manifest.Parse`, `build.LoadConfig`, `build.Build`,
  `project.Service.Info`, `inspect.Info`, and `lpk.WriteRequest` contracts remain
  compatible.
- New slices and maps returned from public APIs are defensive copies.
- Plain YAML, v1, v2, resource-only, release, and development projects remain
  supported.

## Verification

Add tests for:

1. `manifest.Analyze` on plain YAML and all documented LazyCat template forms.
2. Exact restoration of standalone and inline actions, including trim markers.
3. Conditional duplicate keys, bare numeric-style template scalars,
   `stable_secret`, `index`, and inline `if/else/end` expressions.
4. Reserved-marker, malformed-delimiter, missing-marker, and duplicate-marker
   failures.
5. Static, exec, service, multi-service, v1, v2, development-profile, and
   resource-only project inspection.
6. Effective metadata overrides, locale codes, supported permission lists,
   resource exports, service dependencies, embedded image aliases, and
   upstream image comments.
7. Deterministic JSON golden output with `schemaVersion: 1`.
8. Proof that secrets, environment values, scripts, inject bodies, and raw
   template bodies never enter inspection JSON or errors.
9. Build regressions proving templated source is preserved and existing plain
   builds are byte/metadata compatible where promised.
10. Full `go test -race ./...`, `go vet ./...`, import-boundary checks,
    upstream fixture validation, and `git diff --check` before release.
