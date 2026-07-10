package project

import (
	"context"
	"errors"
	"strconv"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/remote"
)

func (service *Service) Logs(ctx context.Context, request LogRequest) (remote.Result, error) {
	appID := strings.TrimSpace(request.AppID)
	if err := service.validate(ctx, "project.logs", appID); err != nil {
		return remote.Result{}, err
	}
	serviceName := strings.TrimSpace(request.Service)
	if serviceName != "" && !validIdentifier(serviceName) {
		return remote.Result{}, projectError(lpkgo.CodeInvalidArgument, "project.logs", errors.New("invalid service name"))
	}
	tail := 200
	if request.Tail != nil {
		tail = *request.Tail
	}
	if tail < 0 {
		return remote.Result{}, projectError(lpkgo.CodeInvalidArgument, "project.logs", errors.New("invalid tail value"))
	}
	since := strings.TrimSpace(request.Since)
	if strings.ContainsAny(since, "\r\n\x00") {
		return remote.Result{}, projectError(lpkgo.CodeInvalidArgument, "project.logs", errors.New("invalid since value"))
	}
	if serviceName != "" {
		if _, err := service.ensureServiceRunning(ctx, appID, serviceName); err != nil {
			return remote.Result{}, err
		}
	} else {
		info, err := service.Info(ctx, InfoRequest{AppID: appID})
		if err != nil {
			return remote.Result{}, err
		}
		if !info.Running {
			return remote.Result{}, projectError(lpkgo.CodeConflict, "project.logs", errors.New("project app is not running"))
		}
		if err := service.ensureComposeProjectRunning(ctx, appID); err != nil {
			return remote.Result{}, err
		}
	}
	composeName, _ := ComposeProjectName(appID)
	args := []string{"-p", composeName, "logs"}
	follow := true
	if request.Follow != nil {
		follow = *request.Follow
	}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, "--tail", strconv.Itoa(tail))
	if since != "" {
		args = append(args, "--since", since)
	}
	if serviceName != "" {
		args = append(args, serviceName)
	}
	tty := true
	if request.TTY != nil {
		tty = *request.TTY
	}
	return service.Compose(ctx, DockerRequest{Args: args, Stdout: request.Stdout, Stderr: request.Stderr, TTY: tty})
}
