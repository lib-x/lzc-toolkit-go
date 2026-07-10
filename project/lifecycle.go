package project

import (
	"context"
	"strings"
)

func (service *Service) Start(ctx context.Context, request StartRequest) (Info, error) {
	appID := strings.TrimSpace(request.AppID)
	info, err := service.Info(ctx, InfoRequest{AppID: appID, LocalVersion: request.LocalVersion})
	if err != nil {
		return Info{}, err
	}
	if request.Restart && info.Running {
		if err := service.backend.Pause(ctx, appID); err != nil {
			return Info{}, err
		}
		if _, err := service.Wait(ctx, WaitRequest{AppID: appID, LocalVersion: request.LocalVersion, State: WaitNotRunning}); err != nil {
			return Info{}, err
		}
		info.Running = false
	}
	if !info.Running {
		if err := service.backend.Resume(ctx, appID); err != nil {
			return Info{}, err
		}
	}
	return service.Wait(ctx, WaitRequest{AppID: appID, LocalVersion: request.LocalVersion, State: WaitRunning})
}

func (service *Service) Stop(ctx context.Context, request StopRequest) (Info, error) {
	appID := strings.TrimSpace(request.AppID)
	info, err := service.Info(ctx, InfoRequest{AppID: appID, LocalVersion: request.LocalVersion})
	if err != nil {
		return Info{}, err
	}
	if !info.Running {
		return info, nil
	}
	if err := service.backend.Pause(ctx, appID); err != nil {
		return Info{}, err
	}
	return service.Wait(ctx, WaitRequest{AppID: appID, LocalVersion: request.LocalVersion, State: WaitNotRunning})
}

func (service *Service) Uninstall(ctx context.Context, request UninstallRequest) error {
	appID := strings.TrimSpace(request.AppID)
	if err := service.validate(ctx, "project.uninstall", appID); err != nil {
		return err
	}
	return service.backend.Uninstall(ctx, appID, request.DeleteData)
}
