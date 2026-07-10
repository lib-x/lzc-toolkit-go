package project

import (
	"context"
	"errors"
	"path"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/remote"
)

const DefaultSyncTarget = "/lzcapp/cache/project-mirror"

func (service *Service) Exec(ctx context.Context, request ExecRequest) (remote.Result, error) {
	appID := strings.TrimSpace(request.AppID)
	serviceName := normalizedService(request.Service)
	if err := service.validate(ctx, "project.exec", appID); err != nil {
		return remote.Result{}, err
	}
	if !validIdentifier(serviceName) {
		return remote.Result{}, projectError(lpkgo.CodeInvalidArgument, "project.exec", errors.New("invalid service name"))
	}
	workdir := DefaultSyncTarget
	if request.Workdir != nil {
		workdir = strings.TrimSpace(*request.Workdir)
	}
	if workdir != "" && !safeAbsoluteContainerPath(workdir) {
		return remote.Result{}, projectError(lpkgo.CodeInvalidArgument, "project.exec", errors.New("invalid workdir"))
	}
	command := append([]string(nil), request.Command...)
	if len(command) == 0 {
		command = []string{"/bin/sh"}
	}
	if err := validateCommandArgs(command); err != nil {
		return remote.Result{}, err
	}
	if _, err := service.ensureServiceRunning(ctx, appID, serviceName); err != nil {
		return remote.Result{}, err
	}
	composeName, _ := ComposeProjectName(appID)
	if workdir != "" {
		if _, err := service.Compose(ctx, DockerRequest{Args: []string{"-p", composeName, "exec", "-T", serviceName, "mkdir", "-p", workdir}}); err != nil {
			return remote.Result{}, err
		}
	}
	args := []string{"-p", composeName, "exec"}
	if workdir != "" {
		args = append(args, "--workdir", workdir)
	}
	tty := true
	if request.TTY != nil {
		tty = *request.TTY
	}
	if !tty {
		args = append(args, "-T")
	}
	args = append(args, serviceName)
	args = append(args, command...)
	return service.Compose(ctx, DockerRequest{
		Args: args, Stdin: request.Stdin, Stdout: request.Stdout, Stderr: request.Stderr, TTY: tty,
	})
}

func safeAbsoluteContainerPath(value string) bool {
	return value != "" && !strings.ContainsAny(value, "\r\n\x00") && path.IsAbs(value) && path.Clean(value) == value
}
