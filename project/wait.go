package project

import (
	"context"
	"errors"
	"strings"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

func (service *Service) Wait(ctx context.Context, request WaitRequest) (Info, error) {
	if err := service.validate(ctx, "project.wait", request.AppID); err != nil {
		return Info{}, err
	}
	if !validWaitState(request.State) || request.Timeout < 0 {
		return Info{}, projectError(lpkgo.CodeInvalidArgument, "project.wait", errors.New("invalid wait request"))
	}
	timeout := request.Timeout
	if timeout == 0 {
		if request.State == WaitNotRunning {
			timeout = service.stopTimeout
		} else {
			timeout = service.startTimeout
		}
	}
	deadline := time.Now().Add(timeout)
	for {
		info, err := service.Info(ctx, InfoRequest{AppID: request.AppID, LocalVersion: request.LocalVersion})
		if err != nil {
			return Info{}, err
		}
		if terminalStartupState(info) && request.State != WaitNotRunning {
			return Info{}, projectError(lpkgo.CodeCommandFailed, "project.wait", errors.New("project entered a terminal startup state"))
		}
		matched, err := service.matches(ctx, request.State, info)
		if err != nil {
			return Info{}, err
		}
		if matched {
			return info, nil
		}
		if !time.Now().Before(deadline) {
			return Info{}, projectError(lpkgo.CodeDeadlineExceeded, "project.wait", context.DeadlineExceeded)
		}
		if err := sleepContext(ctx, service.pollInterval); err != nil {
			return Info{}, projectError(lpkgo.CodeCancelled, "project.wait", err)
		}
	}
}

func (service *Service) matches(ctx context.Context, state WaitState, info Info) (bool, error) {
	switch state {
	case WaitRunning:
		return info.Running, nil
	case WaitNotRunning:
		return !info.Running, nil
	case WaitStarting:
		return info.Running || strings.Contains(strings.ToLower(info.AppStatus), "starting") || strings.Contains(strings.ToLower(info.InstanceStatus), "starting"), nil
	case WaitDevshell:
		return service.backend.IsDevshell(ctx, info.AppID)
	default:
		return false, projectError(lpkgo.CodeInvalidArgument, "project.wait", errors.New("invalid wait state"))
	}
}

func terminalStartupState(info Info) bool {
	appStatus := strings.ToLower(strings.TrimSpace(info.AppStatus))
	instanceStatus := strings.ToLower(strings.TrimSpace(info.InstanceStatus))
	return appStatus == "failed" || appStatus == "paused" || strings.Contains(instanceStatus, "error") || strings.Contains(instanceStatus, "paused")
}

func validWaitState(state WaitState) bool {
	return state == WaitRunning || state == WaitNotRunning || state == WaitStarting || state == WaitDevshell
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
