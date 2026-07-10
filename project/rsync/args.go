package rsync

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
)

const ignoreFileName = ".lzcdevignore"

func BuildArgs(options Options) ([]string, error) {
	root, sourceDir, target, err := normalize(options)
	if err != nil {
		return nil, err
	}
	args := []string{"--recursive", "--links", "--times", "--perms", "--omit-dir-times", "--human-readable", "--itemize-changes", "--compress"}
	if options.Debug {
		args = append(args, "-P")
	}
	if options.Delete {
		args = append(args, "--delete")
	}
	if options.DryRun {
		args = append(args, "--dry-run")
	}
	ignorePath := filepath.Join(root, ignoreFileName)
	if info, statErr := os.Stat(ignorePath); statErr == nil && info.Mode().IsRegular() {
		args = append(args, "--exclude-from="+ignorePath)
	}
	if sourceDir != "" {
		args = append(args, "--relative")
	}
	modulePath, err := modulePath(target)
	if err != nil {
		return nil, err
	}
	source := "./"
	if sourceDir != "" {
		source = "./" + sourceDir + "/"
	}
	destination := fmt.Sprintf("rsync://%s@%s:%d/lzcapp_cache/%s/", target.UID, formatHost(target.Host), target.Port, modulePath)
	return append(args, source, destination), nil
}

func BuildTunnelArgs(options TunnelOptions) ([]string, error) {
	if len(options.SSHArgs) == 0 || options.LocalPort < 1 || options.LocalPort > 65535 {
		return nil, rsyncError(lpkgo.CodeInvalidArgument, "project.rsync.tunnel_args", errors.New("invalid SSH args or local port"))
	}
	targetPort := options.TargetPort
	if targetPort == 0 {
		targetPort = DefaultPort
	}
	if targetPort < 1 || targetPort > 65535 {
		return nil, rsyncError(lpkgo.CodeInvalidArgument, "project.rsync.tunnel_args", errors.New("invalid target port"))
	}
	host := strings.TrimSpace(options.TargetHost)
	if !validHost(host) {
		return nil, rsyncError(lpkgo.CodeInvalidArgument, "project.rsync.tunnel_args", errors.New("invalid tunnel target host"))
	}
	sshTarget := strings.TrimSpace(options.SSHArgs[len(options.SSHArgs)-1])
	if sshTarget == "" || strings.HasPrefix(sshTarget, "-") || strings.ContainsAny(sshTarget, "\r\n\x00") {
		return nil, rsyncError(lpkgo.CodeInvalidArgument, "project.rsync.tunnel_args", errors.New("invalid SSH target"))
	}
	args := append([]string(nil), options.SSHArgs[:len(options.SSHArgs)-1]...)
	forward := "127.0.0.1:" + strconv.Itoa(options.LocalPort) + ":" + formatHost(host) + ":" + strconv.Itoa(targetPort)
	args = append(args, "-o", "ExitOnForwardFailure=yes", "-L", forward, sshTarget, "-N")
	return args, nil
}

func normalize(options Options) (string, string, Target, error) {
	rootValue := strings.TrimSpace(options.RootDir)
	if rootValue == "" {
		return "", "", Target{}, rsyncError(lpkgo.CodeInvalidArgument, "project.rsync.args", errors.New("root directory is required"))
	}
	root, err := filepath.Abs(rootValue)
	if err != nil {
		return "", "", Target{}, rsyncError(lpkgo.CodeInvalidArgument, "project.rsync.args", errors.New("invalid root directory"))
	}
	sourceDir := strings.ReplaceAll(strings.TrimSpace(options.SourceDir), "\\", "/")
	sourceDir = strings.TrimPrefix(sourceDir, "./")
	sourceDir = strings.TrimSuffix(sourceDir, "/")
	if sourceDir != "" && (!safeRelativePath(sourceDir) || filepath.Clean(filepath.Join(root, filepath.FromSlash(sourceDir))) == root) {
		return "", "", Target{}, rsyncError(lpkgo.CodeInvalidArgument, "project.rsync.args", errors.New("invalid source directory"))
	}
	target := options.Target
	target.UID = strings.TrimSpace(target.UID)
	target.Host = strings.TrimSpace(target.Host)
	target.PackageID = strings.TrimSpace(target.PackageID)
	target.Directory = strings.TrimSpace(strings.ReplaceAll(target.Directory, "\\", "/"))
	if target.Port == 0 {
		target.Port = DefaultPort
	}
	if target.Directory == "" {
		target.Directory = DefaultTarget
	}
	if !safeComponent(target.UID) || !safeComponent(target.PackageID) || !validHost(target.Host) || target.Port < 1 || target.Port > 65535 {
		return "", "", Target{}, rsyncError(lpkgo.CodeInvalidArgument, "project.rsync.args", errors.New("invalid rsync target"))
	}
	if _, err := cacheSubpath(target.Directory); err != nil {
		return "", "", Target{}, err
	}
	return root, sourceDir, target, nil
}

func modulePath(target Target) (string, error) {
	result := target.PackageID
	if target.UserApp {
		result += "/" + target.UID
	}
	subpath, err := cacheSubpath(target.Directory)
	if err != nil {
		return "", err
	}
	if subpath != "" {
		result += "/" + subpath
	}
	return result, nil
}

func cacheSubpath(target string) (string, error) {
	target = strings.TrimSuffix(strings.TrimSpace(strings.ReplaceAll(target, "\\", "/")), "/")
	if target == "/lzcapp/cache" {
		return "", nil
	}
	if !strings.HasPrefix(target, "/lzcapp/cache/") || path.Clean(target) != target {
		return "", rsyncError(lpkgo.CodeInvalidArgument, "project.rsync.target", errors.New("sync target must stay under /lzcapp/cache"))
	}
	return strings.TrimPrefix(target, "/lzcapp/cache/"), nil
}

func safeRelativePath(value string) bool {
	if value == "" || strings.ContainsAny(value, ":\r\n\x00") || path.IsAbs(value) || path.Clean(value) != value {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." || part == "." || part == "" {
			return false
		}
	}
	return true
}

func safeComponent(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/\\:@ \t\r\n\x00")
}

func validHost(value string) bool {
	if value == "" || strings.ContainsAny(value, " /\\@\t\r\n\x00") {
		return false
	}
	if strings.Contains(value, ":") {
		return net.ParseIP(strings.Trim(value, "[]")) != nil
	}
	return true
}

func formatHost(value string) string {
	value = strings.Trim(value, "[]")
	if strings.Contains(value, ":") {
		return "[" + value + "]"
	}
	return value
}

func rsyncError(code lpkgo.Code, op string, cause error) error {
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}
