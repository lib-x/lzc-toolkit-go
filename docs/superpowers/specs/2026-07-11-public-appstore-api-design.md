# Public App Store API Design

## Goal

Add anonymous, read-only official App Store APIs to the existing `appstore`
package. The new API reads the public metarepo hosted by LazyCat and constructs
official asset and LPK download URLs without requiring an account token.

The existing `Client` remains the authenticated developer-platform client used
for publishing, image copying, TestFlight, and other developer operations.

## Public protocol

The implementation uses the endpoints consumed by the official store frontend:

| Capability | Public path |
| --- | --- |
| Current release | `/appstore/metarepo/op/index` |
| Application detail | `/appstore/metarepo/{locale}/v3/app_{package}.json` |
| Categories | `/appstore/metarepo/{locale}/categories.json` |
| Application kinds | `/appstore/metarepo/{locale}/app_kinds.json` |
| Homepage | `/appstore/metarepo/{locale}/{release}/homepage_block.json` |
| More-list block | `/appstore/metarepo/{locale}/{release}/block_{type}.json` |
| Download ranking | `/appstore/metarepo/{locale}/app_download_{period}.json` |
| Developer ranking | `/appstore/metarepo/{locale}/developer_list_{period}.json` |
| Version changelog | `/appstore/metarepo/{locale}/apps/{package}/{version}.changelog.json` |

Metadata and image assets default to `https://dl.lazycat.cloud`. LPK downloads
default to `https://dl.lazycatmicroserver.com` plus the `pkg_path` returned by
the application metadata.

Homepage and more-list calls first fetch `op/index`, validate the returned
release name, and then fetch the requested snapshot file. Categories, kinds,
application details, rankings, and changelogs use the stable locale directory.
The client does not parse the `lazycat.cloud/appstore` HTML page.

## Package API

All new declarations remain in package `appstore`, but use a separate client:

```go
client := appstore.NewPublicClient(appstore.PublicOptions{})

app, err := client.GetApplication(ctx, "wx.clawbot.lazycat.app.mediasaber")
homepage, err := client.Homepage(ctx)
categories, err := client.Categories(ctx)
kinds, err := client.Kinds(ctx)
```

`PublicOptions` contains:

- `MetadataBaseURL`, defaulting to `https://dl.lazycat.cloud/appstore/metarepo`.
- `DownloadBaseURL`, defaulting to `https://dl.lazycatmicroserver.com`.
- `Locale`, defaulting to `zh`.
- `HTTPClient`, defaulting to an HTTP client with a 30-second timeout.

`PublicClient` intentionally has no token provider. Its requests never set
`X-User-Token`, `Authorization`, or a `userToken` cookie.

The first public surface contains:

- `GetApplication(ctx, packageName)` for the latest application metadata and
  version.
- `Categories(ctx)` and `Kinds(ctx)` for stable dictionaries.
- `Homepage(ctx)` for the current release's configured homepage blocks.
- `More(ctx, blockType)` for release-scoped store lists such as recents and
  ratings.
- `DownloadRanking(ctx, period)` and `DeveloperRanking(ctx, period)` for the
  stable `week`, `month`, and `all` rankings.
- `VersionChangelog(ctx, packageName, version)` for the full changelog resource.
- `AssetURL(path)` and `DownloadURL(path)` for resolving server-returned paths.
- `ApplicationDownloadURL(app)` for validating the application/version match
  and resolving the current LPK URL.

Free-form path inputs are not exposed. Package names, versions, block types,
periods, locales, and release identifiers are validated as single safe path
segments before URL construction.

## Data model

The public response types follow the observed JSON rather than reshaping it into
the authenticated developer API types:

- `PublicApplication`
- `PublicApplicationInfo`
- `PublicVersion`
- `PublicDeveloper`
- `PublicRating` and `PublicRatingStatistics`
- `PublicCounts`
- `PublicCategory`
- `PublicKind`
- `HomepageBlock`

JSON field names remain represented by struct tags. RFC3339 timestamps use
`time.Time`. Unstable or block-specific homepage payloads retain a typed block
envelope and decode application lists where the official payload supplies them.
Unknown additive JSON fields are ignored so the client remains forward
compatible.

The raw relative asset and package paths remain available in the returned
types. URL resolution is explicit through client methods so custom base URLs
continue to work in tests and mirrors.

## Files

The implementation is split by responsibility inside `appstore`:

- `public_client.go`: options, client construction, bounded anonymous HTTP, and
  release lookup.
- `public_types.go`: shared public response models.
- `public_app.go`: application detail and version changelog APIs.
- `public_home.go`: categories, kinds, homepage, and more-list APIs.
- `public_rankings.go`: download and developer ranking APIs.
- `public_urls.go`: path validation and asset/LPK URL resolution.
- Matching `*_test.go` files for each concern.

This keeps anonymous catalog behavior separate from the authenticated request
implementation in `client.go` while preserving one import path for callers.

## Errors and limits

The implementation uses `lpkgo.Error` consistently:

- Invalid package names, versions, locales, periods, block types, paths, or nil
  contexts return `INVALID_ARGUMENT`.
- HTTP 404 returns `NOT_FOUND`.
- Other non-2xx responses and malformed remote data return
  `REMOTE_UNAVAILABLE`.
- Context cancellation returns `CANCELLED`.
- Oversized responses are rejected as `REMOTE_UNAVAILABLE`.

Response bodies are bounded. Error values do not include complete remote
bodies, tokens, or instruction-like web content.

## Tests and documentation

Tests use `httptest.Server` and verify:

- The exact package-derived metadata and changelog paths.
- The release-index then snapshot request sequence.
- Category, kind, block, and ranking decoding.
- Correct metadata, asset, and LPK base URL resolution.
- No authentication headers or cookies are sent.
- Package/version mismatch, traversal attempts, malformed JSON, 404 responses,
  cancellation, and response limits.

The English and Chinese READMEs receive an anonymous public-store example that
fetches an application and prints its latest version and official LPK URL.
