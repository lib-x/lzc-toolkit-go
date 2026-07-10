// Package rsync implements the local rsync adapter used by lzc-cli project
// synchronization. It executes argv directly and never invokes a shell.
package rsync

import (
	"context"
	"io"
)

const (
	DefaultTarget = "/lzcapp/cache/project-mirror"
	DefaultPort   = 874
)

type Target struct {
	UID       string
	Host      string
	Port      int
	PackageID string
	UserApp   bool
	Directory string
}

type Process struct {
	Name   string
	Args   []string
	Dir    string
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

type Executor interface {
	Run(context.Context, Process) error
}

type Options struct {
	RootDir          string
	SourceDir        string
	Target           Target
	Delete           bool
	DryRun           bool
	Debug            bool
	EnsureIgnoreFile *bool
	Binary           string
	Executor         Executor
	Stdout           io.Writer
	Stderr           io.Writer
}

type Result struct {
	Version     string
	Changed     bool
	Source      string
	Destination string
}

type TunnelOptions struct {
	SSHArgs    []string
	LocalPort  int
	TargetHost string
	TargetPort int
}
