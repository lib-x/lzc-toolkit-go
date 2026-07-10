// Package appstore implements LazyCat developer platform APIs used by
// lzc-cli 2.0.8. It does not execute Docker commands.
package appstore

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/auth"
)

const (
	DefaultBaseURL      = "https://appstore.api.lazycat.cloud"
	defaultPollInterval = time.Second
	maxResponseBytes    = 4 << 20
)

type Options struct {
	BaseURL      string
	HTTPClient   *http.Client
	Token        auth.TokenProvider
	PollInterval time.Duration
}

type Client struct {
	baseURL      string
	httpClient   *http.Client
	token        auth.TokenProvider
	pollInterval time.Duration
}

func New(options Options) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	interval := options.PollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	return &Client{baseURL: baseURL, httpClient: httpClient, token: options.Token, pollInterval: interval}
}

func (client *Client) newRequest(ctx context.Context, method, endpoint string) (*http.Request, error) {
	if ctx == nil || client == nil || client.httpClient == nil || client.token == nil {
		return nil, storeError(lpkgo.CodeInvalidArgument, "appstore.request", errors.New("nil context, client, or token provider"))
	}
	token, err := client.token.Token(ctx)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+endpoint, nil)
	if err != nil {
		return nil, storeError(lpkgo.CodeInvalidArgument, "appstore.request", err)
	}
	request.Header.Set("X-User-Token", token)
	request.AddCookie(&http.Cookie{Name: "userToken", Value: token, Path: "/"})
	return request, nil
}

func (client *Client) do(request *http.Request) ([]byte, error) {
	response, err := client.httpClient.Do(request)
	if err != nil {
		if request.Context().Err() != nil {
			return nil, storeError(lpkgo.CodeCancelled, "appstore.request", request.Context().Err())
		}
		return nil, storeRemoteError("appstore.request", err, 0)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, storeRemoteError("appstore.response", err, response.StatusCode)
	}
	if len(body) > maxResponseBytes {
		return nil, storeRemoteError("appstore.response", errors.New("response exceeds limit"), response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		code := lpkgo.CodeRemoteUnavailable
		if response.StatusCode == http.StatusUnauthorized {
			code = lpkgo.CodeUnauthenticated
		} else if response.StatusCode == http.StatusForbidden {
			code = lpkgo.CodePermissionDenied
		}
		return nil, &lpkgo.Error{Code: code, Op: "appstore.response", StatusCode: response.StatusCode, Retryable: response.StatusCode >= 500, Cause: errors.New("App Store request rejected")}
	}
	return body, nil
}

func storeError(code lpkgo.Code, op string, cause error) error {
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}

func storeRemoteError(op string, cause error, status int) error {
	return &lpkgo.Error{Code: lpkgo.CodeRemoteUnavailable, Op: op, StatusCode: status, Retryable: status == 0 || status >= 500, Cause: cause}
}
