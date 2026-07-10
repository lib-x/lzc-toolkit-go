package project_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/lib-x/lpk-go/project"
	"github.com/lib-x/lpk-go/remote"
)

func TestDockerAndComposePassThroughTypedStreams(t *testing.T) {
	backend := &fakeBackend{
		dockerResults:  []remote.Result{{Stdout: []byte("docker output"), ExitCode: 0}},
		composeResults: []remote.Result{{Stdout: []byte("compose output"), ExitCode: 0}},
	}
	service, err := project.New(project.Options{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	stdin := bytes.NewBufferString("input")
	var stdout bytes.Buffer

	dockerResult, err := service.Docker(t.Context(), project.DockerRequest{
		Args: []string{"inspect", "container-1"}, Stdin: stdin, Stdout: &stdout, TTY: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	composeResult, err := service.Compose(t.Context(), project.DockerRequest{Args: []string{"ps"}})
	if err != nil {
		t.Fatal(err)
	}
	if dockerResult.ExitCode != 0 || composeResult.ExitCode != 0 || stdout.String() != "docker output" {
		t.Fatalf("docker=%#v compose=%#v stdout=%q", dockerResult, composeResult, stdout.String())
	}
	if !reflect.DeepEqual(backend.dockerRequests[0].Args, []string{"inspect", "container-1"}) || !backend.dockerRequests[0].TTY || backend.dockerRequests[0].Stdin != stdin {
		t.Fatalf("docker request=%#v", backend.dockerRequests[0])
	}
	if !reflect.DeepEqual(backend.composeRequests[0].Args, []string{"ps"}) {
		t.Fatalf("compose request=%#v", backend.composeRequests[0])
	}
}

func TestComposeProjectNameMatchesLZCCLI(t *testing.T) {
	name, err := project.ComposeProjectName("cloud.lazycat.apps.example")
	if err != nil {
		t.Fatal(err)
	}
	if name != "cloudlazycatappsexample" {
		t.Fatalf("name=%q", name)
	}
}
