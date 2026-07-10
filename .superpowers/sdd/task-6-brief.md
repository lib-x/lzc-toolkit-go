### Task 6: Manifest build directives and compatibility linting

**Files:**

- Create: manifest/preprocess.go
- Create: manifest/preprocess_test.go
- Create: lint/manifest.go
- Create: lint/manifest_test.go
- Create: lint/resource.go
- Create: lint/resource_test.go

**Interfaces:**

- Produces manifest.Preprocess and PreprocessFile.
- Produces lint.Manifest, lint.ResourcePackage, and stable warning codes.
- Consumes lpkgo.Warning and manifest types.

- [ ] **Step 1: Write directive matrix tests**

Tests cover:

- profile equality and inequality;
- env presence, equality, and inequality;
- quoted values;
- else branches;
- include indentation;
- inactive includes;
- missing include;
- nested directives in included files;
- duplicate else;
- unmatched else and end;
- unclosed if;
- invalid environment-map keys;
- exact source filename and line in errors.

Duplicate raw `KEY=VALUE` entries are validated at the build-configuration
ingestion boundary in Milestone 2. `BuildContext.Env` is already a map, so a
duplicate valid key cannot be represented at this preprocessing boundary.

- [ ] **Step 2: Implement preprocessing**

Expose:

    type BuildContext struct {
        Profile string
        Env     map[string]string
    }

    type IncludeFS = fs.FS

    func Preprocess(
        context.Context,
        sourceName string,
        input []byte,
        buildContext BuildContext,
        includes fs.FS,
    ) ([]byte, error)

    func PreprocessFile(
        context.Context,
        string,
        BuildContext,
    ) ([]byte, error)

Implement the exact #@build if, else, end, and include grammar documented in
the design. Included files are processed with directives disabled. Copy the
environment map before use and sort no externally observable map output.
Preprocess and PreprocessFile require a non-nil context, return CANCELLED for
pre-cancelled contexts, and check cancellation throughout processing and
before and after active include reads.

- [ ] **Step 3: Write manifest lint tests**

Use typed and raw manifest documents to assert warning codes for:

- unknown-manifest-fields;
- legacy-static-package-fields;
- application-handlers-deprecated;
- application-user-app-deprecated;
- application-depends-on-deprecated;
- service-health-check-deprecated;
- ext-config-http-routing-deprecated.

Warnings must have stable order: schema traversal order followed by sorted
dynamic service paths.

- [ ] **Step 4: Implement manifest lint**

Represent the known 2.0.8 schema as private recursive schema nodes. Walk the
Document mapping without losing raw fields. Emit lpkgo.Warning values with
SeverityWarning and stable codes. Keep message text human-readable but test
codes and paths as the stable contract.

- [ ] **Step 5: Write resource package lint tests**

Create temporary package roots covering:

- missing package.yml;
- invalid package name;
- missing version;
- missing or empty exports;
- more than 100 kinds;
- invalid kind and resource ID names;
- non-directory kind or resource entries;
- empty payloads;
- a valid nested resource payload.

- [ ] **Step 6: Implement resource lint**

Expose:

    func ResourcePackage(ctx context.Context, root fs.FS) ([]lpkgo.Warning, error)

Use the exact reference warning codes from lib/lpk/resource_lint.js. Ignore
dot-prefixed entries when counting visible kinds and resources. Traverse
payloads with fs.WalkDir and stop at the first regular payload file.

- [ ] **Step 7: Verify and commit Task 6**

Run:

    gofmt -w manifest lint
    go test ./manifest ./lint
    go test ./...

Commit:

    git add manifest lint
    git commit -m "feat: preprocess and lint LPK manifests"

---
