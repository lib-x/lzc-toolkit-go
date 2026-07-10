package lpk_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	lpkarchive "github.com/lib-x/lzc-toolkit-go/archive"
	"github.com/lib-x/lzc-toolkit-go/lpk"
)

func TestWriteV1ProducesReproducibleZIPAndResult(t *testing.T) {
	root := fstest.MapFS{
		"manifest.yml": &fstest.MapFile{Data: []byte("package: cloud.lazycat.apps.demo\nversion: 1.0.0\n")},
		"content.txt":  &fstest.MapFile{Data: []byte("hello\n")},
	}
	var output bytes.Buffer

	result, err := lpk.Write(context.Background(), &output, lpk.WriteRequest{
		Layout: lpk.LayoutV1,
		Files:  root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Layout != lpk.LayoutV1 || result.Format != lpkarchive.FormatZIP {
		t.Fatalf("result = %#v", result)
	}
	if result.Size != int64(output.Len()) {
		t.Fatalf("size = %d, want %d", result.Size, output.Len())
	}
	if got, want := result.SHA256, sha256.Sum256(output.Bytes()); got != want {
		t.Fatalf("digest = %x, want %x", got, want)
	}
	reader, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 2 {
		t.Fatalf("ZIP files = %#v", reader.File)
	}

	var second bytes.Buffer
	if _, err := lpk.Write(context.Background(), &second, lpk.WriteRequest{Layout: lpk.LayoutV1, Files: root}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), second.Bytes()) {
		t.Fatal("v1 output is not reproducible")
	}
}

func TestWriteV2RequiresPackageFileBeforeWriting(t *testing.T) {
	root := fstest.MapFS{
		"manifest.yml": &fstest.MapFile{Data: []byte("application:\n  image: demo\n")},
	}
	var output bytes.Buffer

	_, err := lpk.Write(context.Background(), &output, lpk.WriteRequest{
		Layout: lpk.LayoutV2,
		Files:  root,
	})
	if !errors.Is(err, lpkgo.ErrInvalidManifest) {
		t.Fatalf("error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("validation wrote %d bytes", output.Len())
	}
}

func TestWriteV2WithoutManifestRequiresExports(t *testing.T) {
	root := fstest.MapFS{
		"package.yml": &fstest.MapFile{Data: []byte("package: cloud.lazycat.apps.resources\nversion: 1.0.0\n")},
	}
	var output bytes.Buffer

	_, err := lpk.Write(context.Background(), &output, lpk.WriteRequest{
		Layout: lpk.LayoutV2,
		Files:  root,
	})
	if !errors.Is(err, lpkgo.ErrInvalidManifest) {
		t.Fatalf("error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("validation wrote %d bytes", output.Len())
	}
}

func TestWriteV2ResourceOnlyPermitsPackageAndExports(t *testing.T) {
	var output bytes.Buffer

	result, err := lpk.Write(context.Background(), &output, lpk.WriteRequest{
		Layout: lpk.LayoutV2,
		Files:  resourceOnlyRoot(),
		Strict: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Layout != lpk.LayoutV2 || result.Format != lpkarchive.FormatTAR {
		t.Fatalf("result = %#v", result)
	}
	if output.Len() == 0 {
		t.Fatal("empty output")
	}
}

func TestWriteV1RejectsResourceOnlyPackage(t *testing.T) {
	var output bytes.Buffer

	_, err := lpk.Write(context.Background(), &output, lpk.WriteRequest{
		Layout: lpk.LayoutV1,
		Files:  resourceOnlyRoot(),
	})
	if !errors.Is(err, lpkgo.ErrInvalidManifest) {
		t.Fatalf("error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("validation wrote %d bytes", output.Len())
	}
}

func TestWriteV2StrictRejectsStaticManifestFields(t *testing.T) {
	root := fstest.MapFS{
		"package.yml":  &fstest.MapFile{Data: []byte("package: cloud.lazycat.apps.demo\nversion: 1.0.0\n")},
		"manifest.yml": &fstest.MapFile{Data: []byte("name: Demo\napplication:\n  subdomain: demo\n")},
	}
	var output bytes.Buffer

	_, err := lpk.Write(context.Background(), &output, lpk.WriteRequest{
		Layout: lpk.LayoutV2,
		Files:  root,
		Strict: true,
	})
	if !errors.Is(err, lpkgo.ErrInvalidManifest) {
		t.Fatalf("error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("validation wrote %d bytes", output.Len())
	}
}

func TestWriteManifestTemplateRequiresExplicitCompatibilityOption(t *testing.T) {
	root := fstest.MapFS{
		"package.yml":  &fstest.MapFile{Data: []byte("package: cloud.lazycat.apps.demo\nversion: 1.0.0\n")},
		"manifest.yml": &fstest.MapFile{Data: []byte("application:\n  subdomain: demo\n{{if .U.enabled}}\n  background_task: true\n{{end}}\n")},
	}
	var rejected bytes.Buffer
	_, err := lpk.Write(context.Background(), &rejected, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: root})
	if !errors.Is(err, lpkgo.ErrInvalidManifest) || rejected.Len() != 0 {
		t.Fatalf("error=%v bytes=%d", err, rejected.Len())
	}

	var accepted bytes.Buffer
	_, err = lpk.Write(context.Background(), &accepted, lpk.WriteRequest{
		Layout:                lpk.LayoutV2,
		Files:                 root,
		AllowManifestTemplate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Len() == 0 {
		t.Fatal("empty output")
	}
}

func TestWriteFileIsAtomicAndReportsFinalFile(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "nested", "demo.lpk")

	result, err := lpk.WriteFile(context.Background(), destination, lpk.WriteRequest{
		Layout: lpk.LayoutV2,
		Files:  v2Root(),
		Strict: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if result.Layout != lpk.LayoutV2 || result.Format != lpkarchive.FormatTAR {
		t.Fatalf("result = %#v", result)
	}
	if result.Size != int64(len(contents)) {
		t.Fatalf("size = %d, want %d", result.Size, len(contents))
	}
	if got, want := result.SHA256, sha256.Sum256(contents); got != want {
		t.Fatalf("digest = %x, want %x", got, want)
	}

	_, err = lpk.WriteFile(context.Background(), destination, lpk.WriteRequest{
		Layout: lpk.LayoutV2,
		Files:  fstest.MapFS{"manifest.yml": &fstest.MapFile{Data: []byte("application:\n  subdomain: demo\n")}},
	})
	if !errors.Is(err, lpkgo.ErrInvalidManifest) {
		t.Fatalf("failure error = %v", err)
	}
	after, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, contents) {
		t.Fatal("failed WriteFile changed destination")
	}
}
