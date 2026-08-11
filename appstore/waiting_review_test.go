package appstore_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/appstore"
	"github.com/lib-x/lzc-toolkit-go/auth"
)

func TestWaitingReviewVersionUsesPATEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/sdk/v3/developer/app/community.lazycat.app.cloudflare-mesh/review/waiting" {
			t.Fatalf("path=%q", request.URL.Path)
		}
		if request.Header.Get("X-API-Token") != "pat-token" {
			t.Fatalf("X-API-Token=%q", request.Header.Get("X-API-Token"))
		}
		_, _ = io.WriteString(response, `{"errorCode":0,"msg":"ok","data":{"version":{"name":"2026.7.0"}}}`)
	}))
	defer server.Close()
	client, err := appstore.NewPAT(appstore.Options{
		BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("pat-token"),
	})
	if err != nil {
		t.Fatal(err)
	}

	version, found, err := client.WaitingReviewVersion(t.Context(), "community.lazycat.app.cloudflare-mesh")
	if err != nil || !found || version != "2026.7.0" {
		t.Fatalf("version=%q found=%t err=%v", version, found, err)
	}
}

func TestWaitingReviewVersionReturnsMissingForNotFound(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client, err := appstore.NewPAT(appstore.Options{
		BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("pat-token"),
	})
	if err != nil {
		t.Fatal(err)
	}

	version, found, err := client.WaitingReviewVersion(t.Context(), "community.lazycat.app.no-review")
	if err != nil || found || version != "" {
		t.Fatalf("version=%q found=%t err=%v", version, found, err)
	}
}

func TestWaitingReviewVersionRejectsInvalidInputAndResponse(t *testing.T) {
	client, err := appstore.NewPAT(appstore.Options{Token: auth.StaticToken("pat-token")})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.WaitingReviewVersion(t.Context(), "../unsafe"); !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("invalid package err=%v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, `{"errorCode":0,"msg":"ok","data":{"version":{"name":""}}}`)
	}))
	defer server.Close()
	client, err = appstore.NewPAT(appstore.Options{
		BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("pat-token"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.WaitingReviewVersion(t.Context(), "community.lazycat.app.demo"); !errors.Is(err, lpkgo.ErrRemoteUnavailable) {
		t.Fatalf("invalid response err=%v", err)
	}
}
