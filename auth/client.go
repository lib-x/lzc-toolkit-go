package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	lpkgo "github.com/lib-x/lpk-go"
)

const maxResponseBytes = 1 << 20

type ClientOptions struct {
	BaseURL    string
	HTTPClient *http.Client
	Store      TokenStore
}

type Client struct {
	baseURL    string
	httpClient *http.Client
	store      TokenStore
}

func NewClient(options ClientOptions) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultAccountURL
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{baseURL: baseURL, httpClient: httpClient, store: options.Store}
}

func (client *Client) Login(ctx context.Context, credentials Credentials) (Session, error) {
	if err := authContextError(ctx, "auth.login"); err != nil {
		return Session{}, err
	}
	if client == nil || client.httpClient == nil {
		return Session{}, authError(lpkgo.CodeInvalidArgument, "auth.login", errors.New("nil client"))
	}
	username := strings.TrimSpace(credentials.Username)
	if username == "" || credentials.Password == "" {
		return Session{}, authError(lpkgo.CodeInvalidArgument, "auth.login", errors.New("username and password are required"))
	}
	form := url.Values{"username": {username}, "password": {credentials.Password}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/api/login/signin", strings.NewReader(form.Encode()))
	if err != nil {
		return Session{}, authError(lpkgo.CodeInvalidArgument, "auth.login", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return Session{}, authError(lpkgo.CodeCancelled, "auth.login", ctx.Err())
		}
		return Session{}, remoteError("auth.login", err, 0)
	}
	body, readErr := readResponse(response)
	if readErr != nil {
		return Session{}, readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		code := lpkgo.CodeRemoteUnavailable
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			code = lpkgo.CodeUnauthenticated
		}
		return Session{}, &lpkgo.Error{Code: code, Op: "auth.login", StatusCode: response.StatusCode, Retryable: response.StatusCode >= 500, Cause: errors.New("login rejected")}
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Session{}, remoteError("auth.login", errors.New("invalid login response"), response.StatusCode)
	}
	token := strings.TrimSpace(payload.Data.Token)
	if !payload.Success || token == "" {
		return Session{}, &lpkgo.Error{Code: lpkgo.CodeUnauthenticated, Op: "auth.login", StatusCode: response.StatusCode, Cause: errors.New("login rejected")}
	}
	if client.store != nil {
		if err := client.store.Save(ctx, token); err != nil {
			return Session{}, err
		}
	}
	return Session{Token: token}, nil
}

func (client *Client) Validate(ctx context.Context, token string) error {
	if err := authContextError(ctx, "auth.validate"); err != nil {
		return err
	}
	if client == nil || client.httpClient == nil {
		return authError(lpkgo.CodeInvalidArgument, "auth.validate", errors.New("nil client"))
	}
	normalized, err := requireToken(token, "auth.validate")
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+"/api/user/current", nil)
	if err != nil {
		return authError(lpkgo.CodeInvalidArgument, "auth.validate", err)
	}
	request.Header.Set("X-User-Token", normalized)
	response, err := client.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return authError(lpkgo.CodeCancelled, "auth.validate", ctx.Err())
		}
		return remoteError("auth.validate", err, 0)
	}
	body, readErr := readResponse(response)
	if readErr != nil {
		return readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &lpkgo.Error{Code: lpkgo.CodeUnauthenticated, Op: "auth.validate", StatusCode: response.StatusCode, Cause: errors.New("token rejected")}
	}
	var payload struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return remoteError("auth.validate", errors.New("invalid validation response"), response.StatusCode)
	}
	if !payload.Success {
		return &lpkgo.Error{Code: lpkgo.CodeUnauthenticated, Op: "auth.validate", StatusCode: response.StatusCode, Cause: errors.New("token rejected")}
	}
	return nil
}

func readResponse(response *http.Response) ([]byte, error) {
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, remoteError("auth.response", err, response.StatusCode)
	}
	if len(body) > maxResponseBytes {
		return nil, remoteError("auth.response", errors.New("response exceeds limit"), response.StatusCode)
	}
	return body, nil
}

func remoteError(op string, cause error, status int) error {
	return &lpkgo.Error{Code: lpkgo.CodeRemoteUnavailable, Op: op, StatusCode: status, Retryable: status == 0 || status >= 500, Cause: cause}
}
