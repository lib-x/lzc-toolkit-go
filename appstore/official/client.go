// Package official implements the anonymous, read-only LazyCat official App
// Store catalog protocol.
package official

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

const (
	DefaultMetadataBaseURL = "https://dl.lazycat.cloud/appstore/metarepo"
	DefaultDownloadBaseURL = "https://dl.lazycatmicroserver.com"
	defaultLocale          = "zh"
	maxResponseBytes       = int64(8 << 20)
)

type Options struct {
	MetadataBaseURL string
	DownloadBaseURL string
	Locale          string
	HTTPClient      *http.Client
}

type Client struct {
	metadataBase *url.URL
	downloadBase *url.URL
	locale       string
	httpClient   *http.Client
	initErr      error
}

func New(options Options) *Client {
	metadataBase, metadataErr := parseBaseURL(options.MetadataBaseURL, DefaultMetadataBaseURL)
	downloadBase, downloadErr := parseBaseURL(options.DownloadBaseURL, DefaultDownloadBaseURL)
	locale := strings.TrimSpace(options.Locale)
	if locale == "" {
		locale = defaultLocale
	}
	if !safeSegment(locale) {
		metadataErr = errors.Join(metadataErr, errors.New("invalid locale"))
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		metadataBase: metadataBase,
		downloadBase: downloadBase,
		locale:       locale,
		httpClient:   httpClient,
		initErr:      errors.Join(metadataErr, downloadErr),
	}
}

func (client *Client) CurrentRelease(ctx context.Context) (string, error) {
	if err := client.validate(ctx, "appstore.official.current_release"); err != nil {
		return "", err
	}
	target := client.resolveMetadata("op", "index")
	body, err := client.getBytes(ctx, target, "appstore.official.current_release")
	if err != nil {
		return "", err
	}
	release := strings.TrimSpace(string(body))
	if strings.HasPrefix(release, `"`) {
		if err := json.Unmarshal(body, &release); err != nil {
			return "", officialError(lpkgo.CodeRemoteUnavailable, "appstore.official.current_release", err, http.StatusOK)
		}
		release = strings.TrimSpace(release)
	}
	if !safeSegment(release) {
		return "", officialError(lpkgo.CodeRemoteUnavailable, "appstore.official.current_release", errors.New("invalid release response"), http.StatusOK)
	}
	return release, nil
}

func (client *Client) validate(ctx context.Context, op string) error {
	if ctx == nil || client == nil || client.httpClient == nil || client.metadataBase == nil || client.downloadBase == nil {
		return officialError(lpkgo.CodeInvalidArgument, op, errors.New("nil context or client"), 0)
	}
	if client.initErr != nil {
		return officialError(lpkgo.CodeInvalidArgument, op, client.initErr, 0)
	}
	return nil
}

func (client *Client) stable(parts ...string) string {
	return client.resolveMetadata(append([]string{client.locale}, parts...)...)
}

func (client *Client) snapshot(release string, parts ...string) string {
	return client.resolveMetadata(append([]string{client.locale, release}, parts...)...)
}

func (client *Client) resolveMetadata(parts ...string) string {
	target := *client.metadataBase
	segments := append([]string{strings.TrimRight(target.Path, "/")}, parts...)
	target.Path = strings.Join(segments, "/")
	return target.String()
}

func (client *Client) getBytes(ctx context.Context, target, op string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, officialError(lpkgo.CodeInvalidArgument, op, err, 0)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, officialError(lpkgo.CodeDeadlineExceeded, op, ctx.Err(), 0)
		}
		if ctx.Err() != nil {
			return nil, officialError(lpkgo.CodeCancelled, op, ctx.Err(), 0)
		}
		return nil, officialError(lpkgo.CodeRemoteUnavailable, op, err, 0)
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if readErr != nil {
		return nil, officialError(lpkgo.CodeRemoteUnavailable, op, readErr, response.StatusCode)
	}
	if int64(len(body)) > maxResponseBytes {
		return nil, officialError(lpkgo.CodeRemoteUnavailable, op, errors.New("response exceeds limit"), response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		code := lpkgo.CodeRemoteUnavailable
		if response.StatusCode == http.StatusNotFound {
			code = lpkgo.CodeNotFound
		}
		return nil, officialError(code, op, errors.New("official App Store request rejected"), response.StatusCode)
	}
	return body, nil
}

func getJSON[T any](ctx context.Context, client *Client, target, op string) (T, error) {
	var result T
	if err := client.validate(ctx, op); err != nil {
		return result, err
	}
	body, err := client.getBytes(ctx, target, op)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return result, officialError(lpkgo.CodeRemoteUnavailable, op, errors.New("invalid JSON response"), http.StatusOK)
	}
	return result, nil
}

func officialError(code lpkgo.Code, op string, cause error, status int) error {
	return &lpkgo.Error{
		Code:       code,
		Op:         op,
		StatusCode: status,
		Retryable:  status == 0 || status >= 500,
		Cause:      cause,
	}
}

func parseBaseURL(value, fallback string) (*url.URL, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("invalid base URL")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

func safeSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			continue
		}
		if index > 0 && (char == '.' || char == '_' || char == '-') {
			continue
		}
		return false
	}
	return true
}
