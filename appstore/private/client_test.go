package private_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/appstore/private"
)

func TestClientLatestVersionUsesPrivateGroupHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/packages/community.lazycat.group-app/latest-version" {
			t.Fatalf("path=%q", request.URL.Path)
		}
		if got := request.Header.Get("X-Group-Codes"); got != "ABC123,LATE23" {
			t.Fatalf("X-Group-Codes=%q", got)
		}
		if got := request.URL.Query().Get("groupCodes"); got != "" {
			t.Fatalf("groupCodes query=%q", got)
		}
		if request.Header.Get("Authorization") != "" || request.Header.Get("X-User-Token") != "" {
			t.Fatal("private store request unexpectedly contains account credentials")
		}
		if _, err := request.Cookie("userToken"); !errors.Is(err, http.ErrNoCookie) {
			t.Fatalf("userToken cookie unexpectedly present: %v", err)
		}
		_, _ = w.Write([]byte(`{
  "packageId":"community.lazycat.group-app",
  "latestVersion":{
    "id":7,"appId":3,"uploaderId":2,"version":"3.0.0",
    "changelog":"Private release","status":"APPROVED","sourceType":"LOCAL",
    "downloadUrl":"https://store.example/download/app.lpk","fileSize":1024,
    "sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "publishedAt":"2026-07-11T00:00:00Z","createdAt":"2026-07-10T00:00:00Z"
  }
}`))
	}))
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	serverURL, _ := url.Parse(server.URL)
	jar.SetCookies(serverURL, []*http.Cookie{{Name: "userToken", Value: "must-not-send"}})
	httpClient := *server.Client()
	httpClient.Jar = jar
	client, err := private.New(private.Options{
		BaseURL:    server.URL,
		HTTPClient: &httpClient,
		GroupCodes: []string{" abc123 ", "invalid", "LATE23", "ABC123"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.LatestVersion(context.Background(), private.LatestVersionRequest{
		PackageID:  "community.lazycat.group-app",
		GroupCodes: []string{"late23"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PackageID != "community.lazycat.group-app" || result.LatestVersion.Version != "3.0.0" || result.LatestVersion.FileSize != 1024 {
		t.Fatalf("result=%+v", result)
	}
}

func TestClientLatestVersionNotFoundIsIndistinguishable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"APP_NOT_FOUND","message":"App not found"}}`))
	}))
	defer server.Close()
	client, err := private.New(private.Options{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.LatestVersion(context.Background(), private.LatestVersionRequest{PackageID: "community.lazycat.missing"})
	if !errors.Is(err, lpkgo.ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestClientGroupCodePlacements(t *testing.T) {
	tests := []struct {
		name       string
		placement  private.GroupCodePlacement
		wantHeader string
		wantQuery  string
	}{
		{name: "header", placement: private.GroupCodesHeader, wantHeader: "ABC123"},
		{name: "query", placement: private.GroupCodesQuery, wantQuery: "ABC123"},
		{name: "both", placement: private.GroupCodesHeaderAndQuery, wantHeader: "ABC123", wantQuery: "ABC123"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if got := request.Header.Get("X-Group-Codes"); got != test.wantHeader {
					t.Fatalf("header=%q", got)
				}
				if got := request.URL.Query().Get("groupCodes"); got != test.wantQuery {
					t.Fatalf("query=%q", got)
				}
				_, _ = w.Write([]byte(`{"packageId":"community.lazycat.app","latestVersion":{"version":"1.0.0","createdAt":"2026-07-10T00:00:00Z"}}`))
			}))
			defer server.Close()
			client, err := private.New(private.Options{
				BaseURL:            server.URL,
				HTTPClient:         server.Client(),
				GroupCodes:         []string{"abc123"},
				GroupCodePlacement: test.placement,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.LatestVersion(context.Background(), private.LatestVersionRequest{PackageID: "community.lazycat.app"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestClientRejectsInvalidConfigurationAndPackage(t *testing.T) {
	if _, err := private.New(private.Options{BaseURL: "not-a-url"}); !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("constructor err=%v", err)
	}
	client, err := private.New(private.Options{BaseURL: "https://store.example"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.LatestVersion(context.Background(), private.LatestVersionRequest{PackageID: "../secret"}); !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("package err=%v", err)
	}
}

func TestClientDoesNotForwardGroupCodesAcrossRedirects(t *testing.T) {
	var redirected bool
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		redirected = true
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, target.URL, http.StatusFound)
	}))
	defer source.Close()
	client, err := private.New(private.Options{BaseURL: source.URL, HTTPClient: source.Client(), GroupCodes: []string{"ABC123"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.LatestVersion(context.Background(), private.LatestVersionRequest{PackageID: "community.lazycat.app"})
	if !errors.Is(err, lpkgo.ErrRemoteUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if redirected {
		t.Fatal("redirect target was contacted")
	}
}
