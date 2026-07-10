package appstore_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lib-x/lzc-toolkit-go/appstore"
	"github.com/lib-x/lzc-toolkit-go/auth"
)

func TestCopyImageUsesServerSideReferenceProtocol(t *testing.T) {
	progressCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-User-Token") != "ci-token" {
			t.Errorf("X-User-Token = %q", request.Header.Get("X-User-Token"))
		}
		cookie, err := request.Cookie("userToken")
		if err != nil || cookie.Value != "ci-token" {
			t.Errorf("cookie=%v err=%v", cookie, err)
		}
		if request.URL.Query().Get("image") != "docker.io/library/demo:1" || request.URL.Query().Get("platform") != "arm64" {
			t.Errorf("query = %v", request.URL.Query())
		}
		switch request.URL.Path {
		case "/api/v3/developer/app/docker/image/push/v3/copy":
			_, _ = response.Write([]byte(`{"started":true}`))
		case "/api/v3/developer/app/docker/image/push/v3/progress":
			progressCalls++
			if progressCalls == 1 {
				_, _ = response.Write([]byte(`{"finished":false,"layers":[{"hash":"abc","progress":50}]}`))
			} else {
				_, _ = response.Write([]byte(`{"finished":true,"lzc_image":"registry.lazycat.cloud/demo:1"}`))
			}
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("ci-token"), PollInterval: time.Millisecond})
	var updates []appstore.CopyProgress

	result, err := client.CopyImage(context.Background(), appstore.CopyImageRequest{
		Image: "docker.io/library/demo:1", Platform: "arm64", Timeout: time.Second,
		OnProgress: func(progress appstore.CopyProgress) { updates = append(updates, progress) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceImage != "docker.io/library/demo:1" || result.Platform != "arm64" || result.LazyCatImage != "registry.lazycat.cloud/demo:1" {
		t.Fatalf("result = %#v", result)
	}
	if !result.Progress.Finished || len(result.Progress.Layers) != 1 || result.Progress.Layers[0].Progress != 100 || len(updates) != 2 || progressCalls != 2 {
		t.Fatalf("result=%#v updates=%#v calls=%d", result, updates, progressCalls)
	}
}

func TestListImagesSortsNewestFirstAndRetainsErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = response.Write([]byte(`[
{"source_image":"old","lzc_image":"lazy/old","UpdatedAt":"2026-01-01T00:00:00Z"},
{"source_image":"new","lzc_image":"lazy/new","UpdatedAt":"2026-02-01T00:00:00Z","errmsg":"copy warning"}
]`))
	}))
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("ci-token")})

	images, err := client.ListImages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 2 || images[0].SourceImage != "new" || images[0].ErrorMessage != "copy warning" {
		t.Fatalf("images = %#v", images)
	}
}
