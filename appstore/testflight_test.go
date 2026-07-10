package appstore_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lib-x/lzc-toolkit-go/appstore"
	"github.com/lib-x/lzc-toolkit-go/auth"
)

func TestTestflightUsesBearerAndReferenceMultipartFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer ci-token" || request.Header.Get("X-User-Token") != "ci-token" {
			t.Errorf("headers = %#v", request.Header)
		}
		if _, err := request.Cookie("userToken"); err != nil {
			t.Error(err)
		}
		switch request.URL.Path {
		case "/groups/dict":
			_, _ = response.Write([]byte(`{"data":[{"id":"group-1","name":"CI"}]}`))
		case "/group/group-1/upload":
			if err := request.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
			if request.FormValue("type") != "Lpk" || request.FormValue("changelog") != "release notes" {
				t.Errorf("form = %#v", request.MultipartForm.Value)
			}
			file, _, err := request.FormFile("file")
			if err != nil {
				t.Error(err)
			} else {
				data, _ := io.ReadAll(file)
				_ = file.Close()
				if string(data) != "lpk-data" {
					t.Errorf("file = %q", data)
				}
			}
			_, _ = response.Write([]byte(`{"success":true,"msg":"uploaded","data":{"id":"job-1"}}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, TestflightURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("ci-token")})

	groups, err := client.ListTestGroups(context.Background())
	if err != nil || len(groups) != 1 || groups[0].ID != "group-1" {
		t.Fatalf("groups=%#v err=%v", groups, err)
	}
	result, err := client.PrePublish(context.Background(), appstore.PrePublishRequest{
		GroupID: "group-1", Changelog: "release notes", FileName: "demo.lpk", Package: bytes.NewBufferString("lpk-data"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.Message != "uploaded" {
		t.Fatalf("result = %#v", result)
	}
}
