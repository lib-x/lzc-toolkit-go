package appstore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strings"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

const defaultAPKTriggerTimeout = 5 * time.Second

// APKRequest describes an APK shell generation request.
type APKRequest struct {
	AppID    string
	Names    map[string]string
	Icon     io.Reader
	IconName string
	Timeout  time.Duration
}

// APKResult reports whether the APK generation request was accepted.
type APKResult struct {
	AppID       string
	StatusCode  int
	Accepted    bool
	NotModified bool
}

// TriggerAPK requests generation of the latest Android APK shell for an app.
// It follows lzc-cli 2.0.8 and does not authenticate this endpoint. Caller-
// owned Icon readers are never closed.
func (client *Client) TriggerAPK(ctx context.Context, input APKRequest) (APKResult, error) {
	if ctx == nil || client == nil || client.httpClient == nil {
		return APKResult{}, storeError(lpkgo.CodeInvalidArgument, "appstore.trigger_apk", errors.New("nil context or client"))
	}
	appID := strings.TrimSpace(input.AppID)
	if appID == "" {
		return APKResult{}, storeError(lpkgo.CodeInvalidArgument, "appstore.trigger_apk", errors.New("app ID is required"))
	}
	names, err := normalizeAPKNames(input.Names)
	if err != nil {
		return APKResult{}, err
	}
	iconName, err := normalizeAPKIconName(input.IconName)
	if err != nil {
		return APKResult{}, err
	}
	timeout := input.Timeout
	if timeout == 0 {
		timeout = defaultAPKTriggerTimeout
	}
	if timeout < 0 {
		return APKResult{}, storeError(lpkgo.CodeInvalidArgument, "appstore.trigger_apk", errors.New("timeout must not be negative"))
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	writeDone := make(chan error, 1)
	go func() {
		defer close(writeDone)
		writeErr := multipartWriter.WriteField("app_id", appID)
		if writeErr == nil && len(names) > 0 {
			var encoded []byte
			encoded, writeErr = json.Marshal(names)
			if writeErr == nil {
				writeErr = multipartWriter.WriteField("app_name_map", string(encoded))
			}
		}
		if writeErr == nil && input.Icon != nil {
			var part io.Writer
			part, writeErr = multipartWriter.CreateFormFile("app_icon", iconName)
			if writeErr == nil {
				_, writeErr = io.Copy(part, &testflightContextReader{ctx: ctx, reader: input.Icon})
			}
		}
		if writeErr == nil {
			writeErr = multipartWriter.Close()
		}
		if writeErr != nil {
			_ = writer.CloseWithError(writeErr)
		} else {
			writeErr = writer.Close()
		}
		writeDone <- writeErr
	}()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/api/trigger_latest_for_app", reader)
	if err != nil {
		_ = reader.CloseWithError(err)
		<-writeDone
		return APKResult{}, storeError(lpkgo.CodeInvalidArgument, "appstore.trigger_apk", err)
	}
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	response, requestErr := client.httpClient.Do(request)
	if requestErr != nil {
		_ = reader.CloseWithError(requestErr)
	}
	writeErr := <-writeDone
	if requestErr != nil {
		if ctx.Err() != nil {
			return APKResult{}, storeError(lpkgo.CodeCancelled, "appstore.trigger_apk", ctx.Err())
		}
		return APKResult{}, storeRemoteError("appstore.trigger_apk", requestErr, 0)
	}
	defer response.Body.Close()
	if writeErr != nil {
		return APKResult{}, storeError(lpkgo.CodeCommandFailed, "appstore.trigger_apk", writeErr)
	}
	read, err := io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return APKResult{}, storeRemoteError("appstore.trigger_apk", err, response.StatusCode)
	}
	if read > maxResponseBytes {
		return APKResult{}, storeRemoteError("appstore.trigger_apk", errors.New("response exceeds limit"), response.StatusCode)
	}
	result := APKResult{
		AppID:       appID,
		StatusCode:  response.StatusCode,
		Accepted:    (response.StatusCode >= 200 && response.StatusCode < 300) || response.StatusCode == http.StatusNotModified,
		NotModified: response.StatusCode == http.StatusNotModified,
	}
	if !result.Accepted {
		return result, &lpkgo.Error{
			Code:       lpkgo.CodeRemoteUnavailable,
			Op:         "appstore.trigger_apk",
			StatusCode: response.StatusCode,
			Retryable:  response.StatusCode >= 500,
			Cause:      errors.New("APK generation request rejected"),
		}
	}
	return result, nil
}

func normalizeAPKNames(source map[string]string) (map[string]string, error) {
	if len(source) == 0 {
		return nil, nil
	}
	result := make(map[string]string, len(source))
	for locale, name := range source {
		locale = strings.TrimSpace(locale)
		name = strings.TrimSpace(name)
		if locale == "" || name == "" {
			return nil, storeError(lpkgo.CodeInvalidArgument, "appstore.trigger_apk", errors.New("APK name locale and value are required"))
		}
		if _, exists := result[locale]; exists {
			return nil, storeError(lpkgo.CodeInvalidArgument, "appstore.trigger_apk", errors.New("duplicate APK name locale"))
		}
		result[locale] = name
	}
	return result, nil
}

func normalizeAPKIconName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.ContainsAny(value, "\r\n") {
		return "", storeError(lpkgo.CodeInvalidArgument, "appstore.trigger_apk", errors.New("invalid icon filename"))
	}
	if value == "" {
		return "icon.png", nil
	}
	value = path.Base(strings.ReplaceAll(value, "\\", "/"))
	if value == "." || value == "/" || value == "" {
		return "icon.png", nil
	}
	return value, nil
}
