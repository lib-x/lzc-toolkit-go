package remote

import (
	"context"
	"encoding/json"
	"io"
)

type AppInfo struct {
	AppID          string          `json:"appid"`
	DeployID       string          `json:"deploy_id"`
	Version        string          `json:"version"`
	Domain         string          `json:"domain"`
	Status         string          `json:"status"`
	InstanceStatus string          `json:"instance_status"`
	ErrorReason    string          `json:"error_reason"`
	Raw            json.RawMessage `json:"-"`
}

type InstallRequest struct {
	Package   io.Reader
	PackageID string
}

type SyncDevIDRequest struct {
	AppID   string
	DevID   string
	UserApp bool
}

type StreamRequest struct {
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	TTY    bool
}

type LifecycleBackend interface {
	Info(context.Context) (BackendInfo, error)
	Install(context.Context, InstallRequest) error
	Status(context.Context, string) (string, error)
	AppInfo(context.Context, string) (AppInfo, error)
	SyncDevID(context.Context, SyncDevIDRequest) error
	IsDevshell(context.Context, string) (bool, error)
	Pause(context.Context, string) error
	Resume(context.Context, string) error
	Uninstall(context.Context, string, bool) error
	Docker(context.Context, StreamRequest) (Result, error)
	DockerCompose(context.Context, StreamRequest) (Result, error)
	HostReadFile(context.Context, string, io.Writer) error
}
