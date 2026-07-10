package inspect_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	lpkarchive "github.com/lib-x/lpk-go/archive"
	"github.com/lib-x/lpk-go/inspect"
	"github.com/lib-x/lpk-go/lpk"
)

func TestInspectV1Package(t *testing.T) {
	data := writePackage(t, lpk.WriteRequest{Layout: lpk.LayoutV1, Files: v1Root()})

	info, err := inspect.Stream(context.Background(), bytes.NewBuffer(data))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != int64(len(data)) || info.Format != lpkarchive.FormatZIP || info.Layout != lpk.LayoutV1 {
		t.Fatalf("container info = %#v", info)
	}
	if info.PackageID != "cloud.lazycat.apps.demo" || info.AppVersion != "1.0.0" {
		t.Fatalf("package identity = %q %q", info.PackageID, info.AppVersion)
	}
	if !info.HasManifest || info.HasPackageInfo || info.ResourceOnly || info.Signed {
		t.Fatalf("presence flags = %#v", info)
	}
}

func TestInspectV2PackageWithReaderAtAndFile(t *testing.T) {
	data := writePackage(t, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: v2Root(), Strict: true})

	info, err := inspect.ReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Format != lpkarchive.FormatTAR || info.Layout != lpk.LayoutV2 {
		t.Fatalf("container info = %#v", info)
	}
	if info.PackageID != "cloud.lazycat.apps.demo" || info.AppVersion != "2.0.0" {
		t.Fatalf("package identity = %q %q", info.PackageID, info.AppVersion)
	}
	if !info.HasManifest || !info.HasPackageInfo || info.ResourceOnly {
		t.Fatalf("presence flags = %#v", info)
	}

	filename := filepath.Join(t.TempDir(), "package.lpk")
	if _, err := lpk.WriteFile(context.Background(), filename, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: v2Root(), Strict: true}); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := inspect.File(context.Background(), filename)
	if err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Size != stat.Size() || fileInfo.PackageID != "cloud.lazycat.apps.demo" {
		t.Fatalf("file inspection = %#v", fileInfo)
	}
}

func TestInspectReaderDoesNotCloseCallerReader(t *testing.T) {
	data := writePackage(t, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: v2Root(), Strict: true})
	reader, err := lpk.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inspect.Reader(context.Background(), reader); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Entry(context.Background(), "manifest.yml"); err != nil {
		t.Fatalf("caller reader was closed or invalidated: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInspectResourceOnlyPackage(t *testing.T) {
	data := writePackage(t, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: resourceOnlyRoot(), Strict: true})

	info, err := inspect.ReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ResourceOnly || info.HasManifest || !info.HasPackageInfo {
		t.Fatalf("presence flags = %#v", info)
	}
	if info.PackageID != "cloud.lazycat.apps.resources" || info.AppVersion != "1.0.0" {
		t.Fatalf("package identity = %q %q", info.PackageID, info.AppVersion)
	}
}

func TestInspectSignedMarker(t *testing.T) {
	root := v2Root()
	root["META"] = &fstest.MapFile{Mode: 0o755 | fsModeDir}
	root["META/signatures"] = &fstest.MapFile{Mode: 0o755 | fsModeDir}
	root["META/signatures/dev.sig"] = &fstest.MapFile{Data: []byte("signature"), Mode: 0o644}
	data := writePackage(t, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: root, Strict: true})

	info, err := inspect.ReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Signed {
		t.Fatalf("Signed = false; info = %#v", info)
	}
}
