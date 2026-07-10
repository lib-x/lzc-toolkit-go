package project_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/lib-x/lpk-go/project"
	"github.com/lib-x/lpk-go/remote"
)

func TestLogsBuildsReferenceComposeArguments(t *testing.T) {
	backend := &fakeBackend{composeResults: []remote.Result{
		{Stdout: []byte(`[{"Name":"cloudexampleapp","Status":"running(1)"}]`)},
		{Stdout: []byte("container-1")},
		{Stdout: []byte("line one\n")},
	}}
	service, err := project.New(project.Options{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	follow := false
	tail := 50
	var output bytes.Buffer

	_, err = service.Logs(t.Context(), project.LogRequest{
		AppID: "cloud.example.app", Service: "worker", Follow: &follow, Tail: &tail, Since: "10m", Stdout: &output,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-p", "cloudexampleapp", "logs", "--tail", "50", "--since", "10m", "worker"}
	request := backend.composeRequests[2]
	if !reflect.DeepEqual(request.Args, want) || !request.TTY || request.Stdout != &output || output.String() != "line one\n" {
		t.Fatalf("request=%#v output=%q", request, output.String())
	}
}

func TestLogsRejectsNegativeTail(t *testing.T) {
	backend := &fakeBackend{composeResults: []remote.Result{
		{Stdout: []byte(`[{"Name":"cloudexampleapp","Status":"running(1)"}]`)},
		{Stdout: []byte("container-1")},
	}}
	service, err := project.New(project.Options{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	tail := -1
	if _, err := service.Logs(t.Context(), project.LogRequest{AppID: "cloud.example.app", Service: "app", Tail: &tail}); err == nil {
		t.Fatal("Logs accepted negative tail")
	}
}
