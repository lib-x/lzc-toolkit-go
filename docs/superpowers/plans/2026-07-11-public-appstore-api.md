# Public App Store API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add anonymous, typed, read-only official App Store APIs to the existing `appstore` Go package.

**Architecture:** Introduce a token-free `PublicClient` alongside the existing authenticated `Client`. Stable locale endpoints serve application details, dictionaries, rankings, and changelogs; homepage and more-list methods resolve the current metarepo release before fetching snapshot JSON. URL helpers resolve official asset and LPK paths without exposing arbitrary remote requests.

**Tech Stack:** Go 1.25, `net/http`, `net/url`, `encoding/json`, `httptest`, existing `lpkgo.Error` conventions.

## Global Constraints

- All declarations remain in package `appstore`, split across focused `public_*.go` files.
- `PublicClient` has no token field or token option and never sends authentication headers or cookies.
- Default metadata root is `https://dl.lazycat.cloud/appstore/metarepo`.
- Default LPK download root is `https://dl.lazycatmicroserver.com`.
- Default locale is `zh`.
- Homepage and more-list calls use `/op/index` followed by a release snapshot request.
- Categories, kinds, application details, rankings, and changelogs use the stable locale directory.
- Ranking periods are exactly `week`, `month`, and `all`.
- Remote bodies are bounded and external paths are validated before URL construction.

---

## File Structure

- Create `appstore/public_client.go`: options, construction, anonymous bounded HTTP, release lookup, shared JSON decoding.
- Create `appstore/public_client_test.go`: defaults, no-auth contract, errors, cancellation, response limit, release lookup.
- Create `appstore/public_types.go`: public application, version, developer, category, kind, block, rating, and count types.
- Create `appstore/public_urls.go`: safe segment validation and asset/download URL resolution.
- Create `appstore/public_urls_test.go`: URL construction, traversal rejection, application/version matching.
- Create `appstore/public_app.go`: application metadata and version changelog methods.
- Create `appstore/public_app_test.go`: real response-shape decoding and exact package-derived paths.
- Create `appstore/public_home.go`: categories, kinds, homepage, and more-list methods.
- Create `appstore/public_home_test.go`: stable dictionary calls and two-request release snapshot flows.
- Create `appstore/public_rankings.go`: download and developer rankings.
- Create `appstore/public_rankings_test.go`: period validation and typed ranking decoding.
- Modify `README.md`: add anonymous catalog example and method summary.
- Modify `README.zh-CN.md`: add matching Chinese documentation.

---

### Task 1: Anonymous Public Client Foundation

**Files:**
- Create: `appstore/public_client.go`
- Create: `appstore/public_client_test.go`

**Interfaces:**
- Produces: `PublicOptions`, `PublicClient`, `NewPublicClient(PublicOptions) *PublicClient`, `(*PublicClient).CurrentRelease(context.Context) (string, error)`.
- Produces internal helpers used later: `publicURL(localeScoped bool, parts ...string)`, `getBytes`, and `getJSON[T]`.

- [ ] **Step 1: Write failing construction and no-auth tests**

```go
func TestPublicClientGetDoesNotSendCredentials(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if got := r.Header.Get("Authorization"); got != "" {
            t.Fatalf("Authorization=%q", got)
        }
        if got := r.Header.Get("X-User-Token"); got != "" {
            t.Fatalf("X-User-Token=%q", got)
        }
        if _, err := r.Cookie("userToken"); !errors.Is(err, http.ErrNoCookie) {
            t.Fatalf("userToken cookie unexpectedly present: %v", err)
        }
        _, _ = io.WriteString(w, `"release-test-1"`)
    }))
    defer server.Close()

    client := appstore.NewPublicClient(appstore.PublicOptions{
        MetadataBaseURL: server.URL,
        HTTPClient:      server.Client(),
    })
    got, err := client.CurrentRelease(context.Background())
    if err != nil || got != "release-test-1" {
        t.Fatalf("release=%q err=%v", got, err)
    }
}
```

- [ ] **Step 2: Run the focused test and confirm the API is missing**

Run: `go test ./appstore -run TestPublicClientGetDoesNotSendCredentials -count=1`

Expected: build failure because `PublicOptions` and `NewPublicClient` do not exist.

- [ ] **Step 3: Implement the public client and bounded request path**

