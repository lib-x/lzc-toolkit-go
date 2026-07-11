package official_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lib-x/lzc-toolkit-go/appstore/official"
)

func TestClientApplicationAndDownloadURL(t *testing.T) {
	const packageID = "wx.clawbot.lazycat.app.mediasaber"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/zh/v3/app_" + packageID + ".json":
			_, _ = w.Write([]byte(`{
  "id":5906,
  "package":"wx.clawbot.lazycat.app.mediasaber",
  "kind_ids":"1",
  "category_ids":[24],
  "status":1,
  "created_at":"2026-06-21T08:26:36.197Z",
  "updated_at":"2026-07-10T09:11:04.729Z",
  "version_updated_at":"2026-07-10T09:11:04.728Z",
  "create_user":{"id":1441,"nickname":"xiaoxi"},
  "information":{"language":"zh","name":"微信ClawBot万能视频下载器","support_pc":true,"support_mobile":true},
  "version":{"name":"1.5.3","package":"wx.clawbot.lazycat.app.mediasaber","pkg_hash":"9d3c8de7bd758ef97ecc59cdfc92e1e63f5f097b834389a5c9e47353876acd6a","pkg_path":"/appstore/lpks/pkgs/wx.clawbot.lazycat.app.mediasaber/wx.clawbot.lazycat.app.mediasaber-v1.5.3.lpk","icon_path":"/appstore/metarepo/apps/wx.clawbot.lazycat.app.mediasaber/icon.png","lpk_size":102912,"image_size":462608400},
  "rating":{"score":5,"statistics":{"total":3,"five":3}},
  "is_original":true,
  "count":{"downloads":236,"likes":4,"comments":3,"remind_count":0}
}`))
		case "/zh/apps/" + packageID + "/1.5.3.changelog.json":
			_, _ = w.Write([]byte(`"修复部分视频无法下载问题"`))
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	client := official.New(official.Options{
		MetadataBaseURL: server.URL,
		DownloadBaseURL: "https://dl.lazycatmicroserver.com",
		HTTPClient:      server.Client(),
	})
	application, err := client.Application(context.Background(), packageID)
	if err != nil {
		t.Fatal(err)
	}
	if application.Package != packageID || application.Version.Name != "1.5.3" || application.Information.Name != "微信ClawBot万能视频下载器" {
		t.Fatalf("application=%+v", application)
	}
	if application.CreatedAt.IsZero() || application.Count.Downloads != 236 {
		t.Fatalf("application timestamps/count=%+v", application)
	}

	downloadURL, err := client.ApplicationDownloadURL(application)
	if err != nil {
		t.Fatal(err)
	}
	const wantDownload = "https://dl.lazycatmicroserver.com/appstore/lpks/pkgs/wx.clawbot.lazycat.app.mediasaber/wx.clawbot.lazycat.app.mediasaber-v1.5.3.lpk"
	if downloadURL != wantDownload {
		t.Fatalf("downloadURL=%q", downloadURL)
	}

	changelog, err := client.VersionChangelog(context.Background(), packageID, "1.5.3")
	if err != nil || changelog != "修复部分视频无法下载问题" {
		t.Fatalf("changelog=%q err=%v", changelog, err)
	}
}
