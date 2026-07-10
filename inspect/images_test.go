package inspect_test

import (
	"bytes"
	"context"
	"testing"
	"testing/fstest"

	"github.com/lib-x/lzc-toolkit-go/inspect"
	"github.com/lib-x/lzc-toolkit-go/lpk"
)

func TestInspectImageSummary(t *testing.T) {
	root := imageBearingRoot()
	data := writePackage(t, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: root, Strict: true})

	info, err := inspect.ReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if !info.HasImagesDir || !info.HasImagesLock {
		t.Fatalf("image presence flags = %#v", info)
	}
	if got, want := info.Images.Aliases, []string{"app", "worker"}; !equalStrings(got, want) {
		t.Fatalf("aliases = %#v, want %#v", got, want)
	}
	if len(info.Images.Details) != 2 {
		t.Fatalf("details = %#v", info.Images.Details)
	}

	app := info.Images.Details[0]
	if app.Alias != "app" || app.ImageID != "app-image" || app.Upstream != "registry.example/app:1.0.0" {
		t.Fatalf("app detail identity = %#v", app)
	}
	if app.EmbeddedLayerCount != 2 || app.UpstreamLayerCount != 1 || app.UniqueEmbeddedLayerCount != 2 ||
		app.EmbeddedBytes != 30 || app.MissingEmbeddedLayerCount != 0 {
		t.Fatalf("app detail counts = %#v", app)
	}

	worker := info.Images.Details[1]
	if worker.Alias != "worker" || worker.ImageID != "worker-image" || worker.Upstream != "" {
		t.Fatalf("worker detail identity = %#v", worker)
	}
	if worker.EmbeddedLayerCount != 2 || worker.UpstreamLayerCount != 0 || worker.UniqueEmbeddedLayerCount != 2 ||
		worker.EmbeddedBytes != 10 || worker.MissingEmbeddedLayerCount != 1 {
		t.Fatalf("worker detail counts = %#v", worker)
	}

	if info.Images.TotalEmbeddedLayerCount != 3 || info.Images.TotalEmbeddedBytes != 30 || info.Images.TotalMissingEmbeddedLayerCount != 1 {
		t.Fatalf("total image counts = %#v", info.Images)
	}
}

func imageBearingRoot() fstest.MapFS {
	root := v2Root()
	shared := digest("1")
	appOnly := digest("2")
	upstreamOnly := digest("3")
	missing := digest("4")
	root["images"] = &fstest.MapFile{Mode: 0o755 | fsModeDir}
	root["images/blobs"] = &fstest.MapFile{Mode: 0o755 | fsModeDir}
	root["images/blobs/sha256"] = &fstest.MapFile{Mode: 0o755 | fsModeDir}
	root["images/blobs/sha256/"+shared] = &fstest.MapFile{Data: bytes.Repeat([]byte("s"), 10), Mode: 0o644}
	root["images/blobs/sha256/"+appOnly] = &fstest.MapFile{Data: bytes.Repeat([]byte("a"), 20), Mode: 0o644}
	root["images.lock"] = &fstest.MapFile{Data: []byte(`images:
  worker:
    image_id: worker-image
    upstream: ""
    layers:
      - digest: SHA256:` + shared + `
        source: embed
      - digest: sha256:` + missing + `
        source: embed
  app:
    image_id: app-image
    upstream: registry.example/app:1.0.0
    layers:
      - digest: sha256:` + shared + `
        source: embed
      - digest: sha256:` + appOnly + `
        source: embed
      - digest: sha256:` + upstreamOnly + `
        source: upstream
`), Mode: 0o644}
	return root
}

func digest(prefix string) string {
	return prefix + "000000000000000000000000000000000000000000000000000000000000000"[:64-len(prefix)]
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