```go
const (
    DefaultPublicMetadataURL = "https://dl.lazycat.cloud/appstore/metarepo"
    DefaultPublicDownloadURL = "https://dl.lazycatmicroserver.com"
    defaultPublicLocale      = "zh"
    maxPublicResponseBytes   = int64(8 << 20)
)

type PublicOptions struct {
    MetadataBaseURL string
    DownloadBaseURL string
    Locale          string
    HTTPClient      *http.Client
}

type PublicClient struct {
    metadataBase *url.URL
    downloadBase *url.URL
    locale       string
    httpClient   *http.Client
}

func NewPublicClient(options PublicOptions) *PublicClient
func (client *PublicClient) CurrentRelease(ctx context.Context) (string, error)
```

`CurrentRelease` requests `{metadataBase}/op/index`, accepts either a JSON string
or trimmed plain text, and validates the result with the same safe-segment helper
used by later path constructors. `getBytes` maps 404 to `CodeNotFound`, maps
context cancellation to `CodeCancelled`, rejects bodies above 8 MiB, and never
copies a remote response body into an error.

- [ ] **Step 4: Add failure-path tests**

Cover nil context, nil receiver, 404, malformed release values such as
`../release`, oversized responses, and a canceled request. Assert with
`errors.Is(err, lpkgo.ErrInvalidArgument)`, `ErrNotFound`, `ErrRemoteUnavailable`,
and `ErrCancelled` respectively.

- [ ] **Step 5: Run and commit Task 1**

Run: `go test ./appstore -run 'TestPublicClient|TestPublicCurrentRelease' -count=1`

Expected: PASS.

```bash
git add appstore/public_client.go appstore/public_client_test.go
git commit -m "feat: add anonymous app store client"
```

---

### Task 2: Public Models, URL Helpers, and Application Detail

**Files:**
- Create: `appstore/public_types.go`
- Create: `appstore/public_urls.go`
- Create: `appstore/public_urls_test.go`
- Create: `appstore/public_app.go`
- Create: `appstore/public_app_test.go`

**Interfaces:**
- Consumes: `PublicClient`, `getJSON[T]`, and public base URLs from Task 1.
- Produces: `PublicApplication`, `PublicApplicationInfo`, `PublicVersion`, `PublicDeveloper`, `PublicRating`, `PublicCounts`.
- Produces: `GetApplication`, `VersionChangelog`, `AssetURL`, `DownloadURL`, `ApplicationDownloadURL`.

- [ ] **Step 1: Define tests using the observed official JSON shape**

Use a fixture containing these required fields:

```json
{
  "id": 5906,
  "package": "wx.clawbot.lazycat.app.mediasaber",
  "kind_ids": "1",
  "category_ids": [24],
  "status": 1,
  "created_at": "2026-06-21T08:26:36.197Z",
  "updated_at": "2026-07-10T09:11:04.729Z",
  "version_updated_at": "2026-07-10T09:11:04.728Z",
  "information": {"language":"zh","name":"微信ClawBot万能视频下载器","support_pc":true,"support_mobile":true},
  "version": {
    "name":"1.5.3",
    "package":"wx.clawbot.lazycat.app.mediasaber",
    "pkg_hash":"9d3c8de7bd758ef97ecc59cdfc92e1e63f5f097b834389a5c9e47353876acd6a",
    "pkg_path":"/appstore/lpks/pkgs/wx.clawbot.lazycat.app.mediasaber/wx.clawbot.lazycat.app.mediasaber-v1.5.3.lpk",
    "icon_path":"/appstore/metarepo/apps/wx.clawbot.lazycat.app.mediasaber/icon.png",
    "lpk_size":102912,
    "image_size":462608400
  },
  "count":{"downloads":236,"likes":4,"comments":3,"remind_count":0}
}
```

Assert the request path is
`/zh/v3/app_wx.clawbot.lazycat.app.mediasaber.json`, timestamps decode, and the
version fields remain intact.

- [ ] **Step 2: Run the application test and confirm it fails**

Run: `go test ./appstore -run 'TestPublicGetApplication|TestPublicURLs' -count=1`

Expected: build failure because the models and methods do not exist.

- [ ] **Step 3: Implement the observed response types**

