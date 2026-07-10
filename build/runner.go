package build

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"sort"
)

// ShellRunner preserves lzc-cli shell behavior: sh -c on Unix-like systems
// and cmd /c on Windows.
type ShellRunner struct{}

func (ShellRunner) Run(ctx context.Context, command Command) error {
	if err := checkContext(ctx, "build.shell_runner"); err != nil {
		return err
	}
	if command.Script == "" {
		return errors.New("empty build script")
	}
	name, flag := "sh", "-c"
	if runtime.GOOS == "windows" {
		name, flag = "cmd", "/c"
	}
	cmd := exec.CommandContext(ctx, name, flag, command.Script)
	cmd.Dir = command.Dir
	keys := make([]string, 0, len(command.Env))
	for key := range command.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	cmd.Env = make([]string, 0, len(keys))
	for _, key := range keys {
		cmd.Env = append(cmd.Env, key+"="+command.Env[key])
	}
	return cmd.Run()
}
