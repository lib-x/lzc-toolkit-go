package project

import (
	"context"
	"errors"
	"strings"
	"time"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/remote"
)

func (service *Service) Deploy(ctx context.Context, request DeployRequest) (DeployResult, error) {
	packageID := strings.TrimSpace(request.PackageID)
	if err := service.validate(ctx, "project.deploy", packageID); err != nil {
		return DeployResult{}, err
	}
	if request.Package == nil {
		return DeployResult{}, projectError(lpkgo.CodeInvalidArgument, "project.deploy", errors.New("package reader is required"))
	}
	backendInfo, err := service.backend.Info(ctx)
	if err != nil {
		return DeployResult{}, err
	}
	if err := remote.Require(remote.CapabilityLPKV2, backendInfo.Version); err != nil {
		return DeployResult{}, err
	}
	pendingSync, err := remote.Supports(remote.CapabilityPendingSyncDevID, backendInfo.Version)
	if err != nil {
		return DeployResult{}, err
	}
	if err := service.backend.Install(ctx, remote.InstallRequest{Package: request.Package, PackageID: packageID}); err != nil {
		return DeployResult{}, err
	}
	result := DeployResult{Backend: backendInfo}
	if !pendingSync {
		if _, err := service.waitDevshell(ctx, packageID, service.startTimeout); err != nil {
			return DeployResult{}, err
		}
		result.WaitedForContainer = true
	}
	if err := service.backend.SyncDevID(ctx, remote.SyncDevIDRequest{
		AppID: packageID, DevID: strings.TrimSpace(request.DevID), UserApp: request.UserApp,
	}); err != nil {
		return DeployResult{}, err
	}
	result.SyncedDevID = true
	result.Info, err = service.Info(ctx, InfoRequest{AppID: packageID})
	if err != nil {
		return DeployResult{}, err
	}
	return result, nil
}

func (service *Service) waitDevshell(ctx context.Context, appID string, timeout time.Duration) (Info, error) {
	return service.Wait(ctx, WaitRequest{AppID: appID, State: WaitDevshell, Timeout: timeout})
}
