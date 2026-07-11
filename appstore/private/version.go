package private

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

type LatestVersionRequest struct {
	PackageID  string
	GroupCodes []string
}

type LatestVersion struct {
	PackageID     string  `json:"packageId"`
	LatestVersion Version `json:"latestVersion"`
}

type Version struct {
	ID          int        `json:"id"`
	AppID       int        `json:"appId"`
	UploaderID  int        `json:"uploaderId"`
	Version     string     `json:"version"`
	Changelog   string     `json:"changelog"`
	Status      string     `json:"status"`
	SourceType  string     `json:"sourceType"`
	DownloadURL string     `json:"downloadUrl"`
	StorageKey  string     `json:"storageKey,omitempty"`
	StoragePath string     `json:"storagePath,omitempty"`
	FileSize    int64      `json:"fileSize"`
	SHA256      string     `json:"sha256"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

func (client *Client) LatestVersion(ctx context.Context, input LatestVersionRequest) (LatestVersion, error) {
	packageID := strings.TrimSpace(input.PackageID)
	if ctx == nil || client == nil || !safeSegment(packageID) {
		return LatestVersion{}, privateError(lpkgo.CodeInvalidArgument, "appstore.private.latest_version", errors.New("invalid context, client, or package ID"), 0)
	}
	codes := mergeGroupCodes(client.groupCodes, input.GroupCodes)
	target := *client.baseURL
	target.Path = strings.TrimRight(target.Path, "/") + "/api/v1/packages/" + url.PathEscape(packageID) + "/latest-version"
	if len(codes) > 0 && (client.groupCodePlacement == GroupCodesQuery || client.groupCodePlacement == GroupCodesHeaderAndQuery) {
		query := target.Query()
		query.Set("groupCodes", strings.Join(codes, ","))
		target.RawQuery = query.Encode()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return LatestVersion{}, privateError(lpkgo.CodeInvalidArgument, "appstore.private.latest_version", err, 0)
	}
	if len(codes) > 0 && (client.groupCodePlacement == GroupCodesHeader || client.groupCodePlacement == GroupCodesHeaderAndQuery) {
		request.Header.Set("X-Group-Codes", strings.Join(codes, ","))
	}
	result, err := client.doRequest(request, "appstore.private.latest_version")
	if err != nil {
		return LatestVersion{}, err
	}
	if result.PackageID != packageID || strings.TrimSpace(result.LatestVersion.Version) == "" {
		return LatestVersion{}, privateError(lpkgo.CodeRemoteUnavailable, "appstore.private.latest_version", errors.New("latest version response identity mismatch"), http.StatusOK)
	}
	return result, nil
}

func (client *Client) doRequest(request *http.Request, op string) (LatestVersion, error) {
	var result LatestVersion
	if request == nil || client == nil || client.httpClient == nil {
		return result, privateError(lpkgo.CodeInvalidArgument, op, errors.New("nil request or client"), 0)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		if errors.Is(request.Context().Err(), context.DeadlineExceeded) {
			return result, privateError(lpkgo.CodeDeadlineExceeded, op, request.Context().Err(), 0)
		}
		if request.Context().Err() != nil {
			return result, privateError(lpkgo.CodeCancelled, op, request.Context().Err(), 0)
		}
		return result, privateError(lpkgo.CodeRemoteUnavailable, op, errors.New("private App Store request failed"), 0)
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if readErr != nil {
		return result, privateError(lpkgo.CodeRemoteUnavailable, op, errors.New("private App Store response failed"), response.StatusCode)
	}
	if int64(len(body)) > maxResponseBytes {
		return result, privateError(lpkgo.CodeRemoteUnavailable, op, errors.New("response exceeds limit"), response.StatusCode)
	}
	if response.StatusCode == http.StatusNotFound {
		return result, privateError(lpkgo.CodeNotFound, op, errors.New("application not found"), response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, privateError(lpkgo.CodeRemoteUnavailable, op, errors.New("private App Store request rejected"), response.StatusCode)
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return result, privateError(lpkgo.CodeRemoteUnavailable, op, errors.New("invalid JSON response"), response.StatusCode)
	}
	return result, nil
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
