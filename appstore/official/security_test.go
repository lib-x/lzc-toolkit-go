package official_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/appstore/official"
)

func TestClientRejectsUnsafeInputs(t *testing.T) {
	client := official.New(official.Options{})
	if _, err := client.Application(context.Background(), "../secret"); !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("application err=%v", err)
	}
	if _, err := client.More(context.Background(), "ratings/../../secret"); !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("more err=%v", err)
	}
	if _, err := client.DownloadRanking(context.Background(), official.RankingPeriod("daily")); !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("ranking err=%v", err)
	}
	if _, err := client.DownloadURL("https://evil.example/app.lpk"); !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("download URL err=%v", err)
	}
	if _, err := client.AssetURL("/appstore/metarepo/../secret"); !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("asset URL err=%v", err)
	}
}

func TestClientMapsNotFoundAndNilReceiver(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client := official.New(official.Options{MetadataBaseURL: server.URL, HTTPClient: server.Client()})
	if _, err := client.Application(context.Background(), "cloud.lazycat.missing"); !errors.Is(err, lpkgo.ErrNotFound) {
		t.Fatalf("not found err=%v", err)
	}
	var nilClient *official.Client
	if _, err := nilClient.Categories(context.Background()); !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("nil client err=%v", err)
	}
}
