package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lib-x/lzc-toolkit-go/auth"
)

func TestClientLoginUsesReferenceProtocolAndStoresToken(t *testing.T) {
	store := auth.NewMemoryStore("")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/login/signin" {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", got)
		}
		if err := request.ParseForm(); err != nil {
			t.Error(err)
		}
		if request.Form.Get("username") != "developer" || request.Form.Get("password") != "password" {
			t.Errorf("unexpected form fields")
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"success":true,"data":{"token":"session-token"}}`))
	}))
	defer server.Close()
	client := auth.NewClient(auth.ClientOptions{BaseURL: server.URL, HTTPClient: server.Client(), Store: store})

	session, err := client.Login(context.Background(), auth.Credentials{Username: "developer", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	if session.Token != "session-token" {
		t.Fatalf("session = %#v", session)
	}
	stored, err := store.Load(context.Background())
	if err != nil || stored != "session-token" {
		t.Fatalf("stored=%q err=%v", stored, err)
	}
}

func TestClientValidateUsesUserTokenHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/user/current" || request.Header.Get("X-User-Token") != "session-token" {
			t.Errorf("unexpected validation request")
		}
		_, _ = response.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()
	client := auth.NewClient(auth.ClientOptions{BaseURL: server.URL, HTTPClient: server.Client()})

	if err := client.Validate(context.Background(), "session-token"); err != nil {
		t.Fatal(err)
	}
}
