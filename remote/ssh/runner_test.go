package ssh_test

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
	sshremote "github.com/lib-x/lpk-go/remote/ssh"
)

type executorCall struct {
	name string
	args []string
}

type fakeExecutor struct {
	call   executorCall
	stdout string
	stderr string
	err    error
}

func (executor *fakeExecutor) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, stderr io.Writer) error {
	executor.call = executorCall{name: name, args: append([]string(nil), args...)}
	_, _ = io.WriteString(stdout, executor.stdout)
	_, _ = io.WriteString(stderr, executor.stderr)
	return executor.err
}

func TestRunnerBuildsRemoteArgvWithoutShell(t *testing.T) {
	target, err := sshremote.ParseTarget("developer", "box.example:2222")
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{stdout: "1.0.5\n"}
	runner := sshremote.New(sshremote.Options{Target: target, Executor: executor})
	var _ debugbridge.CommandFactory = runner.BridgeCommand
	var _ debugbridge.HostCommandFactory = runner.HostCommand
	var streamed bytes.Buffer
	command := remote.NewCommand("version", "--json")
	command.Stdout = &streamed

	result, err := runner.Run(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-q", "-p", "2222",
		"developer@box.example", "version", "--json",
	}
	if executor.call.name != "ssh" || !reflect.DeepEqual(executor.call.args, wantArgs) {
		t.Fatalf("call = %#v", executor.call)
	}
	if string(result.Stdout) != "1.0.5\n" || streamed.String() != "1.0.5\n" || result.ExitCode != 0 {
		t.Fatalf("result=%#v streamed=%q", result, streamed.String())
	}
}

func TestBridgeCommandMatchesDebugBridgeRemoteMode(t *testing.T) {
	target, err := sshremote.ParseTarget("developer", "box.example")
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{}
	runner := sshremote.New(sshremote.Options{Target: target, Executor: executor})

	command := runner.BridgeCommand(true, "platform")
	if _, err := runner.Run(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	wantTail := []string{
		"-t", "developer@box.example",
		"lzc-docker", "exec", "-it", "cloudlazycatdevelopertools-app-1", "/lzcapp/pkg/content/debug.bridge", "platform",
	}
	if !reflect.DeepEqual(executor.call.args[len(executor.call.args)-len(wantTail):], wantTail) {
		t.Fatalf("args = %#v", executor.call.args)
	}
}

func TestRunnerReturnsBoundedOutputAndStableError(t *testing.T) {
	target, err := sshremote.ParseTarget("developer", "box.example")
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{stdout: "123456789", stderr: "secret remote detail", err: errors.New("exit status 7")}
	runner := sshremote.New(sshremote.Options{Target: target, Executor: executor, MaxOutputBytes: 4})

	result, err := runner.Run(context.Background(), remote.NewCommand("version"))
	if !errors.Is(err, lpkgo.ErrCommandFailed) {
		t.Fatalf("error = %#v", err)
	}
	if string(result.Stdout) != "1234" || string(result.Stderr) != "secr" || result.ExitCode != -1 {
		t.Fatalf("result = %#v", result)
	}
	if err.Error() != string(lpkgo.CodeCommandFailed) {
		t.Fatalf("error text = %q", err.Error())
	}
}