```go
type PublicApplication struct {
    ID               int                   `json:"id"`
    Package          string                `json:"package"`
    KindIDs          string                `json:"kind_ids"`
    CategoryIDs      []int                 `json:"category_ids"`
    Status           int                   `json:"status"`
    CreatedAt        time.Time             `json:"created_at"`
    UpdatedAt        time.Time             `json:"updated_at"`
    VersionUpdatedAt time.Time             `json:"version_updated_at"`
    CreateUser       PublicDeveloper       `json:"create_user"`
    Information      PublicApplicationInfo `json:"information"`
    Version          PublicVersion         `json:"version"`
    Rating           PublicRating          `json:"rating"`
    IsOriginal       bool                  `json:"is_original"`
    Count            PublicCounts          `json:"count"`
}

type PublicVersion struct {
    ID                   int      `json:"id"`
    CreateUserID         int      `json:"create_user_id"`
    AppID                int      `json:"app_id"`
    Name                 string   `json:"name"`
    Package              string   `json:"package"`
    PackageHash          string   `json:"pkg_hash"`
    PackagePath          string   `json:"pkg_path"`
    IconPath             string   `json:"icon_path"`
    UnsupportedPlatforms []string `json:"unsupported_platforms"`
    MinOSVersion         string   `json:"min_os_version"`
    ChangelogList        []string `json:"changelog_list"`
    ChangelogLanguage    string   `json:"changelog_language"`
    LPKSize              int64    `json:"lpk_size"`
    ImageSize            int64    `json:"image_size"`
}
```

Define the remaining nested structs with every field observed in the approved
design document and fetched sample, including screenshots, rating statistics,
developer identity, and counts.

- [ ] **Step 4: Implement safe URL and application methods**

```go
func (client *PublicClient) GetApplication(ctx context.Context, packageName string) (PublicApplication, error)
func (client *PublicClient) VersionChangelog(ctx context.Context, packageName, version string) (string, error)
func (client *PublicClient) AssetURL(path string) (string, error)
func (client *PublicClient) DownloadURL(path string) (string, error)
func (client *PublicClient) ApplicationDownloadURL(app PublicApplication) (string, error)
```

`VersionChangelog` decodes the endpoint's JSON string response. Segment
validation permits ASCII letters, digits, `.`, `_`, and `-`, requires a leading
letter or digit, and rejects slashes, backslashes, whitespace, query markers,
fragments, and `.`/`..`. `ApplicationDownloadURL` requires non-empty matching
application/version package names and a non-empty package path.

- [ ] **Step 5: Add URL security and mismatch tests**

Assert the sample resolves to:

```text
https://dl.lazycatmicroserver.com/appstore/lpks/pkgs/wx.clawbot.lazycat.app.mediasaber/wx.clawbot.lazycat.app.mediasaber-v1.5.3.lpk
```

Also reject `../demo`, `demo/child`, a package/version mismatch, an HTTP URL
returned as a package path, and an asset path escaping the expected origin.

- [ ] **Step 6: Run and commit Task 2**

Run: `go test ./appstore -run 'TestPublicGetApplication|TestPublicVersionChangelog|TestPublicURLs' -count=1`

Expected: PASS.

```bash
git add appstore/public_types.go appstore/public_urls.go appstore/public_urls_test.go appstore/public_app.go appstore/public_app_test.go
git commit -m "feat: add public app metadata api"
```

---

### Task 3: Categories, Kinds, Homepage, and More Lists

**Files:**
- Create: `appstore/public_home.go`
- Create: `appstore/public_home_test.go`

**Interfaces:**
- Consumes: `CurrentRelease`, `PublicApplication`, URL validation, and `getJSON[T]`.
- Produces: `PublicCategory`, `PublicKind`, `HomepageBlock`, `Categories`, `Kinds`, `Homepage`, and `More`.

- [ ] **Step 1: Write failing stable dictionary tests**

```go
type PublicCategory struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
    Icon string `json:"icon"`
}

type PublicKind struct {
    ID       int    `json:"id"`
    Code     string `json:"code"`
    Name     string `json:"name"`
    OrderNum int    `json:"order_num"`
}
```

Assert `Categories` calls `/zh/categories.json`, `Kinds` calls
`/zh/app_kinds.json`, and neither request first calls `/op/index`.

- [ ] **Step 2: Write failing release snapshot tests**

Configure one test server to return `release-test-1` from `/op/index`, homepage
blocks from `/zh/release-test-1/homepage_block.json`, and a populated block from
`/zh/release-test-1/block_recents.json`. Assert the exact two-request sequence
for each release-scoped public method.

- [ ] **Step 3: Implement dictionary and block types and methods**

```go
type HomepageBlockOptions struct {
    ShowMore bool `json:"show_more"`
}

type HomepageBlock struct {
    ID                int                  `json:"id"`
    Name              string               `json:"name"`
    BlockType         string               `json:"block_type"`
    APIPath           string               `json:"api_path"`
    HomepageShowLimit int                  `json:"homepage_show_limit"`
    Options           *HomepageBlockOptions `json:"options"`
    Data              []PublicApplication  `json:"data"`
}

func (client *PublicClient) Categories(ctx context.Context) ([]PublicCategory, error)
func (client *PublicClient) Kinds(ctx context.Context) ([]PublicKind, error)
func (client *PublicClient) Homepage(ctx context.Context) ([]HomepageBlock, error)
func (client *PublicClient) More(ctx context.Context, blockType string) (HomepageBlock, error)
```

