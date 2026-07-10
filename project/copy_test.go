package project_test

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/lib-x/lpk-go/project"
	"github.com/lib-x/lpk-go/remote"
)

func TestCopyToStreamsPortableTarToDockerCP(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	backend := &fakeBackend{
		composeResults: []remote.Result{
			{Stdout: []byte(`[{"Name":"cloudexampleapp","Status":"running(1)"}]`)},
			{Stdout: []byte("container-1")},
		},
		captureDockerStdin: true,
		dockerResults:      []remote.Result{{ExitCode: 0}},
	}
	service, err := project.New(project.Options{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.CopyTo(t.Context(), project.CopyRequest{
		AppID: "cloud.example.app", SourcePath: source, Destination: "/opt/app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(backend.dockerRequests[0].Args, []string{"cp", "-", "container-1:/opt/app"}) {
		t.Fatalf("args=%#v", backend.dockerRequests[0].Args)
	}
	if result.ContainerID != "container-1" || result.Destination != "/opt/app" || result.SourcePath != source {
		t.Fatalf("result=%#v", result)
	}
	archive := tar.NewReader(bytes.NewReader(backend.dockerStdin[0]))
	entries := map[string]string{}
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(archive)
		if err != nil {
			t.Fatal(err)
		}
		entries[header.Name] = string(data)
	}
	if entries["source/hello.txt"] != "hello" {
		t.Fatalf("entries=%#v", entries)
	}
}

func TestCopyToRejectsServicePrefixedDestination(t *testing.T) {
	backend := &fakeBackend{composeResults: []remote.Result{
		{Stdout: []byte(`[{"Name":"cloudexampleapp","Status":"running(1)"}]`)},
		{Stdout: []byte("container-1")},
	}}
	service, err := project.New(project.Options{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CopyTo(t.Context(), project.CopyRequest{
		AppID: "cloud.example.app", SourcePath: t.TempDir(), Destination: "app:/opt/app",
	})
	if err == nil {
		t.Fatal("CopyTo accepted service-prefixed destination")
	}
}
