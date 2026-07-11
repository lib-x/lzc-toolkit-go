package official_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/lib-x/lzc-toolkit-go/appstore/official"
)

func TestClientDiscoveryAndRankings(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.URL.Path)
		switch request.URL.Path {
		case "/op/index":
			_, _ = w.Write([]byte("release-test-1"))
		case "/zh/categories.json":
			_, _ = w.Write([]byte(`[{"id":24,"name":"效率工具","icon":"/appstore/metarepo/uploads/tools.png"}]`))
		case "/zh/app_kinds.json":
			_, _ = w.Write([]byte(`[{"id":2,"code":"official","name":"官方","order_num":2}]`))
		case "/zh/release-test-1/homepage_block.json":
			_, _ = w.Write([]byte(`[{"id":1,"name":"最近更新","block_type":"latest_update","api_path":"/snapshot/homepage_block_recents.json","homepage_show_limit":9,"options":{"show_more":true},"data":null}]`))
		case "/zh/release-test-1/block_recents.json":
			_, _ = w.Write([]byte(`{"id":1,"name":"最近更新","block_type":"latest_update","homepage_show_limit":9,"options":null,"data":[]}`))
		case "/zh/app_download_week.json":
			_, _ = w.Write([]byte(`[{"package":"cloud.lazycat.demo","version":{"package":"cloud.lazycat.demo","name":"1.0.0"}}]`))
		case "/zh/developer_list_week.json":
			_, _ = w.Write([]byte(`[{"developer_id":101,"nickname":"天天","apps":[{"package":"cloud.lazycat.demo","version":{"package":"cloud.lazycat.demo","name":"1.0.0"}}]}]`))
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	client := official.New(official.Options{MetadataBaseURL: server.URL, HTTPClient: server.Client()})
	ctx := context.Background()
	categories, err := client.Categories(ctx)
	if err != nil || len(categories) != 1 || categories[0].ID != 24 {
		t.Fatalf("categories=%+v err=%v", categories, err)
	}
	kinds, err := client.Kinds(ctx)
	if err != nil || len(kinds) != 1 || kinds[0].Code != "official" {
		t.Fatalf("kinds=%+v err=%v", kinds, err)
	}
	homepage, err := client.Homepage(ctx)
	if err != nil || len(homepage) != 1 || homepage[0].Options == nil || !homepage[0].Options.ShowMore || homepage[0].Data != nil {
		t.Fatalf("homepage=%+v err=%v", homepage, err)
	}
	block, err := client.More(ctx, "recents")
	if err != nil || block.BlockType != "latest_update" {
		t.Fatalf("block=%+v err=%v", block, err)
	}
	downloads, err := client.DownloadRanking(ctx, official.RankingWeek)
	if err != nil || len(downloads) != 1 || downloads[0].Package != "cloud.lazycat.demo" {
		t.Fatalf("downloads=%+v err=%v", downloads, err)
	}
	developers, err := client.DeveloperRanking(ctx, official.RankingWeek)
	if err != nil || len(developers) != 1 || len(developers[0].Apps) != 1 {
		t.Fatalf("developers=%+v err=%v", developers, err)
	}

	wantPaths := []string{
		"/zh/categories.json",
		"/zh/app_kinds.json",
		"/op/index", "/zh/release-test-1/homepage_block.json",
		"/op/index", "/zh/release-test-1/block_recents.json",
		"/zh/app_download_week.json",
		"/zh/developer_list_week.json",
	}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("paths=%v", paths)
	}
}
