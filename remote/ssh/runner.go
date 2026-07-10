// Package ssh implements the explicit SSH transport used by lzc-cli remote
// build mode. It invokes an SSH executable directly and never uses a local
// shell command string.
package ssh

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/remote"
)

const (
	defaultMaxOutputBytes = int64(4 << 20)
	debugBridgeContainer  = "cloudlazycatdevelopertools-app-1"
	debugBridgeBinary     = "/lzcapp/pkg/content/debug.bridge"
)

type Executor interface {
	Run(context.Context, string, []string, io.Reader, io.Writer, io.Writer) error
}

type Options struct {
	Target         Target
	Executor       Executor
	SSHBinary      string
	MaxOutputBytes int64
	Quiet          *bool
}

type Runner struct {
	target         Target
	executor       Executor
	sshBinary      string
	maxOutputBytes int64
	quiet          bool
}

func New(options Options) *Runner {
	executor := options.Executor
	if executor == nil {
		executor = commandExecutor{}
	}
	sshBinary := strings.TrimSpace(options.SSHBinary)
	if sshBinary == "" {
		sshBinary = "ssh"
	}
	maximum := options.MaxOutputBytes
	if maximum <= 0 {
		maximum = defaultMaxOutputBytes
	}
	quiet := true
	if options.Quiet != nil {
		quiet = *options.Quiet
	}
	return &Runner{target: options.Target, executor: executor, sshBinary: sshBinary, maxOutputBytes: maximum, quiet: quiet}
}

func (runner *Runner) Run(ctx context.Context, command remote.Command) (remote.Result, error) {
	result := remote.Result{ExitCode: -1}
	if ctx == nil || runner == nil || runner.executor == nil || runner.target.SSHAddress() == "" || runner.target.Port < 1 || runner.target.Port > 65535 {
		return result, sshError(lpkgo.CodeInvalidArgument, "remote.ssh.run", errors.New("invalid context, runner, or target"))
	}
	name := strings.TrimSpace(command.Name)
	if name == "" {
		return result, sshError(lpkgo.CodeInvalidArgument, "remote.ssh.run", errors.New("remote command name is required"))
	}
	if err := ctx.Err(); err != nil {
		return result, sshError(lpkgo.CodeCancelled, "remote.ssh.run", err)
	}

	args := []string{"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null"}
	if runner.quiet {
		args = append(args, "-q")
	}
	args = append(args, "-p", strconv.Itoa(runner.target.Port))
	if command.TTY {
		args = append(args, "-t")
	}
	args = append(args, runner.target.SSHAddress(), name)
	args = append(args, command.Args...)

	stdout := &boundedBuffer{remaining: runner.maxOutputBytes}
	stderr := &boundedBuffer{remaining: runner.maxOutputBytes}
	stdoutWriter := io.Writer(stdout)
	stderrWriter := io.Writer(stderr)
	if command.Stdout != nil {
		stdoutWriter = io.MultiWriter(stdout, command.Stdout)
	}
	if command.Stderr != nil {
		stderrWriter = io.MultiWriter(stderr, command.Stderr)
	}

	runErr := runner.executor.Run(ctx, runner.sshBinary, append([]string(nil), args...), command.Stdin, stdoutWriter, stderrWriter)
	result.Stdout = stdout.Bytes()
	result.Stderr = stderr.Bytes()
	if runErr == nil {
		result.ExitCode = 0
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, sshError(lpkgo.CodeCancelled, "remote.ssh.run", err)
	}
	var exitCoder interface{ ExitCode() int }
	if errors.As(runErr, &exitCoder) {
		result.ExitCode = exitCoder.ExitCode()
	}
	return result, sshError(lpkgo.CodeCommandFailed, "remote.ssh.run", errors.New("SSH command failed"))
}

func (runner *Runner) BridgeCommand(tty bool, args ...string) remote.Command {
	mode := "-i"
	if tty {
		mode = "-it"
	}
	command := remote.NewCommand(
		"lzc-docker",
		append([]string{"exec", mode, debugBridgeContainer, debugBridgeBinary}, args...)...,
	)
	command.TTY = tty
	return command
}

type commandExecutor struct{}

func (commandExecutor) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

type boundedBuffer struct {
	buffer    bytes.Buffer
	remaining int64
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	if buffer.remaining > 0 {
		keep := min(int64(len(data)), buffer.remaining)
		_, _ = buffer.buffer.Write(data[:keep])
		buffer.remaining -= keep
	}
	return len(data), nil
}

func (buffer *boundedBuffer) Bytes() []byte {
	return append([]byte(nil), buffer.buffer.Bytes()...)
}
