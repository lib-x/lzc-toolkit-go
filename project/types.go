// Package project orchestrates the LazyCat application lifecycle over an
// injected remote backend. Transport adapters remain in remote subpackages.
package project

import (
	"encoding/json"
	"io"
	"time"

	"github.com/lib-x/lpk-go/remote"
)

const (
	defaultPollInterval    = time.Second
	defaultStartTimeout    = 90 * time.Second
	defaultStopTimeout     = 30 * time.Second
	defaultMaxCaptureBytes = int64(4 << 20)
)

type Options struct {
	Backend         remote.LifecycleBackend
	PollInterval    time.Duration
	StartTimeout    time.Duration
	StopTimeout     time.Duration
	MaxCaptureBytes int64
}

type DockerRequest = remote.StreamRequest

type ComposeProject struct {
	Name        string `json:"Name"`
	Status      string `json:"Status"`
	ConfigFiles string `json:"ConfigFiles"`
}

type ExecRequest struct {
	AppID   string
	Service string
	Workdir *string
	Command []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	TTY     *bool
}

type LogRequest struct {
	AppID   string
	Service string
	Follow  *bool
	Tail    *int
	Since   string
	Stdout  io.Writer
	Stderr  io.Writer
	TTY     *bool
}

type CopyRequest struct {
	AppID       string
	Service     string
	SourcePath  string
	Destination string
	Stdout      io.Writer
	Stderr      io.Writer
}

type CopyResult struct {
	ContainerID string
	SourcePath  string
	Destination string
	Command     remote.Result
}

type Info struct {
	AppID                  string
	DeployID               string
	LocalVersion           string
	DeployedVersion        string
	Domain                 string
	AppStatus              string
	InstanceStatus         string
	ErrorReason            string
	Deployed               bool
	CurrentVersionDeployed bool
	Running                bool
	Raw                    json.RawMessage
}

type InfoRequest struct {
	AppID        string
	LocalVersion string
}

type DeployRequest struct {
	Package   io.Reader
	PackageID string
	DevID     string
	UserApp   bool
}

type DeployResult struct {
	Info               Info
	Backend            remote.BackendInfo
	WaitedForContainer bool
	SyncedDevID        bool
}

type WaitState string

const (
	WaitRunning    WaitState = "RUNNING"
	WaitNotRunning WaitState = "NOT_RUNNING"
	WaitStarting   WaitState = "STARTING"
	WaitDevshell   WaitState = "DEVSHELL"
)

type WaitRequest struct {
	AppID        string
	LocalVersion string
	State        WaitState
	Timeout      time.Duration
}

type StartRequest struct {
	AppID        string
	LocalVersion string
	Restart      bool
}

type StopRequest struct {
	AppID        string
	LocalVersion string
}

type UninstallRequest struct {
	AppID      string
	DeleteData bool
}
