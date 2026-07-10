package inspect_test

import (
	"bytes"
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/lib-x/lzc-toolkit-go/lpk"
)

const fsModeDir = fs.ModeDir

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

func resourceOnlyRoot() fstest.MapFS {
	return fstest.MapFS{
		"package.yml":        &fstest.MapFile{Data: []byte("package: cloud.lazycat.apps.resources\nversion: 1.0.0\n"), Mode: 0o644},
		"exports":            &fstest.MapFile{Mode: 0o755 | fs.ModeDir},
		"exports/config.txt": &fstest.MapFile{Data: []byte("config\n"), Mode: 0o644},
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
