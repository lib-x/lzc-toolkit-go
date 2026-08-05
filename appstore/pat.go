package appstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

const (
	DefaultPATBaseURL      = "https://appstore.api.lazycat.cloud"
	legacyDeveloperAPIPath = "/api/v3/developer"
	patDeveloperAPIPath    = "/sdk/v3/developer"
)

// ResolvePATBaseURL converts a domain-only PAT API host into its HTTPS base URL.
func ResolvePATBaseURL(configuredHost string) (string, error) {
	host := strings.TrimSpace(configuredHost)
	if host == "" {
		return DefaultPATBaseURL, nil
	}
	if strings.Contains(host, "://") || strings.ContainsAny(host, ":/?#@") || strings.ContainsAny(host, " \t\r\n") {
		return "", storeError(lpkgo.CodeInvalidArgument, "appstore.pat_base_url", errors.New("PAT API host must contain only a host name"))
	}
	return "https://" + host, nil
}

// NewPAT constructs a developer-platform client for personal access tokens.
// Existing New callers retain the legacy lzc-cli session-token protocol.
func NewPAT(options Options) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultPATBaseURL
	}
	httpClient, err := NewPATHTTPClient(baseURL, options.HTTPClient)
	if err != nil {
		return nil, err
	}
	options.BaseURL = baseURL
	options.HTTPClient = httpClient
	return New(options), nil
}

// NewPATHTTPClient adapts legacy developer request shapes to the PAT SDK API.
func NewPATHTTPClient(baseURL string, base *http.Client) (*http.Client, error) {
	origin, err := parsePATOrigin(baseURL)
	if err != nil {
		return nil, storeError(lpkgo.CodeInvalidArgument, "appstore.pat_http_client", err)
	}
	if base == nil {
		base = &http.Client{Timeout: 30 * time.Second}
	}
	cloned := *base
	transport := cloned.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	cloned.Transport = patTransport{base: transport, origin: origin}
	cloned.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &cloned, nil
}

func parsePATOrigin(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return "", errors.New("PAT API base URL is invalid")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" || parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("PAT API base URL is invalid")
	}
	return strings.ToLower(parsed.Scheme + "://" + parsed.Host), nil
}

type patTransport struct {
	base   http.RoundTripper
	origin string
}

func (transport patTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.Header = request.Header.Clone()
	token := strings.TrimSpace(cloned.Header.Get("X-User-Token"))
	cloned.Header.Del("X-User-Token")
	cloned.Header.Del("X-API-Token")
	cloned.Header.Del("Cookie")

	sameOrigin := strings.EqualFold(cloned.URL.Scheme+"://"+cloned.URL.Host, transport.origin)
	if sameOrigin && developerPath(cloned.URL.Path, legacyDeveloperAPIPath) {
		urlCopy := *cloned.URL
		urlCopy.Path = patDeveloperAPIPath + strings.TrimPrefix(cloned.URL.Path, legacyDeveloperAPIPath)
		cloned.URL = &urlCopy
		if token != "" {
			cloned.Header.Set("X-API-Token", token)
		}
	}

	response, err := transport.base.RoundTrip(cloned)
	if err != nil || response == nil || !sameOrigin || !developerPath(cloned.URL.Path, patDeveloperAPIPath) {
		return response, err
	}
	return unwrapPATResponse(response)
}

func developerPath(value, prefix string) bool {
	return value == prefix || strings.HasPrefix(value, prefix+"/")
}

func unwrapPATResponse(response *http.Response) (*http.Response, error) {
	if response.Body == nil {
		return response, nil
	}
	body := response.Body
	prefix, err := io.ReadAll(io.LimitReader(body, maxResponseBytes+1))
	if err != nil {
		_ = body.Close()
		return nil, err
	}
	if len(prefix) > maxResponseBytes {
		response.Body = &readCloser{Reader: io.MultiReader(bytes.NewReader(prefix), body), Closer: body}
		return response, nil
	}
	if err := body.Close(); err != nil {
		return nil, err
	}

	var envelope map[string]json.RawMessage
	if json.Unmarshal(prefix, &envelope) != nil {
		return replaceResponseBody(response, prefix), nil
	}
	rawCode, found := envelope["errorCode"]
	if !found {
		return replaceResponseBody(response, prefix), nil
	}
	var errorCode int
	if json.Unmarshal(rawCode, &errorCode) != nil {
		return replaceResponseBody(response, prefix), nil
	}
	payload := envelope["data"]
	if errorCode != 0 {
		if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			response.StatusCode = http.StatusBadRequest
			response.Status = "400 Bad Request"
		}
		var message string
		_ = json.Unmarshal(envelope["msg"], &message)
		payload, _ = json.Marshal(map[string]string{"message": strings.TrimSpace(message)})
	} else if len(payload) == 0 || bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
		payload = []byte("{}")
	}
	return replaceResponseBody(response, payload), nil
}

type readCloser struct {
	io.Reader
	io.Closer
}

func replaceResponseBody(response *http.Response, body []byte) *http.Response {
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	response.TransferEncoding = nil
	if response.Header == nil {
		response.Header = make(http.Header)
	}
	response.Header.Set("Content-Length", strconv.Itoa(len(body)))
	response.Header.Del("Transfer-Encoding")
	return response
}
