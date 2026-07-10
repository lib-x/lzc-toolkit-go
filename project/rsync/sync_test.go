package rsync_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/project/rsync"
)

type fakeExecutor struct {
	processes []rsync.Process
	outputs   []string
	errors    []error
}

func (executor *fakeExecutor) Run(_ context.Context, process rsync.Process) error {
	copyProcess := process
	copyProcess.Args = append([]string(nil), process.Args...)
	copyProcess.Env = append([]string(nil), process.Env...)
	executor.processes = append(executor.processes, copyProcess)
	index := len(executor.processes) - 1
	if index < len(executor.outputs) && process.Stdout != nil {
		_, _ = io.WriteString(process.Stdout, executor.outputs[index])
	}
	if index < len(executor.errors) {
		return executor.errors[index]
	}
	return nil
}

func TestSyncChecksVersionCreatesIgnoreAndUsesPasswordEnvironment(t *testing.T) {
	t.Setenv("RSYNC_PASSWORD", "caller-secret")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("vendor\nnode_modules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{outputs: []string{
		"rsync  version 3.2.7  protocol version 31\n",
		"sending incremental file list\n>f+++++++++ app.go\nsent 10 bytes\ntotal size is 4\n",
	}}
	var stdout strings.Builder

	result, err := rsync.Sync(t.Context(), rsync.Options{
		RootDir: root, Delete: true, Executor: executor, Stdout: &stdout,
		Target: rsync.Target{UID: "user-1", Host: "box.example", PackageID: "cloud.example.app"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "3.2.7" || !result.Changed || result.Source != "./" {
		t.Fatalf("result=%#v", result)
	}
	if len(executor.processes) != 2 || executor.processes[0].Args[0] != "--version" || executor.processes[1].Dir != root {
		t.Fatalf("processes=%#v", executor.processes)
	}
	passwordCount := 0
	for _, item := range executor.processes[1].Env {
		if strings.HasPrefix(item, "RSYNC_PASSWORD=") {
			passwordCount++
			if item != "RSYNC_PASSWORD=fakefakefake" {
				t.Fatalf("password env=%q", item)
			}
		}
	}
	if passwordCount != 1 {
		t.Fatalf("password count=%d", passwordCount)
	}
	ignore, err := os.ReadFile(filepath.Join(root, ".lzcdevignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignore), "vendor\n") || strings.Count(string(ignore), "node_modules\n") != 1 {
		t.Fatalf("ignore=%q", ignore)
	}
	if stdout.String() == "" {
		t.Fatal("sync output was not streamed to caller")
	}
}

func TestSyncRejectsOldRsyncVersion(t *testing.T) {
	executor := &fakeExecutor{outputs: []string{"rsync version 3.1.3 protocol version 31\n"}}
	_, err := rsync.Sync(t.Context(), rsync.Options{
		RootDir: t.TempDir(), Executor: executor,
		Target: rsync.Target{UID: "user-1", Host: "box.example", PackageID: "cloud.example.app"},
	})
	if !errors.Is(err, lpkgo.ErrIncompatibleBackend) {
		t.Fatalf("error=%#v", err)
	}
}

func TestSyncFailureDoesNotExposePasswordOrExecutorError(t *testing.T) {
	executor := &fakeExecutor{
		outputs: []string{"rsync version 3.2.0 protocol version 31\n"},
		errors:  []error{nil, errors.New("RSYNC_PASSWORD=fakefakefake private output")},
	}
	_, err := rsync.Sync(t.Context(), rsync.Options{
		RootDir: t.TempDir(), Executor: executor,
		Target: rsync.Target{UID: "user-1", Host: "box.example", PackageID: "cloud.example.app"},
	})
	if !errors.Is(err, lpkgo.ErrCommandFailed) || err.Error() != string(lpkgo.CodeCommandFailed) {
		t.Fatalf("error=%#v text=%q", err, err.Error())
	}
}
