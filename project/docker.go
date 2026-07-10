package project

import (
	"context"
	"errors"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/remote"
)

func (service *Service) Docker(ctx context.Context, request DockerRequest) (remote.Result, error) {
	if err := service.validateService(ctx, "project.docker"); err != nil {
		return remote.Result{}, err
	}
	if err := validateCommandArgs(request.Args); err != nil {
		return remote.Result{}, err
	}
	return service.backend.Docker(ctx, request)
}

func (service *Service) Compose(ctx context.Context, request DockerRequest) (remote.Result, error) {
	if err := service.validateService(ctx, "project.compose"); err != nil {
		return remote.Result{}, err
	}
	if err := validateCommandArgs(request.Args); err != nil {
		return remote.Result{}, err
	}
	return service.backend.DockerCompose(ctx, request)
}

func ComposeProjectName(appID string) (string, error) {
	appID = strings.TrimSpace(appID)
	if !validIdentifier(appID) {
		return "", projectError(lpkgo.CodeInvalidArgument, "project.compose_name", errors.New("invalid app ID"))
	}
	name := strings.ReplaceAll(appID, ".", "")
	if name == "" {
		return "", projectError(lpkgo.CodeInvalidArgument, "project.compose_name", errors.New("empty Compose project name"))
	}
	return name, nil
}

func validateCommandArgs(args []string) error {
	if len(args) == 0 {
		return projectError(lpkgo.CodeInvalidArgument, "project.command", errors.New("command arguments are required"))
	}
	for _, argument := range args {
		if strings.ContainsAny(argument, "\r\n\x00") {
			return projectError(lpkgo.CodeInvalidArgument, "project.command", errors.New("invalid command argument"))
		}
	}
	return nil
}
