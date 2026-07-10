package signature_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/lib-x/lzc-toolkit-go/lpk"
	"github.com/lib-x/lzc-toolkit-go/signature"
)

type testKeys struct {
	privatePEM []byte
	publicPEM  []byte
}

func v1Root() fstest.MapFS {
	return fstest.MapFS{
		"manifest.yml": &fstest.MapFile{Data: []byte(`package: cloud.lazycat.apps.demo
version: 1.0.0
name: Demo
application:
  subdomain: demo
`), Mode: 0o644},
		"payload.txt": &fstest.MapFile{Data: []byte("hello v1\n"), Mode: 0o644},
	}
}

func v2Root() fstest.MapFS {
	return fstest.MapFS{
		"package.yml": &fstest.MapFile{Data: []byte(`package: cloud.lazycat.apps.demo
version: 2.0.0
name: Demo
`), Mode: 0o644},
		"manifest.yml": &fstest.MapFile{Data: []byte(`application:
  subdomain: demo
  image: registry.example/demo:2.0.0
`), Mode: 0o644},
		"payload.txt": &fstest.MapFile{Data: []byte("hello v2\n"), Mode: 0o644},
	}
}

func writePackage(t *testing.T, request lpk.WriteRequest) []byte {
	t.Helper()
	var output bytes.Buffer
	if _, err := lpk.Write(context.Background(), &output, request); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func generateKeys(t *testing.T) testKeys {
	t.Helper()
	pair, err := signature.GenerateKeyPair(signature.GenerateKeyRequest{
		Directory: t.TempDir(),
		Name:      "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	privatePEM, err := os.ReadFile(pair.PrivateKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	publicPEM, err := os.ReadFile(pair.PublicKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	return testKeys{privatePEM: privatePEM, publicPEM: publicPEM}
}

func signPackage(t *testing.T, data []byte, keys testKeys, request signature.SignRequest) []byte {
	t.Helper()
	request.PrivateKeyPEM = keys.privatePEM
	request.PublicKeyPEM = keys.publicPEM
	if request.KeyID == "" {
		request.KeyID = "dev"
	}
	var output bytes.Buffer
	if _, err := signature.Sign(context.Background(), &output, bytes.NewReader(data), request); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func readSignedEntry(t *testing.T, data []byte, name string) []byte {
	t.Helper()
	reader, err := lpk.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	contents, err := reader.OpenEntry(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	defer contents.Close()
	var output bytes.Buffer
	if _, err := output.ReadFrom(contents); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func rewritePackage(t *testing.T, data []byte, mutate func(string)) []byte {
	t.Helper()
	reader, err := lpk.OpenReaderAt(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	workDir := t.TempDir()
	if err := reader.Extract(context.Background(), workDir); err != nil {
		t.Fatal(err)
	}
	mutate(workDir)
	var output bytes.Buffer
	if _, err := lpk.Write(context.Background(), &output, lpk.WriteRequest{
		Layout: reader.Layout(),
		Files:  os.DirFS(workDir),
	}); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func readJSONFile[T any](t *testing.T, filename string) T {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func writeJSONFile(t *testing.T, filename string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func slashPath(root string, name string) string {
	return filepath.Join(append([]string{root}, splitSlash(name)...)...)
}

func splitSlash(name string) []string {
	var parts []string
	for _, part := range bytes.Split([]byte(name), []byte("/")) {
		parts = append(parts, string(part))
	}
	return parts
}
