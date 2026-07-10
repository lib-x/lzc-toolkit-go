package debugbridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/remote"
)

func (client *Client) Install(ctx context.Context, input remote.InstallRequest) error {
	if err := client.validate(ctx, "remote.debugbridge.install"); err != nil {
		return err
	}
	if input.Package == nil {
		return bridgeError(lpkgo.CodeInvalidArgument, "remote.debugbridge.install", errors.New("package reader is required"))
	}
	args, err := client.uidArgs("install")
	if err != nil {
		return err
	}
	if packageID := strings.TrimSpace(input.PackageID); packageID != "" {
		if !validIdentifier(packageID) {
			return bridgeError(lpkgo.CodeInvalidArgument, "remote.debugbridge.install", errors.New("invalid package ID"))
		}
		args = append(args, "--pkgId", packageID)
	}
	command := client.command(false, args...)
	command.Stdin = input.Package
	_, err = client.runner.Run(ctx, command)
	return client.commandError(ctx, "remote.debugbridge.install", err)
}

func (client *Client) Status(ctx context.Context, appID string) (string, error) {
	result, err := client.runUID(ctx, "remote.debugbridge.status", "status", appID)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(result.Stdout)), nil
}

func (client *Client) AppInfo(ctx context.Context, appID string) (remote.AppInfo, error) {
	result, err := client.runUID(ctx, "remote.debugbridge.app_info", "info", appID)
	if err != nil {
		return remote.AppInfo{}, err
	}
	var info remote.AppInfo
	if json.Unmarshal(result.Stdout, &info) != nil || info.AppID != strings.TrimSpace(appID) || !validIdentifier(info.AppID) {
		return remote.AppInfo{}, invalidRemoteData("remote.debugbridge.app_info")
	}
	info.Raw = append(json.RawMessage(nil), result.Stdout...)
	return info, nil
}

func (client *Client) SyncDevID(ctx context.Context, input remote.SyncDevIDRequest) error {
	if !validIdentifier(strings.TrimSpace(input.AppID)) {
		return bridgeError(lpkgo.CodeInvalidArgument, "remote.debugbridge.sync_dev_id", errors.New("invalid app ID"))
	}
	args, err := client.uidArgs("sync-dev-id")
	if err != nil {
		return err
	}
	if devID := strings.TrimSpace(input.DevID); devID != "" {
		args = append(args, "--dev-id", devID)
	}
	if input.UserApp {
		args = append(args, "--userapp")
	}
	args = append(args, strings.TrimSpace(input.AppID))
	_, err = client.runner.Run(ctx, client.command(false, args...))
	return client.commandError(ctx, "remote.debugbridge.sync_dev_id", err)
}

func (client *Client) IsDevshell(ctx context.Context, appID string) (bool, error) {
	result, err := client.runUID(ctx, "remote.debugbridge.is_devshell", "isDevshellV2", appID)
	if err != nil {
		return false, err
	}
	value := strings.TrimSpace(string(result.Stdout))
	if value != "true" && value != "false" {
		return false, invalidRemoteData("remote.debugbridge.is_devshell")
	}
	return value == "true", nil
}

func (client *Client) Pause(ctx context.Context, appID string) error {
	_, err := client.runUID(ctx, "remote.debugbridge.pause", "pause", appID)
	return err
}

func (client *Client) Resume(ctx context.Context, appID string) error {
	_, err := client.runUID(ctx, "remote.debugbridge.resume", "resume", appID)
	return err
}

func (client *Client) Uninstall(ctx context.Context, appID string, deleteData bool) error {
	if !validIdentifier(strings.TrimSpace(appID)) {
		return bridgeError(lpkgo.CodeInvalidArgument, "remote.debugbridge.uninstall", errors.New("invalid app ID"))
	}
	args, err := client.uidArgs("uninstall")
	if err != nil {
		return err
	}
	if deleteData {
		args = append(args, "--delete-data")
	}
	args = append(args, strings.TrimSpace(appID))
	_, err = client.runner.Run(ctx, client.command(false, args...))
	return client.commandError(ctx, "remote.debugbridge.uninstall", err)
}

func (client *Client) HostReadFile(ctx context.Context, pathname string, destination io.Writer) error {
	if err := client.validate(ctx, "remote.debugbridge.host_read_file"); err != nil {
		return err
	}
	pathname = strings.TrimSpace(pathname)
	if destination == nil || pathname == "" || !path.IsAbs(pathname) || path.Clean(pathname) != pathname || strings.ContainsRune(pathname, '\x00') {
		return bridgeError(lpkgo.CodeInvalidArgument, "remote.debugbridge.host_read_file", errors.New("invalid host path or destination"))
	}
	if client.hostCommand == nil {
		return bridgeError(lpkgo.CodeIncompatibleBackend, "remote.debugbridge.host_read_file", errors.New("host command transport is unavailable"))
	}
	command := client.hostCommand(false, "cat", pathname)
	command.Stdout = destination
	_, err := client.runner.Run(ctx, command)
	return client.commandError(ctx, "remote.debugbridge.host_read_file", err)
}

func (client *Client) runUID(ctx context.Context, op, commandName, appID string) (remote.Result, error) {
	if err := client.validate(ctx, op); err != nil {
		return remote.Result{}, err
	}
	appID = strings.TrimSpace(appID)
	if !validIdentifier(appID) {
		return remote.Result{}, bridgeError(lpkgo.CodeInvalidArgument, op, errors.New("invalid app ID"))
	}
	args, err := client.uidArgs(commandName)
	if err != nil {
		return remote.Result{}, err
	}
	args = append(args, appID)
	result, err := client.runner.Run(ctx, client.command(false, args...))
	return result, client.commandError(ctx, op, err)
}

func (client *Client) uidArgs(commandName string) ([]string, error) {
	uid := strings.TrimSpace(client.uid)
	if uid == "" {
		return nil, bridgeError(lpkgo.CodeInvalidConfig, "remote.debugbridge.uid", errors.New("backend UID is required"))
	}
	return []string{commandName, "--uid", uid}, nil
}

func validIdentifier(value string) bool {
	if value == "" || strings.ContainsAny(value, " \t\r\n\x00") {
		return false
	}
	for _, current := range value {
		if (current >= 'a' && current <= 'z') || (current >= 'A' && current <= 'Z') || (current >= '0' && current <= '9') || current == '.' || current == '_' || current == '-' {
			continue
		}
		return false
	}
	return true
}

func (client *Client) commandError(ctx context.Context, op string, err error) error {
	if err == nil {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return bridgeError(lpkgo.CodeCancelled, op, ctx.Err())
	}
	var typed *lpkgo.Error
	if errors.As(err, &typed) {
		return err
	}
	return bridgeError(lpkgo.CodeCommandFailed, op, errors.New("DebugBridge command failed"))
}
