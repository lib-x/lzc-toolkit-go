package appstore_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/appstore"
	"github.com/lib-x/lzc-toolkit-go/auth"
	"github.com/lib-x/lzc-toolkit-go/lpk"
)

type publishTrackingReader struct {
	io.Reader
	closed bool
}

func (reader *publishTrackingReader) Close() error {
	reader.closed = true
	return nil
}

func TestPublishRunsOfficialPrecheckCreateUploadAndReview(t *testing.T) {
	packageData := publishLPK(t)
	packageReader := &publishTrackingReader{Reader: bytes.NewReader(packageData)}
	var created, uploaded, reviewed bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-User-Token") != "ci-token" {
			t.Errorf("missing auth header")
		}
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			if request.URL.Query().Get("package") != "cloud.lazycat.apps.publish-demo" {
				t.Errorf("package query = %q", request.URL.Query().Get("package"))
			}
			_, _ = response.Write([]byte(`{"exist":false}`))
		case "/api/v3/developer/app/create":
			created = true
			var input appstore.CreateApplicationRequest
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			if input.Package != "cloud.lazycat.apps.publish-demo" || input.Language != "en" {
				t.Errorf("create input = %#v", input)
			}
			_, _ = response.Write([]byte(`{"success":true}`))
		case "/api/v3/developer/app/lpk/upload":
			uploaded = true
			if err := request.ParseMultipartForm(2 << 20); err != nil {
				t.Error(err)
			}
			file, _, err := request.FormFile("file")
			if err != nil {
				t.Error(err)
			} else if err := file.Close(); err != nil {
				t.Error(err)
			}
			_, _ = response.Write([]byte(`{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"abc","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`))
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			reviewed = true
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			_, _ = response.Write([]byte(`{"success":true,"review":"queued"}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("ci-token")})

	result, err := client.Publish(context.Background(), appstore.PublishRequest{
		Package: packageReader, Changelogs: map[string]string{"en": "release notes"}, CreateIfMissing: true,
		Application: &appstore.CreateApplicationRequest{Language: "en", Name: "Publish Demo"},
	})
	if err != nil {
		t.Fatalf("Publish() error = %#v", err)
	}
	if !created || !uploaded || !reviewed || !result.Created || result.Upload.Package != "cloud.lazycat.apps.publish-demo" {
		t.Fatalf("created=%v uploaded=%v reviewed=%v result=%#v", created, uploaded, reviewed, result)
	}
	if packageReader.closed {
		t.Fatal("Publish closed caller-owned package reader")
	}
}

func TestPublishIncludesApplicationInfoInReview(t *testing.T) {
	packageData := publishLPK(t)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			if err := request.ParseMultipartForm(2 << 20); err != nil {
				t.Error(err)
			}
			_, _ = response.Write([]byte(validUploadResponse()))
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			var body struct {
				Infos []appstore.ApplicationInfo `json:"infos"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body.Infos) != 1 {
				t.Fatalf("infos=%#v", body.Infos)
			}
			info := body.Infos[0]
			if info.ID != 0 || info.Language != "zh" || info.Name != "Publish Demo" || info.Brief != "A collaborative workspace" || !info.SupportPC || info.SupportMobile {
				t.Fatalf("info=%#v", info)
			}
			if strings.Join(info.ScreenshotPCPaths, ",") != "/screens/one.png,/screens/two.png" {
				t.Fatalf("screenshots=%v", info.ScreenshotPCPaths)
			}
			_, _ = response.Write([]byte(`{"success":true,"review":"queued"}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("ci-token")})

	_, err := client.Publish(context.Background(), appstore.PublishRequest{
		Package: bytes.NewReader(packageData), Changelogs: map[string]string{"zh": "release notes"},
		ApplicationInfos: []appstore.ApplicationInfo{{
			Language: "zh", Name: "Publish Demo", Brief: "A collaborative workspace", SupportPC: true,
			ScreenshotPCPaths: []string{"/screens/one.png", "/screens/two.png"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPublishRejectsInvalidApplicationInformationBeforeReadingPackage(t *testing.T) {
	tests := []struct {
		name string
		info appstore.ApplicationInfo
	}{
		{name: "unsupported desktop screenshots", info: appstore.ApplicationInfo{Language: "zh", Name: "Demo", Brief: "Brief", SupportMobile: true, ScreenshotPCPaths: []string{"/one.png"}, ScreenshotMobilePaths: []string{"/one.png", "/two.png", "/three.png"}}},
		{name: "empty screenshot path", info: appstore.ApplicationInfo{Language: "zh", Name: "Demo", Brief: "Brief", SupportPC: true, ScreenshotPCPaths: []string{"/one.png", " "}}},
		{name: "control in screenshot path", info: appstore.ApplicationInfo{Language: "zh", Name: "Demo", Brief: "Brief", SupportPC: true, ScreenshotPCPaths: []string{"/one.png", "/two.png\r\n"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &publishTrackingReader{Reader: strings.NewReader("not-an-lpk")}
			client := appstore.New(appstore.Options{Token: auth.StaticToken("ci-token")})
			_, err := client.Publish(context.Background(), appstore.PublishRequest{
				Package: reader, Changelogs: map[string]string{"zh": "notes"}, ApplicationInfos: []appstore.ApplicationInfo{test.info},
			})
			if !errors.Is(err, lpkgo.ErrInvalidArgument) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestUploadApplicationImageUsesAuthenticatedMultipartAndWrappedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v3/developer/upload" || request.Method != http.MethodPost {
			t.Fatalf("request=%s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("X-User-Token") != "ci-token" {
			t.Fatal("missing auth header")
		}
		if err := request.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		file, header, err := request.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatal(err)
		}
		if header.Filename != "screen.png" || !bytes.Equal(data, []byte("png-data")) {
			t.Fatalf("filename=%q data=%q", header.Filename, data)
		}
		_, _ = response.Write([]byte(`{"success":true,"data":{"url":"/appstore/screens/screen.png"}}`))
	}))
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("ci-token")})

	path, err := client.UploadApplicationImage(context.Background(), bytes.NewReader([]byte("png-data")), "screen.png")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/appstore/screens/screen.png" {
		t.Fatalf("path=%q", path)
	}
}

func TestUploadApplicationImageRejectsUnsafeFilenameBeforeRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("unsafe filename must not reach the server")
	}))
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("ci-token")})

	for _, name := range []string{"../screen.png", "screen\r\nX-Test.png", "截图.png", "screen.gif"} {
		if _, err := client.UploadApplicationImage(context.Background(), bytes.NewReader([]byte("png-data")), name); !errors.Is(err, lpkgo.ErrInvalidArgument) {
			t.Fatalf("name=%q err=%v", name, err)
		}
	}
}

func TestPublishAcceptsTemplatedLPK(t *testing.T) {
	server := publishServer(t, true, validUploadResponse(), http.StatusOK)
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("ci-token")})

	result, err := client.Publish(context.Background(), appstore.PublishRequest{
		Package: bytes.NewReader(publishTemplatedLPK(t)), Changelogs: map[string]string{"en": "release notes"},
	})
	if err != nil {
		t.Fatalf("Publish() error = %#v", err)
	}
	if result.Upload.Package != "cloud.lazycat.apps.publish-demo" || result.Upload.Version != "1.0.0" {
		t.Fatalf("result = %#v", result)
	}
}

func TestPublishRejectsOfficialLintWarningsBeforeNetwork(t *testing.T) {
	packageData := publishLPKWithImage(t, "docker.io/library/demo:1.0.0")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		http.Error(response, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("ci-token")})

	_, err := client.Publish(context.Background(), appstore.PublishRequest{
		Package: bytes.NewReader(packageData), Changelogs: map[string]string{"en": "release notes"},
	})
	if !errors.Is(err, lpkgo.ErrInvalidManifest) || requests != 0 {
		t.Fatalf("err=%#v requests=%d", err, requests)
	}
}

func TestPublishRequiresApplicationDetailsWhenPackageIsMissing(t *testing.T) {
	server := publishServer(t, false, validUploadResponse(), http.StatusOK)
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("ci-token")})

	_, err := client.Publish(context.Background(), appstore.PublishRequest{
		Package: bytes.NewReader(publishLPK(t)), Changelogs: map[string]string{"en": "release notes"},
	})
	if !errors.Is(err, lpkgo.ErrNotFound) {
		t.Fatalf("err = %#v", err)
	}
}

func TestPublishRejectsApplicationPackageMismatch(t *testing.T) {
	server := publishServer(t, false, validUploadResponse(), http.StatusOK)
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("ci-token")})

	_, err := client.Publish(context.Background(), appstore.PublishRequest{
		Package: bytes.NewReader(publishLPK(t)), Changelogs: map[string]string{"en": "release notes"}, CreateIfMissing: true,
		Application: &appstore.CreateApplicationRequest{Package: "cloud.lazycat.apps.other", Language: "en", Name: "Other"},
	})
	if !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("err = %#v", err)
	}
}

func TestPublishRejectsIncompleteUploadResponse(t *testing.T) {
	server := publishServer(t, true, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0"}`, http.StatusOK)
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("ci-token")})

	_, err := client.Publish(context.Background(), appstore.PublishRequest{
		Package: bytes.NewReader(publishLPK(t)), Changelogs: map[string]string{"en": "release notes"},
	})
	if !errors.Is(err, lpkgo.ErrRemoteUnavailable) {
		t.Fatalf("err = %#v", err)
	}
}

