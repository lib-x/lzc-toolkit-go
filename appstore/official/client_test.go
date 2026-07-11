package official_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lib-x/lzc-toolkit-go/appstore/official"
)

func TestClientCurrentReleaseIsAnonymous(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/op/index" {
			t.Fatalf("path=%q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "" || request.Header.Get("X-User-Token") != "" {
			t.Fatal("official request unexpectedly contains authentication headers")
		}
		if _, err := request.Cookie("userToken"); !errors.Is(err, http.ErrNoCookie) {
			t.Fatalf("userToken cookie unexpectedly present: %v", err)
		}
		_, _ = w.Write([]byte("release-test-1\n"))
	}))
	defer server.Close()

	client := official.New(official.Options{MetadataBaseURL: server.URL, HTTPClient: server.Client()})
	release, err := client.CurrentRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if release != "release-test-1" {
		t.Fatalf("release=%q", release)
	}
}
