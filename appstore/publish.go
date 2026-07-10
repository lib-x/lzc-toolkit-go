package appstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/lint"
	"github.com/lib-x/lzc-toolkit-go/lpk"
)

const defaultMaxPublishBytes = int64(32 << 30)

type CreateApplicationRequest struct {
	Package      string `json:"package"`
	Language     string `json:"language"`
	Name         string `json:"name"`
	Source       string `json:"source,omitempty"`
	SourceAuthor string `json:"source_author,omitempty"`
}

type PublishRequest struct {
	Package         io.Reader
	FileName        string
	Changelogs      map[string]string
	CreateIfMissing bool
	Application     *CreateApplicationRequest
	MaxPackageBytes int64
}

type UploadInfo struct {
	Package              string   `json:"package"`
	Version              string   `json:"version"`
	IconPath             string   `json:"iconPath"`
	URL                  string   `json:"url"`
	SHA256               string   `json:"sha256"`
	UnsupportedPlatforms []string `json:"unsupportedPlatforms"`
	MinOSVersion         string   `json:"minOsVersion"`
	LPKSize              int64    `json:"lpkSize"`
	ImageSize            int64    `json:"imageSize"`
}

type PublishResult struct {
	Created  bool
	Upload   UploadInfo
	Response json.RawMessage
}

func (client *Client) Publish(ctx context.Context, input PublishRequest) (PublishResult, error) {
	if ctx == nil || input.Package == nil || len(input.Changelogs) == 0 {
		return PublishResult{}, storeError(lpkgo.CodeInvalidArgument, "appstore.publish", errors.New("package and changelogs are required"))
	}
	for locale, changelog := range input.Changelogs {
		if strings.TrimSpace(locale) == "" || strings.TrimSpace(changelog) == "" {
			return PublishResult{}, storeError(lpkgo.CodeInvalidArgument, "appstore.publish", errors.New("changelog locale and content are required"))
		}
	}
	work, err := os.MkdirTemp("", "lpk-publish-*")
	if err != nil {
		return PublishResult{}, storeError(lpkgo.CodeCommandFailed, "appstore.publish", err)
	}
	defer os.RemoveAll(work)
	packagePath := filepath.Join(work, "application.lpk")
	if err := spoolPublishPackage(ctx, input.Package, packagePath, input.MaxPackageBytes); err != nil {
		return PublishResult{}, err
	}
	packageID, err := precheckPublishPackage(ctx, packagePath, filepath.Join(work, "root"))
	if err != nil {
		return PublishResult{}, err
	}
	exists, err := client.CheckApplication(ctx, packageID)
	if err != nil {
		return PublishResult{}, err
	}
	result := PublishResult{}
	if !exists {
		if !input.CreateIfMissing || input.Application == nil {
			return PublishResult{}, storeError(lpkgo.CodeNotFound, "appstore.publish", errors.New("application does not exist"))
		}
		application := *input.Application
		if strings.TrimSpace(application.Package) == "" {
			application.Package = packageID
		}
		if application.Package != packageID {
			return PublishResult{}, storeError(lpkgo.CodeInvalidArgument, "appstore.publish", errors.New("application package does not match LPK"))
		}
		if err := client.CreateApplication(ctx, application); err != nil {
			return PublishResult{}, err
		}
		result.Created = true
	}
	filename := strings.TrimSpace(input.FileName)
	if filename == "" {
		filename = filepath.Base(packagePath)
	}
	upload, err := client.uploadLPK(ctx, packagePath, filename)
	if err != nil {
		return PublishResult{}, err
	}
	if strings.TrimSpace(upload.Package) == "" || strings.TrimSpace(upload.Version) == "" || strings.TrimSpace(upload.URL) == "" || strings.TrimSpace(upload.SHA256) == "" {
		return PublishResult{}, storeRemoteError("appstore.publish_upload", errors.New("incomplete upload response"), http.StatusOK)
	}
	if strings.TrimSpace(upload.Package) != packageID {
		return PublishResult{}, storeRemoteError("appstore.publish_upload", errors.New("upload response package does not match LPK"), http.StatusOK)
	}
	reviewBody := struct {
		Version struct {
			Package              string            `json:"package"`
			Name                 string            `json:"name"`
			IconPath             string            `json:"icon_path"`
			PackagePath          string            `json:"pkg_path"`
			PackageHash          string            `json:"pkg_hash"`
			UnsupportedPlatforms []string          `json:"unsupported_platforms"`
			MinOSVersion         string            `json:"min_os_version"`
			LPKSize              int64             `json:"lpk_size"`
			ImageSize            int64             `json:"image_size"`
			Changelogs           map[string]string `json:"changelogs"`
		} `json:"version"`
	}{}
	reviewBody.Version.Package = upload.Package
	reviewBody.Version.Name = upload.Version
	reviewBody.Version.IconPath = upload.IconPath
	reviewBody.Version.PackagePath = upload.URL
	reviewBody.Version.PackageHash = upload.SHA256
	reviewBody.Version.UnsupportedPlatforms = upload.UnsupportedPlatforms
	reviewBody.Version.MinOSVersion = upload.MinOSVersion
	reviewBody.Version.LPKSize = upload.LPKSize
	reviewBody.Version.ImageSize = upload.ImageSize
	reviewBody.Version.Changelogs = cloneChangelogs(input.Changelogs)
	encoded, err := json.Marshal(reviewBody)
	if err != nil {
		return PublishResult{}, storeError(lpkgo.CodeInvalidArgument, "appstore.publish_review", err)
	}
	reviewRequest, err := client.newRequestAt(ctx, http.MethodPost, client.baseURL+"/api/v3/developer/app/"+url.PathEscape(upload.Package)+"/review/create", strings.NewReader(string(encoded)))
	if err != nil {
		return PublishResult{}, err
	}
	response, err := client.do(reviewRequest)
	if err != nil {
		return PublishResult{}, err
	}
	result.Upload = upload
	result.Response = append(json.RawMessage(nil), response...)
	return result, nil
}

