package appstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

const maxApplicationImageBytes = int64(15 << 20)

// UploadApplicationImage uploads one validated application screenshot and
// returns the developer-platform path used by an ApplicationInfo review.
func (client *Client) UploadApplicationImage(ctx context.Context, source io.Reader, fileName string) (string, error) {
	if ctx == nil || source == nil {
		return "", storeError(lpkgo.CodeInvalidArgument, "appstore.upload_application_image", errors.New("context and image are required"))
	}
	data, err := io.ReadAll(io.LimitReader(&testflightContextReader{ctx: ctx, reader: source}, maxApplicationImageBytes+1))
	if err != nil {
		return "", storeError(lpkgo.CodeCommandFailed, "appstore.upload_application_image", err)
	}
	if int64(len(data)) > maxApplicationImageBytes {
		return "", storeError(lpkgo.CodeInvalidArgument, "appstore.upload_application_image", errors.New("image exceeds limit"))
	}
	fileName, err = applicationImageFileName(fileName)
	if err != nil {
		return "", storeError(lpkgo.CodeInvalidArgument, "appstore.upload_application_image", err)
	}
	var encoded bytes.Buffer
	writer := multipart.NewWriter(&encoded)
	part, err := writer.CreateFormFile("file", fileName)
	if err == nil {
		_, err = part.Write(data)
	}
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", storeError(lpkgo.CodeCommandFailed, "appstore.upload_application_image", err)
	}
	request, err := client.newRequestAt(ctx, http.MethodPost, client.baseURL+"/api/v3/developer/upload", bytes.NewReader(encoded.Bytes()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	body, err := client.do(request)
	if err != nil {
		return "", err
	}
	var payload struct {
		URL  string `json:"url"`
		Data *struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", storeRemoteError("appstore.upload_application_image", errors.New("invalid image upload response"), http.StatusOK)
	}
	path := strings.TrimSpace(payload.URL)
	if path == "" && payload.Data != nil {
		path = strings.TrimSpace(payload.Data.URL)
	}
	if path == "" || len(path) > 4096 || strings.ContainsAny(path, "\r\n\x00") {
		return "", storeRemoteError("appstore.upload_application_image", errors.New("incomplete image upload response"), http.StatusOK)
	}
	return path, nil
}

func applicationImageFileName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "screenshot.png", nil
	}
	if len(value) > 124 || filepath.Base(value) != value || strings.ContainsAny(value, `/\\`) {
		return "", errors.New("image filename is invalid")
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '.' || character == '_' || character == '-' {
			continue
		}
		return "", errors.New("image filename is invalid")
	}
	extension := strings.ToLower(filepath.Ext(value))
	if value == "." || value == ".." || extension != ".png" && extension != ".jpg" && extension != ".jpeg" {
		return "", errors.New("image filename must use PNG or JPEG")
	}
	return value, nil
}
