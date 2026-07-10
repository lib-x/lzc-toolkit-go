// Package remote defines dependency-light LazyCat backend contracts.
// Transport implementations live in subpackages so base LPK/build users do
// not import SSH or gRPC dependencies.
package remote

import (
	"context"
	"io"
)

type Platform struct {
	OS           string
	Architecture string
}

func (platform Platform) String() string {
	if platform.OS == "" || platform.Architecture == "" {
		return ""
	}
	return platform.OS + "/" + platform.Architecture
}

type BackendInfo struct {
	Version  string
	Platform Platform
}

type Command struct {
	Name   string
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	TTY    bool
}

func NewCommand(name string, args ...string) Command {
	return Command{Name: name, Args: append([]string(nil), args...)}
}

type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

type Runner interface {
	Run(context.Context, Command) (Result, error)
}
