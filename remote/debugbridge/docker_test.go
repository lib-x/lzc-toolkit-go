package debugbridge_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/remote"
	"github.com/lib-x/lpk-go/remote/debugbridge"
)

func TestDockerAndComposePropagateStreamsAndTTY(t *testing.T) {
	runner := &fakeRunner{results: []remote.Result{
		{Stdout: []byte("docker-out"), Stderr: []byte("docker-err"), ExitCode: 0},
		{Stdout: []byte("compose-out"), ExitCode: 0},
	}}
	client := debugbridge.New(runner, bridgeCommand)
	stdin := bytes.NewBufferString("stdin-data")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	result, err := client.Docker(context.Background(), remote.StreamRequest{
		Args:  []string{"exec", "container-1", "echo", "hello world"},
		Stdin: stdin, Stdout: &stdout, Stderr: &stderr, TTY: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || stdout.String() != "docker-out" {
		t.Fatalf("result=%#v stdout=%q", result, stdout.String())
	}
	if _, err := client.DockerCompose(context.Background(), remote.StreamRequest{Args: []string{"-p", "cloudexampleapp", "ps"}}); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(runner.commands[0].Args, []string{"lzc-docker", "exec", "container-1", "echo", "hello world"}) || !runner.commands[0].TTY {
		t.Fatalf("docker command=%#v", runner.commands[0])
	}
	if !reflect.DeepEqual(runner.commands[1].Args, []string{"lzc-docker-compose", "-p", "cloudexampleapp", "ps"}) || runner.commands[1].TTY {
		t.Fatalf("compose command=%#v", runner.commands[1])
	}
	data, err := io.ReadAll(runner.commands[0].Stdin)
	if err != nil || string(data) != "stdin-data" {
		t.Fatalf("stdin=%q err=%v", data, err)
	}
}

func TestDockerRejectsControlCharacters(t *testing.T) {
	client := debugbridge.New(&fakeRunner{}, bridgeCommand)
	_, err := client.Docker(context.Background(), remote.StreamRequest{Args: []string{"exec", "bad\nargument"}})
	if !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("error=%#v", err)
	}
}

func TestHostReadFileUsesHostTransport(t *testing.T) {
	runner := &fakeRunner{results: []remote.Result{{Stdout: []byte("contents")}}}
	hostCommand := func(tty bool, name string, args ...string) remote.Command {
		command := remote.NewCommand(name, args...)
		command.TTY = tty
		return command
	}
	client := debugbridge.New(runner, bridgeCommand, debugbridge.WithHostCommand(hostCommand))
	var destination bytes.Buffer

	if err := client.HostReadFile(context.Background(), "/data/system/pkgm/run/deploy-1/errmsg", &destination); err != nil {
		t.Fatal(err)
	}
	if destination.String() != "contents" {
		t.Fatalf("destination=%q", destination.String())
	}
	command := runner.commands[0]
	if command.Name != "cat" || !reflect.DeepEqual(command.Args, []string{"/data/system/pkgm/run/deploy-1/errmsg"}) {
		t.Fatalf("command=%#v", command)
	}
}
