package project

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
)

func (service *Service) ComposeProjects(ctx context.Context, appID string) ([]ComposeProject, error) {
	if err := service.validate(ctx, "project.compose_projects", appID); err != nil {
		return nil, err
	}
	result, err := service.Compose(ctx, DockerRequest{Args: []string{"ls", "--format", "json"}})
	if err != nil {
		return nil, err
	}
	if int64(len(result.Stdout)) > service.maxCaptureBytes {
		return nil, projectError(lpkgo.CodeRemoteUnavailable, "project.compose_projects", errors.New("Compose project response exceeds limit"))
	}
	var projects []ComposeProject
	if len(strings.TrimSpace(string(result.Stdout))) == 0 {
		return []ComposeProject{}, nil
	}
	if err := json.Unmarshal(result.Stdout, &projects); err != nil {
		return nil, projectError(lpkgo.CodeRemoteUnavailable, "project.compose_projects", errors.New("invalid Compose project response"))
	}
	filtered := make([]ComposeProject, 0, len(projects))
	for _, item := range projects {
		item.Name = strings.TrimSpace(item.Name)
		item.Status = strings.TrimSpace(item.Status)
		item.ConfigFiles = strings.TrimSpace(item.ConfigFiles)
		if item.Name != "" {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (service *Service) ensureServiceRunning(ctx context.Context, appID, serviceName string) (string, error) {
	composeName, err := ComposeProjectName(appID)
	if err != nil {
		return "", err
	}
	serviceName = normalizedService(serviceName)
	if !validIdentifier(serviceName) {
		return "", projectError(lpkgo.CodeInvalidArgument, "project.service", errors.New("invalid service name"))
	}
	projects, err := service.ComposeProjects(ctx, appID)
	if err != nil {
		return "", err
	}
	running := false
	for _, item := range projects {
		if item.Name == composeName && strings.HasPrefix(item.Status, "running(") {
			running = true
			break
		}
	}
	if !running {
		return "", projectError(lpkgo.CodeConflict, "project.service", errors.New("project app is not running"))
	}
	result, err := service.Compose(ctx, DockerRequest{Args: []string{"-p", composeName, "ps", "--status", "running", "-q", serviceName}})
	if err != nil {
		return "", err
	}
	if int64(len(result.Stdout)) > service.maxCaptureBytes {
		return "", projectError(lpkgo.CodeRemoteUnavailable, "project.service", errors.New("service response exceeds limit"))
	}
	containerID := firstNonEmptyLine(result.Stdout)
	if !validIdentifier(containerID) {
		return "", projectError(lpkgo.CodeConflict, "project.service", errors.New("project service is not running"))
	}
	return containerID, nil
}

func (service *Service) ensureComposeProjectRunning(ctx context.Context, appID string) error {
	composeName, err := ComposeProjectName(appID)
	if err != nil {
		return err
	}
	projects, err := service.ComposeProjects(ctx, appID)
	if err != nil {
		return err
	}
	for _, item := range projects {
		if item.Name == composeName && strings.HasPrefix(item.Status, "running(") {
			return nil
		}
	}
	return projectError(lpkgo.CodeConflict, "project.compose_project", errors.New("project app is not running"))
}

func normalizedService(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "app"
	}
	return value
}

func firstNonEmptyLine(data []byte) string {
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if value := strings.TrimSpace(line); value != "" {
			return value
		}
	}
	return ""
}
