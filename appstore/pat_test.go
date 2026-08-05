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

func TestNewPATUsesSDKDeveloperAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/sdk/v3/developer/app/check/exist" {
			t.Errorf("path=%q", request.URL.Path)
		}
		if request.Header.Get("X-API-Token") != "pat-token" {
			t.Errorf("X-API-Token=%q", request.Header.Get("X-API-Token"))
		}
		if request.Header.Get("X-User-Token") != "" || request.Header.Get("Cookie") != "" {
			t.Error("legacy credentials must not be sent")
		}
		_, _ = io.WriteString(response, `{"errorCode":0,"msg":"ok","data":{"exist":true}}`)
	}))
	defer server.Close()

	client, err := appstore.NewPAT(appstore.Options{
		BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("pat-token"),
	})
	if err != nil {
		t.Fatal(err)
	}
	exists, err := client.CheckApplication(t.Context(), "cloud.lazycat.example")
	if err != nil || !exists {
		t.Fatalf("exists=%t err=%v", exists, err)
	}
}

func TestPATEnvelopeErrorBecomesHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, `{"errorCode":40101,"msg":"PAT rejected","data":null}`)
	}))
	defer server.Close()
	client, err := appstore.NewPAT(appstore.Options{
		BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("pat-token"),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CheckApplication(t.Context(), "cloud.lazycat.example")
	if err == nil || !errors.Is(err, lpkgo.ErrRemoteUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestPATHTTPClientDoesNotFollowRedirects(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL+request.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	client, err := appstore.NewPATHTTPClient(origin.URL, origin.Client())
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, origin.URL+"/api/v3/developer/app/lpk/upload", nil)
	request.Header.Set("X-User-Token", "pat-token")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusTemporaryRedirect || reached {
		t.Fatalf("status=%d reached=%t", response.StatusCode, reached)
	}
}

func TestPATHTTPClientDoesNotForwardCredentialsToAnotherOrigin(t *testing.T) {
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		forwarded = request.Header.Get("X-API-Token") != "" || request.Header.Get("X-User-Token") != "" || request.Header.Get("Cookie") != ""
		response.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.NotFoundHandler())
	defer origin.Close()
	client, err := appstore.NewPATHTTPClient(origin.URL, target.Client())
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodGet, target.URL+"/api/v3/developer/app/list", nil)
	request.Header.Set("X-User-Token", "pat-token")
	request.Header.Set("Cookie", "userToken=pat-token")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if forwarded {
		t.Fatal("PAT credentials were forwarded to another origin")
	}
}

func TestResolvePATBaseURL(t *testing.T) {
	if got, err := appstore.ResolvePATBaseURL(""); err != nil || got != appstore.DefaultPATBaseURL {
		t.Fatalf("default=%q err=%v", got, err)
	}
	if got, err := appstore.ResolvePATBaseURL("api.example.invalid"); err != nil || got != "https://api.example.invalid" {
		t.Fatalf("custom=%q err=%v", got, err)
	}
	for _, value := range []string{"https://api.example.com", "api.example.com:443", "user@api.example.com", "api.example.com/sdk", "api.example.com?x=1", "api example.com"} {
		if _, err := appstore.ResolvePATBaseURL(value); err == nil {
			t.Fatalf("unsafe host %q accepted", value)
		}
	}
}
