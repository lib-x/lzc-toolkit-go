package project_test

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/project"
	"github.com/lib-x/lpk-go/remote"
)

func TestExecResolvesServiceEnsuresWorkdirAndUsesDefaults(t *testing.T) {
	backend := &fakeBackend{composeResults: []remote.Result{
		{Stdout: []byte(`[{"Name":"cloudexampleapp","Status":"running(1)","ConfigFiles":"compose.yml"}]`)},
		{Stdout: []byte("container-1\n")},
		{},
		{Stdout: []byte("exec output")},
	}}
	service, err := project.New(project.Options{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	stdin := bytes.NewBufferString("input")
	var stdout bytes.Buffer

	result, err := service.Exec(t.Context(), project.ExecRequest{
		AppID: "cloud.example.app", Stdin: stdin, Stdout: &stdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := [][]string{
		{"ls", "--format", "json"},
		{"-p", "cloudexampleapp", "ps", "--status", "running", "-q", "app"},
		{"-p", "cloudexampleapp", "exec", "-T", "app", "mkdir", "-p", "/lzcapp/cache/project-mirror"},
		{"-p", "cloudexampleapp", "exec", "--workdir", "/lzcapp/cache/project-mirror", "app", "/bin/sh"},
	}
	for index, want := range wantArgs {
		if !reflect.DeepEqual(backend.composeRequests[index].Args, want) {
			t.Fatalf("request %d args=%#v want=%#v", index, backend.composeRequests[index].Args, want)
		}
	}
	final := backend.composeRequests[3]
	if !final.TTY || final.Stdin != stdin || final.Stdout != &stdout || result.ExitCode != 0 || stdout.String() != "exec output" {
		t.Fatalf("request=%#v result=%#v stdout=%q", final, result, stdout.String())
	}
}

func TestExecRejectsUnsafeWorkdir(t *testing.T) {
	backend := &fakeBackend{composeResults: []remote.Result{
		{Stdout: []byte(`[{"Name":"cloudexampleapp","Status":"running(1)"}]`)},
		{Stdout: []byte("container-1")},
	}}
	service, err := project.New(project.Options{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	workdir := "/lzcapp/cache/../data"
	if _, err := service.Exec(t.Context(), project.ExecRequest{AppID: "cloud.example.app", Workdir: &workdir}); err == nil {
		t.Fatal("Exec accepted unsafe workdir")
	}
}

func TestExecDisablesTTYWithComposeTFlag(t *testing.T) {
	backend := &fakeBackend{composeResults: []remote.Result{
		{Stdout: []byte(`[{"Name":"cloudexampleapp","Status":"running(1)"}]`)},
		{Stdout: []byte("container-1")},
		{},
		{},
	}}
	service, err := project.New(project.Options{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	tty := false
	if _, err := service.Exec(t.Context(), project.ExecRequest{AppID: "cloud.example.app", Command: []string{"printf", "%s", "hello world"}, TTY: &tty}); err != nil {
		t.Fatal(err)
	}
	want := []string{"-p", "cloudexampleapp", "exec", "--workdir", "/lzcapp/cache/project-mirror", "-T", "app", "printf", "%s", "hello world"}
	request := backend.composeRequests[3]
	if request.TTY || !reflect.DeepEqual(request.Args, want) {
		t.Fatalf("request=%#v", request)
	}
}

func TestComposeProjectsRejectsOversizedCapture(t *testing.T) {
	backend := &fakeBackend{composeResults: []remote.Result{{Stdout: []byte("12345")}}}
	service, err := project.New(project.Options{Backend: backend, MaxCaptureBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ComposeProjects(t.Context(), "cloud.example.app")
	if !errors.Is(err, lpkgo.ErrRemoteUnavailable) {
		t.Fatalf("error=%#v", err)
	}
}