func TestPublishRejectsMismatchedUploadPackageBeforeReview(t *testing.T) {
	reviewed := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			_, _ = response.Write([]byte(`{"package":"cloud.lazycat.apps.other","version":"1.0.0","url":"/demo.lpk","sha256":"abc"}`))
		default:
			reviewed = true
			_, _ = response.Write([]byte(`{"success":true}`))
		}
	}))
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("ci-token")})

	_, err := client.Publish(context.Background(), appstore.PublishRequest{
		Package: bytes.NewReader(publishLPK(t)), Changelogs: map[string]string{"en": "release notes"},
	})
	if !errors.Is(err, lpkgo.ErrRemoteUnavailable) || reviewed {
		t.Fatalf("err=%#v reviewed=%v", err, reviewed)
	}
}

func TestPublishReturnsReviewFailure(t *testing.T) {
	server := publishServer(t, true, validUploadResponse(), http.StatusInternalServerError)
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("ci-token")})

	_, err := client.Publish(context.Background(), appstore.PublishRequest{
		Package: bytes.NewReader(publishLPK(t)), Changelogs: map[string]string{"en": "release notes"},
	})
	var storeErr *lpkgo.Error
	if !errors.As(err, &storeErr) || storeErr.Code != lpkgo.CodeRemoteUnavailable || storeErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("err = %#v", err)
	}
}

