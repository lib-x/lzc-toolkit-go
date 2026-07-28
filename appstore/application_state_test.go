package appstore_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lib-x/lzc-toolkit-go/appstore"
	"github.com/lib-x/lzc-toolkit-go/auth"
)

func TestApplicationStateUsesExactDeveloperApplicationMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v3/developer/app/list" || request.URL.Query().Get("seek") != "cloud.lazycat.apps.publish-demo" {
			t.Fatalf("request=%s?%s", request.URL.Path, request.URL.RawQuery)
		}
		_, _ = response.Write([]byte(`{"items":[
			{"id":4,"package":"cloud.lazycat.apps.publish-demo-extra","resource":{"info_data":{}}},
			{"id":7,"package":"cloud.lazycat.apps.publish-demo","waiting_review_id":null,"resource":{"info_data":{}}}
		]}`))
	}))
	defer server.Close()
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("ci-token")})

	state, err := client.ApplicationState(context.Background(), "cloud.lazycat.apps.publish-demo")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Exists || state.ID != 7 || state.InformationReady || state.ReviewPending {
		t.Fatalf("state=%#v", state)
	}
}

func TestApplicationStateRecognizesReadyInformationAndPendingReview(t *testing.T) {
	tests := []struct {
		name        string
		application string
		ready       bool
		pending     bool
	}{
		{
			name: "ready desktop information",
			application: `{"id":7,"package":"cloud.lazycat.apps.publish-demo","resource":{"info_data":{"zh":{
				"language":"zh","name":"Publish Demo","brief":"Workspace","support_pc":true,
				"screenshot_pc_paths":["/one.png","/two.png"]
			}}}}`,
			ready: true,
		},
		{
			name:        "review pending",
			application: `{"id":7,"package":"cloud.lazycat.apps.publish-demo","waiting_review_id":19,"resource":{"info_data":{}}}`,
			pending:     true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				_, _ = response.Write([]byte(`{"items":[` + test.application + `]}`))
			}))
			defer server.Close()
			client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("ci-token")})
			state, err := client.ApplicationState(context.Background(), "cloud.lazycat.apps.publish-demo")
			if err != nil {
				t.Fatal(err)
			}
			if state.InformationReady != test.ready || state.ReviewPending != test.pending {
				t.Fatalf("state=%#v", state)
			}
		})
	}
}
