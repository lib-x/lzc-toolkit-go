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
	BaseURL            string
	HTTPClient         *http.Client
	GroupCodes         []string
	GroupCodePlacement GroupCodePlacement
}

type Client struct {
	baseURL            *url.URL
	httpClient         *http.Client
	groupCodes         []string
	groupCodePlacement GroupCodePlacement
}

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
