package remote_test

import (
	"testing"

	"github.com/lib-x/lpk-go/remote"
)

func TestNewCommandCopiesArguments(t *testing.T) {
	args := []string{"version"}
	command := remote.NewCommand("debug.bridge", args...)
	args[0] = "changed"
	if command.Name != "debug.bridge" || len(command.Args) != 1 || command.Args[0] != "version" {
		t.Fatalf("command = %#v", command)
	}
}