func (client *Client) CheckApplication(ctx context.Context, packageID string) (bool, error) {
	packageID = strings.TrimSpace(packageID)
	if packageID == "" {
		return false, storeError(lpkgo.CodeInvalidArgument, "appstore.check_application", errors.New("package is required"))
	}
	query := url.Values{"package": {packageID}}.Encode()
	request, err := client.newRequest(ctx, http.MethodGet, "/api/v3/developer/app/check/exist?"+query)
	if err != nil {
		return false, err
	}
	body, err := client.do(request)
	if err != nil {
		return false, err
	}
	var response struct {
		Exist bool `json:"exist"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return false, storeRemoteError("appstore.check_application", errors.New("invalid existence response"), http.StatusOK)
	}
	return response.Exist, nil
}

func (client *Client) CreateApplication(ctx context.Context, input CreateApplicationRequest) error {
	if strings.TrimSpace(input.Package) == "" || strings.TrimSpace(input.Language) == "" || strings.TrimSpace(input.Name) == "" {
		return storeError(lpkgo.CodeInvalidArgument, "appstore.create_application", errors.New("package, language, and name are required"))
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return storeError(lpkgo.CodeInvalidArgument, "appstore.create_application", err)
	}
	request, err := client.newRequestAt(ctx, http.MethodPost, client.baseURL+"/api/v3/developer/app/create", strings.NewReader(string(encoded)))
	if err != nil {
		return err
	}
	_, err = client.do(request)
	return err
}

func (client *Client) uploadLPK(ctx context.Context, filename, formFilename string) (UploadInfo, error) {
	file, err := os.Open(filename)
	if err != nil {
		return UploadInfo{}, storeError(lpkgo.CodeCommandFailed, "appstore.publish_upload", err)
	}
	defer file.Close()
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	done := make(chan error, 1)
	go func() {
		part, err := multipartWriter.CreateFormFile("file", formFilename)
		if err == nil {
			_, err = io.Copy(part, &testflightContextReader{ctx: ctx, reader: file})
		}
		if err == nil {
			err = multipartWriter.Close()
		}
		if err != nil {
			_ = writer.CloseWithError(err)
		} else {
			err = writer.Close()
		}
		done <- err
	}()
	request, err := client.newRequestAt(ctx, http.MethodPost, client.baseURL+"/api/v3/developer/app/lpk/upload", reader)
	if err != nil {
		_ = reader.CloseWithError(err)
		<-done
		return UploadInfo{}, err
	}
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	body, requestErr := client.do(request)
	if requestErr != nil {
		_ = reader.CloseWithError(requestErr)
	}
	writeErr := <-done
	if requestErr != nil {
		return UploadInfo{}, requestErr
	}
	if writeErr != nil {
		return UploadInfo{}, storeError(lpkgo.CodeCommandFailed, "appstore.publish_upload", writeErr)
	}
	var upload UploadInfo
	if err := json.Unmarshal(body, &upload); err != nil {
		return UploadInfo{}, storeRemoteError("appstore.publish_upload", errors.New("invalid upload response"), http.StatusOK)
	}
	return upload, nil
}

func spoolPublishPackage(ctx context.Context, source io.Reader, destination string, limit int64) error {
	if limit <= 0 {
		limit = defaultMaxPublishBytes
	}
	file, err := os.Create(destination)
	if err != nil {
		return storeError(lpkgo.CodeCommandFailed, "appstore.publish_spool", err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(&testflightContextReader{ctx: ctx, reader: source}, limit+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return storeError(lpkgo.CodeCommandFailed, "appstore.publish_spool", errors.Join(copyErr, closeErr))
	}
	if written > limit {
		return storeError(lpkgo.CodeInvalidArgument, "appstore.publish_spool", errors.New("package exceeds limit"))
	}
	return nil
}

func precheckPublishPackage(ctx context.Context, packagePath, extractRoot string) (string, error) {
	reader, err := lpk.OpenFile(ctx, packagePath)
	if err != nil {
		return "", err
	}
	effective, err := reader.EffectiveManifest(ctx)
	if err == nil {
		err = reader.Extract(ctx, extractRoot)
	}
	closeErr := reader.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", storeError(lpkgo.CodeCommandFailed, "appstore.publish_precheck", closeErr)
	}
	warnings, err := lint.Package(ctx, os.DirFS(extractRoot), lint.WithOfficial())
	if err != nil {
		return "", err
	}
	if len(warnings) > 0 {
		return "", &lpkgo.Error{Code: lpkgo.CodeInvalidManifest, Op: "appstore.publish_precheck", Cause: fmt.Errorf("official lint returned %d warning(s)", len(warnings))}
	}
	return strings.TrimSpace(effective.Manifest.Package), nil
}

func cloneChangelogs(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for locale, content := range source {
		clone[locale] = strings.TrimSpace(content)
	}
	return clone
}
