package appstore_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/appstore"
)

func TestTriggerAPKStreamsTypedMultipartWithoutAuthentication(t *testing.T) {
	icon := &publishTrackingReader{Reader: bytes.NewReader([]byte("png-data"))}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/trigger_latest_for_app" {
			http.NotFound(response, request)
			return
		}
		if request.Header.Get("X-User-Token") != "" || request.Header.Get("Authorization") != "" {
			t.Errorf("unexpected authentication headers")
		}
		if err := request.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if request.FormValue("app_id") != "cloud.lazycat.apps.demo" {
			t.Errorf("app_id = %q", request.FormValue("app_id"))
		}
		var names map[string]string
		if err := json.Unmarshal([]byte(request.FormValue("app_name_map")), &names); err != nil {
			t.Fatal(err)
		}
		if names["zh"] != "演示" || names["en"] != "Demo" {
			t.Errorf("names = %#v", names)
		}
		file, header, err := request.FormFile("app_icon")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if header.Filename != "demo.png" {
			t.Errorf("filename = %q", header.Filename)
		}
		response.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client()})

	result, err := client.TriggerAPK(context.Background(), appstore.APKRequest{
		AppID: " cloud.lazycat.apps.demo ", Names: map[string]string{"zh": " 演示 ", "en": "Demo"}, Icon: icon, IconName: "assets/demo.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted || result.NotModified || result.StatusCode != http.StatusCreated || result.AppID != "cloud.lazycat.apps.demo" {
		t.Fatalf("result = %#v", result)
	}
	if icon.closed {
		t.Fatal("TriggerAPK closed caller-owned icon reader")
	}
}

func TestTriggerAPKAcceptsNotModified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client()})

	result, err := client.TriggerAPK(context.Background(), appstore.APKRequest{AppID: "cloud.lazycat.apps.demo"})
	if err != nil || !result.Accepted || !result.NotModified {
		t.Fatalf("result=%#v err=%#v", result, err)
	}
}

func TestTriggerAPKValidatesInputAndRemoteFailure(t *testing.T) {
	client := appstore.New(appstore.Options{})
	if _, err := client.TriggerAPK(context.Background(), appstore.APKRequest{}); !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("empty app ID error = %#v", err)
	}
	if _, err := client.TriggerAPK(context.Background(), appstore.APKRequest{AppID: "demo", Names: map[string]string{"en": " "}}); !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("empty name error = %#v", err)
	}
	if _, err := client.TriggerAPK(context.Background(), appstore.APKRequest{AppID: "demo", Icon: bytes.NewReader(nil), IconName: "icon.png\r\nInjected: value"}); !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("invalid icon filename error = %#v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Error(response, "sensitive upstream response", http.StatusInternalServerError)
	}))
	defer server.Close()
	client = appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client()})
	result, err := client.TriggerAPK(context.Background(), appstore.APKRequest{AppID: "demo"})
	var storeErr *lpkgo.Error
	if !errors.As(err, &storeErr) || storeErr.StatusCode != http.StatusInternalServerError || result.Accepted {
		t.Fatalf("result=%#v err=%#v", result, err)
	}
}

func TestTriggerAPKHonorsRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		time.Sleep(100 * time.Millisecond)
		response.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client()})

	_, err := client.TriggerAPK(context.Background(), appstore.APKRequest{AppID: "demo", Timeout: 10 * time.Millisecond})
	if !errors.Is(err, lpkgo.ErrCancelled) {
		t.Fatalf("err = %#v", err)
	}
}
