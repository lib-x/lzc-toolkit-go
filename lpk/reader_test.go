package lpk_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	lpkarchive "github.com/lib-x/lzc-toolkit-go/archive"
	"github.com/lib-x/lzc-toolkit-go/lpk"
)

func TestOpenV1ReaderFromStreamOwnsTemporaryFileOnly(t *testing.T) {
	data := writePackage(t, lpk.WriteRequest{Layout: lpk.LayoutV1, Files: v1Root()})
	input := &trackingReader{Reader: bytes.NewBuffer(data)}
	tempDir := t.TempDir()

	reader, err := lpk.Open(context.Background(), input, lpk.WithTempDir(tempDir), lpk.WithFilenameHint("demo.lpk"))
	if err != nil {
		t.Fatal(err)
	}
	if got := reader.Layout(); got != lpk.LayoutV1 {
		t.Fatalf("layout = %q", got)
	}
	if got := reader.Format(); got != lpkarchive.FormatZIP {
		t.Fatalf("format = %q", got)
	}
	if got := reader.Size(); got != int64(len(data)) {
		t.Fatalf("size = %d, want %d", got, len(data))
	}

	entry, err := reader.Entry(context.Background(), "payload.txt")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Name != "payload.txt" || entry.Type != lpkarchive.EntryRegular {
		t.Fatalf("entry = %#v", entry)
	}
	contents := mustReadEntry(t, reader, "payload.txt")
	if string(contents) != "hello v1\n" {
		t.Fatalf("payload = %q", contents)
	}

	document, err := reader.Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertLookup(t, document, "cloud.lazycat.apps.demo", "package")
	packageInfo, err := reader.PackageInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertLookup(t, packageInfo, "1.0.0", "version")

	effective, err := reader.EffectiveManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if effective.HasPackageFile || effective.PackageInfo != nil {
		t.Fatalf("legacy effective package file state = %#v", effective)
	}
	if effective.Manifest.Package != "cloud.lazycat.apps.demo" || effective.Manifest.Application.Subdomain != "demo" {
		t.Fatalf("effective manifest = %+v", effective.Manifest)
	}

	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if input.closed {
		t.Fatal("Open closed caller-provided reader")
	}
	if entries, err := os.ReadDir(tempDir); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("temporary files remain after Close: %#v", entries)
	}
}

func TestReaderOpenVariantsDetectLayout(t *testing.T) {
	tests := []struct {
		name   string
		req    lpk.WriteRequest
		layout lpk.Layout
		format lpkarchive.Format
	}{
		{name: "v1", req: lpk.WriteRequest{Layout: lpk.LayoutV1, Files: v1Root()}, layout: lpk.LayoutV1, format: lpkarchive.FormatZIP},
		{name: "v2", req: lpk.WriteRequest{Layout: lpk.LayoutV2, Files: v2Root(), Strict: true}, layout: lpk.LayoutV2, format: lpkarchive.FormatTAR},
		{name: "resource-only", req: lpk.WriteRequest{Layout: lpk.LayoutV2, Files: resourceOnlyRoot(), Strict: true}, layout: lpk.LayoutV2, format: lpkarchive.FormatTAR},
	}
	for _, test := range tests {
		t.Run(test.name+"/Open", func(t *testing.T) {
			data := writePackage(t, test.req)
			reader, err := lpk.Open(context.Background(), bytes.NewBuffer(data))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reader.Close() })
			assertReaderLayout(t, reader, test.layout, test.format, int64(len(data)))
		})

		t.Run(test.name+"/OpenReaderAt", func(t *testing.T) {
			data := writePackage(t, test.req)
			reader, err := lpk.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reader.Close() })
			assertReaderLayout(t, reader, test.layout, test.format, int64(len(data)))
		})

		t.Run(test.name+"/OpenFile", func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "package.lpk")
			if _, err := lpk.WriteFile(context.Background(), destination, test.req); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(destination)
			if err != nil {
				t.Fatal(err)
			}
			reader, err := lpk.OpenFile(context.Background(), destination)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reader.Close() })
			assertReaderLayout(t, reader, test.layout, test.format, info.Size())
		})
	}
}

