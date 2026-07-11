// Package private implements the anonymous Miaomiao private community App
// Store protocol, including private group access codes.
package private

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

const maxResponseBytes = int64(4 << 20)

type Options struct {
	// BaseURL is the Miaomiao private store origin and is required.
	BaseURL string
	// HTTPClient supplies transport and timeout settings. Its CookieJar is
	// ignored and redirects are disabled to avoid forwarding group credentials.
	HTTPClient *http.Client
	// GroupCodes are default private group bearer codes for all requests.
	GroupCodes []string
	// GroupCodePlacement controls whether group codes use the header, query, or
	// both. The zero value uses only X-Group-Codes.
	GroupCodePlacement GroupCodePlacement
}

// Client queries a Miaomiao private community App Store.
type Client struct {
	baseURL            *url.URL
	httpClient         *http.Client
	groupCodes         []string
	groupCodePlacement GroupCodePlacement
}

// New constructs a private community-store client.
func New(options Options) (*Client, error) {
	baseURL, err := parseBaseURL(options.BaseURL)
	if err != nil {
		return nil, privateError(lpkgo.CodeInvalidArgument, "appstore.private.new", err, 0)
	}
	if !validGroupCodePlacement(options.GroupCodePlacement) {
		return nil, privateError(lpkgo.CodeInvalidArgument, "appstore.private.new", errors.New("invalid group code placement"), 0)
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	isolatedHTTPClient := *httpClient
	isolatedHTTPClient.Jar = nil
	isolatedHTTPClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{
		baseURL:            baseURL,
		httpClient:         &isolatedHTTPClient,
		groupCodes:         normalizeGroupCodes(options.GroupCodes),
		groupCodePlacement: options.GroupCodePlacement,
	}, nil
}

func privateError(code lpkgo.Code, op string, cause error, status int) error {
	return &lpkgo.Error{Code: code, Op: op, StatusCode: status, Retryable: code == lpkgo.CodeRemoteUnavailable && (status == 0 || status >= 500), Cause: cause}
}

func parseBaseURL(value string) (*url.URL, error) {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("invalid base URL")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}