func publishServer(t *testing.T, exists bool, uploadResponse string, reviewStatus int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":` + map[bool]string{true: "true", false: "false"}[exists] + `}`))
		case "/api/v3/developer/app/lpk/upload":
			if err := request.ParseMultipartForm(2 << 20); err != nil {
				t.Error(err)
			}
			_, _ = response.Write([]byte(uploadResponse))
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			response.WriteHeader(reviewStatus)
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
}

func validUploadResponse() string {
	return `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"abc","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`
}

func publishLPK(t *testing.T) []byte {
	return publishLPKWithImage(t, "registry.lazycat.cloud/demo/app:1.0.0")
}

func publishTemplatedLPK(t *testing.T) []byte {
	t.Helper()
	root := fstest.MapFS{
		"package.yml": {Data: []byte(`package: cloud.lazycat.apps.publish-demo
version: 1.0.0
name: Publish Demo
min_os_version: 1.3.0
locales:
  en:
    name: Publish Demo
`), Mode: 0o644},
		"manifest.yml": {Data: []byte(`application:
  subdomain: publish-demo
  image: registry.lazycat.cloud/demo/app:1.0.0
{{- if .U.multi_instance }}
  multi_instance: true
{{- end }}
`), Mode: 0o644},
		"icon.png": {Data: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, Mode: 0o644},
	}
	var output bytes.Buffer
	if _, err := lpk.Write(context.Background(), &output, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: root, Strict: true, AllowManifestTemplate: true}); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func publishLPKWithImage(t *testing.T, image string) []byte {
	t.Helper()
	root := fstest.MapFS{
		"package.yml": {Data: []byte(`package: cloud.lazycat.apps.publish-demo
version: 1.0.0
name: Publish Demo
min_os_version: 1.3.0
locales:
  en:
    name: Publish Demo
`), Mode: 0o644},
		"manifest.yml": {Data: []byte("application:\n" +
			"  subdomain: publish-demo\n" +
			"  image: " + image + "\n"), Mode: 0o644},
		"icon.png": {Data: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, Mode: 0o644},
	}
	var output bytes.Buffer
	if _, err := lpk.Write(context.Background(), &output, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: root, Strict: true}); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
