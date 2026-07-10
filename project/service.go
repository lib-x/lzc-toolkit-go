package project

import (
	"context"
	"errors"
	"strings"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/remote"
)

type Service struct {
	backend         remote.LifecycleBackend
	pollInterval    time.Duration
	startTimeout    time.Duration
	stopTimeout     time.Duration
	maxCaptureBytes int64
}

func New(options Options) (*Service, error) {
	if options.Backend == nil || options.PollInterval < 0 || options.StartTimeout < 0 || options.StopTimeout < 0 || options.MaxCaptureBytes < 0 {
		return nil, projectError(lpkgo.CodeInvalidArgument, "project.new", errors.New("invalid project service options"))
	}
	pollInterval := options.PollInterval
	if pollInterval == 0 {
		pollInterval = defaultPollInterval
	}
	startTimeout := options.StartTimeout
	if startTimeout == 0 {
		startTimeout = defaultStartTimeout
	}
	stopTimeout := options.StopTimeout
	if stopTimeout == 0 {
		stopTimeout = defaultStopTimeout
	}
	maxCaptureBytes := options.MaxCaptureBytes
	if maxCaptureBytes == 0 {
		maxCaptureBytes = defaultMaxCaptureBytes
	}
	return &Service{backend: options.Backend, pollInterval: pollInterval, startTimeout: startTimeout, stopTimeout: stopTimeout, maxCaptureBytes: maxCaptureBytes}, nil
}

func (service *Service) Info(ctx context.Context, request InfoRequest) (Info, error) {
	appID := strings.TrimSpace(request.AppID)
	if err := service.validate(ctx, "project.info", appID); err != nil {
		return Info{}, err
	}
	remoteInfo, err := service.backend.AppInfo(ctx, appID)
	if err != nil {
		return Info{}, err
	}
	if strings.TrimSpace(remoteInfo.AppID) != appID {
		return Info{}, projectError(lpkgo.CodeRemoteUnavailable, "project.info", errors.New("backend returned mismatched app info"))
	}
	appStatus := strings.TrimSpace(remoteInfo.Status)
	instanceStatus := strings.TrimSpace(remoteInfo.InstanceStatus)
	deployedVersion := strings.TrimSpace(remoteInfo.Version)
	localVersion := strings.TrimSpace(request.LocalVersion)
	deployed := appStatus != "" && appStatus != "NotInstalled"
	return Info{
		AppID: remoteInfo.AppID, DeployID: strings.TrimSpace(remoteInfo.DeployID),
		LocalVersion: localVersion, DeployedVersion: deployedVersion,
		Domain: strings.TrimSpace(remoteInfo.Domain), AppStatus: appStatus,
		InstanceStatus: instanceStatus, ErrorReason: strings.TrimSpace(remoteInfo.ErrorReason),
		Deployed: deployed, CurrentVersionDeployed: deployed && localVersion != "" && localVersion == deployedVersion,
		Running: instanceStatus == "Status_Running", Raw: append([]byte(nil), remoteInfo.Raw...),
	}, nil
}

func (service *Service) validate(ctx context.Context, op, appID string) error {
	if err := service.validateService(ctx, op); err != nil {
		return err
	}
	if !validIdentifier(strings.TrimSpace(appID)) {
		return projectError(lpkgo.CodeInvalidArgument, op, errors.New("invalid app ID"))
	}
	return nil
}

func (service *Service) validateService(ctx context.Context, op string) error {
	if ctx == nil || service == nil || service.backend == nil {
		return projectError(lpkgo.CodeInvalidArgument, op, errors.New("nil context, service, or backend"))
	}
	if err := ctx.Err(); err != nil {
		return projectError(lpkgo.CodeCancelled, op, err)
	}
	return nil
}

func validIdentifier(value string) bool {
	if value == "" || strings.ContainsAny(value, " \t\r\n\x00") {
		return false
	}
	for _, current := range value {
		if current >= 'a' && current <= 'z' || current >= 'A' && current <= 'Z' || current >= '0' && current <= '9' || current == '.' || current == '_' || current == '-' {
			continue
		}
		return false
	}
	return true
}

func projectError(code lpkgo.Code, op string, cause error) error {
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}