Homepage keeps `Data == nil` when the remote payload is `null`. `More` accepts
safe values such as `recents`, `ratings`, and numeric group IDs without hard
coding an exhaustive list, because the official homepage publishes group IDs.

- [ ] **Step 4: Add malformed snapshot and traversal tests**

Reject an invalid release returned by `/op/index`, an invalid block type such as
`../ratings`, malformed JSON, and a snapshot 404 with the standard SDK errors.

- [ ] **Step 5: Run and commit Task 3**

Run: `go test ./appstore -run 'TestPublicCategories|TestPublicKinds|TestPublicHomepage|TestPublicMore' -count=1`

Expected: PASS.

```bash
git add appstore/public_home.go appstore/public_home_test.go appstore/public_types.go
git commit -m "feat: add public app store discovery api"
```

---

### Task 4: Download and Developer Rankings

**Files:**
- Create: `appstore/public_rankings.go`
- Create: `appstore/public_rankings_test.go`
- Modify: `appstore/public_types.go`

**Interfaces:**
- Consumes: stable locale URL construction, `PublicApplication`, and `PublicDeveloper`.
- Produces: `RankingPeriod`, `RankingWeek`, `RankingMonth`, `RankingAll`, `DownloadRanking`, and `DeveloperRanking`.

- [ ] **Step 1: Write failing ranking tests**

Use `RankingWeek` and assert exact paths:

```text
/zh/app_download_week.json
/zh/developer_list_week.json
```

The download response is `[]PublicApplication`. The developer response is
`[]PublicDeveloper`, with `Apps []PublicApplication` added to
`PublicDeveloper`.

- [ ] **Step 2: Implement the ranking enum and methods**

```go
type RankingPeriod string

const (
    RankingWeek  RankingPeriod = "week"
    RankingMonth RankingPeriod = "month"
    RankingAll   RankingPeriod = "all"
)

func (client *PublicClient) DownloadRanking(ctx context.Context, period RankingPeriod) ([]PublicApplication, error)
func (client *PublicClient) DeveloperRanking(ctx context.Context, period RankingPeriod) ([]PublicDeveloper, error)
```

Only the three constants are accepted. Rankings do not call `CurrentRelease`.

- [ ] **Step 3: Add validation and response tests**

Assert an empty or unknown period returns `ErrInvalidArgument`, typed nested
applications decode, and no request carries authentication state.

- [ ] **Step 4: Run and commit Task 4**

Run: `go test ./appstore -run 'TestPublicDownloadRanking|TestPublicDeveloperRanking' -count=1`

Expected: PASS.

```bash
git add appstore/public_rankings.go appstore/public_rankings_test.go appstore/public_types.go
git commit -m "feat: add public app store rankings"
```

---

### Task 5: Documentation and Full Verification

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`

**Interfaces:**
- Consumes all public APIs from Tasks 1-4.
- Produces documented caller examples and final verified package behavior.

- [ ] **Step 1: Add the English anonymous catalog example**

```go
client := appstore.NewPublicClient(appstore.PublicOptions{})
application, err := client.GetApplication(ctx, "wx.clawbot.lazycat.app.mediasaber")
if err != nil {
    return err
}
downloadURL, err := client.ApplicationDownloadURL(application)
if err != nil {
    return err
}
fmt.Println(application.Version.Name, downloadURL)
```

State explicitly that no login or token is required, and list homepage,
categories, kinds, more lists, rankings, and changelog methods.

- [ ] **Step 2: Add the equivalent Chinese section**

Use the same identifiers and explain that `PublicClient` neither accepts nor
sends a Token. Mention metadata and download base URL overrides for mirrors and
tests.

- [ ] **Step 3: Format and run focused tests**

Run:

```bash
gofmt -w appstore/public_*.go
go test ./appstore -count=1
```

Expected: PASS with zero failures.

- [ ] **Step 4: Run repository-wide verification**

Run:

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
./scripts/check-import-boundaries.sh
git diff --check
```

Expected: every command exits 0; tests report no failures; import-boundary
script reports no forbidden imports; diff check prints no whitespace errors.

- [ ] **Step 5: Review the public surface and commit documentation**

Confirm `go doc ./appstore` shows `PublicClient` and all public response types,
and confirm `rg -n 'Token|X-User-Token|userToken' appstore/public_*.go` finds
only negative test assertions or documentation, never request construction.

```bash
git add README.md README.zh-CN.md appstore/public_*.go
git commit -m "docs: document public app store api"
```