func TestOpenReaderAtV2ReadsManifestPackageAndExtracts(t *testing.T) {
	data := writePackage(t, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: v2Root(), Strict: true})
	input := &trackingReaderAt{ReaderAt: bytes.NewReader(data)}

	reader, err := lpk.OpenReaderAt(context.Background(), input, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	if got := reader.Layout(); got != lpk.LayoutV2 {
		t.Fatalf("layout = %q", got)
	}
	if got := reader.Format(); got != lpkarchive.FormatTAR {
		t.Fatalf("format = %q", got)
	}
	if input.closed {
		t.Fatal("OpenReaderAt closed caller-provided reader")
	}

	manifestDocument, err := reader.Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertLookup(t, manifestDocument, "demo", "application", "subdomain")
	packageInfo, err := reader.PackageInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertLookup(t, packageInfo, "cloud.lazycat.apps.demo", "package")

	effective, err := reader.EffectiveManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !effective.HasPackageFile || effective.PackageInfo == nil {
		t.Fatalf("effective package file state = %#v", effective)
	}
	if effective.Manifest.Package != "cloud.lazycat.apps.demo" || effective.Manifest.Application.Image != "registry.example/demo:2.0.0" {
		t.Fatalf("effective manifest = %+v", effective.Manifest)
	}

	destination := t.TempDir()
	if err := reader.Extract(context.Background(), destination); err != nil {
		t.Fatal(err)
	}
	if contents, err := os.ReadFile(filepath.Join(destination, "payload.txt")); err != nil {
		t.Fatal(err)
	} else if string(contents) != "hello v2\n" {
		t.Fatalf("extracted payload = %q", contents)
	}
}

func TestOpenFileReadsLPK(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "demo.lpk")
	if _, err := lpk.WriteFile(context.Background(), destination, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: v2Root(), Strict: true}); err != nil {
		t.Fatal(err)
	}

	reader, err := lpk.OpenFile(context.Background(), destination)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	if got := reader.Layout(); got != lpk.LayoutV2 {
		t.Fatalf("layout = %q", got)
	}
	if got := string(mustReadEntry(t, reader, "payload.txt")); got != "hello v2\n" {
		t.Fatalf("payload = %q", got)
	}
}

func TestResourceOnlyV2HasPackageInfoWithoutManifest(t *testing.T) {
	data := writePackage(t, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: resourceOnlyRoot(), Strict: true})
	reader, err := lpk.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })

	if _, err := reader.Manifest(context.Background()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Manifest() error = %v; want fs.ErrNotExist", err)
	}
	packageInfo, err := reader.PackageInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertLookup(t, packageInfo, "cloud.lazycat.apps.resources", "package")
	if got := string(mustReadEntry(t, reader, "exports/config.txt")); got != "config\n" {
		t.Fatalf("resource = %q", got)
	}

	effective, err := reader.EffectiveManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !effective.HasPackageFile || effective.PackageInfo == nil || effective.PackageInfo.Package != "cloud.lazycat.apps.resources" {
		t.Fatalf("effective package info = %#v", effective.PackageInfo)
	}
	if effective.Manifest.Package != "" || effective.Source != nil {
		t.Fatalf("resource-only manifest state = %#v", effective)
	}
}

func TestReaderReturnsDefensiveManifestDocuments(t *testing.T) {
	data := writePackage(t, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: v2Root(), Strict: true})
	reader, err := lpk.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })

	manifestDocument, err := reader.Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := manifestDocument.Set("changed", "application", "subdomain"); err != nil {
		t.Fatal(err)
	}
	again, err := reader.Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertLookup(t, again, "demo", "application", "subdomain")

	packageInfo, err := reader.PackageInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := packageInfo.Set("changed", "package"); err != nil {
		t.Fatal(err)
	}
	packageAgain, err := reader.PackageInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertLookup(t, packageAgain, "cloud.lazycat.apps.demo", "package")
}

func mustReadEntry(t *testing.T, reader *lpk.Reader, name string) []byte {
	t.Helper()
	contents, err := reader.OpenEntry(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(contents)
	closeErr := contents.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	return data
}

func assertReaderLayout(t *testing.T, reader *lpk.Reader, layout lpk.Layout, format lpkarchive.Format, size int64) {
	t.Helper()
	if got := reader.Layout(); got != layout {
		t.Fatalf("layout = %q, want %q", got, layout)
	}
	if got := reader.Format(); got != format {
		t.Fatalf("format = %q, want %q", got, format)
	}
	if got := reader.Size(); got != size {
		t.Fatalf("size = %d, want %d", got, size)
	}
}
