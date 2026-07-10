package rsync

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
)

const rsyncPassword = "fakefakefake"

var versionPattern = regexp.MustCompile(`(?i)rsync\s+version\s+(\d+)\.(\d+)\.(\d+)`)

var defaultIgnoreRules = []string{
	".git", ".git/**", "node_modules", "node_modules/**", ".venv", ".venv/**",
	"dist", "dist/**", "build", "build/**", "__pycache__", "__pycache__/**",
	".idea", ".idea/**", ".vscode", ".vscode/**", ".DS_Store",
	".lzc-cli-*", ".lzc-cli-*/**", "*.lpk", "*.lpk.tar",
}

func Sync(ctx context.Context, options Options) (Result, error) {
	if ctx == nil {
		return Result{}, rsyncError(lpkgo.CodeInvalidArgument, "project.rsync.sync", errors.New("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return Result{}, rsyncError(lpkgo.CodeCancelled, "project.rsync.sync", err)
	}
	root, sourceDir, _, err := normalize(options)
	if err != nil {
		return Result{}, err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return Result{}, rsyncError(lpkgo.CodeNotFound, "project.rsync.sync", errors.New("root directory not found"))
	}
	if sourceDir != "" {
		sourcePath := filepath.Join(root, filepath.FromSlash(sourceDir))
		info, err := os.Stat(sourcePath)
		if err != nil || !info.IsDir() {
			return Result{}, rsyncError(lpkgo.CodeNotFound, "project.rsync.sync", errors.New("source directory not found"))
		}
	}
	ensureIgnore := true
	if options.EnsureIgnoreFile != nil {
		ensureIgnore = *options.EnsureIgnoreFile
	}
	if ensureIgnore {
		if _, err := EnsureIgnoreFile(root); err != nil {
			return Result{}, err
		}
	}
	args, err := BuildArgs(options)
	if err != nil {
		return Result{}, err
	}
	binary := strings.TrimSpace(options.Binary)
	if binary == "" {
		binary = "rsync"
	}
	executor := options.Executor
	if executor == nil {
		executor = commandExecutor{}
	}
	version, err := validateVersion(ctx, executor, binary)
	if err != nil {
		return Result{}, err
	}
	detector := &changeDetector{destination: options.Stdout}
	stderr := options.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	process := Process{
		Name: binary, Args: append([]string(nil), args...), Dir: root,
		Env: withPassword(os.Environ()), Stdout: detector, Stderr: stderr,
	}
	if err := executor.Run(ctx, process); err != nil {
		if ctx.Err() != nil {
			return Result{}, rsyncError(lpkgo.CodeCancelled, "project.rsync.sync", ctx.Err())
		}
		return Result{}, rsyncError(lpkgo.CodeCommandFailed, "project.rsync.sync", errors.New("rsync command failed"))
	}
	detector.flush()
	return Result{Version: version, Changed: detector.changed, Source: args[len(args)-2], Destination: args[len(args)-1]}, nil
}

func EnsureIgnoreFile(rootDir string) (bool, error) {
	root, err := filepath.Abs(strings.TrimSpace(rootDir))
	if err != nil || strings.TrimSpace(rootDir) == "" {
		return false, rsyncError(lpkgo.CodeInvalidArgument, "project.rsync.ignore", errors.New("invalid root directory"))
	}
	ignorePath := filepath.Join(root, ignoreFileName)
	if _, err := os.Stat(ignorePath); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, rsyncError(lpkgo.CodeCommandFailed, "project.rsync.ignore", errors.New("cannot inspect ignore file"))
	}
	rules := append([]string(nil), defaultIgnoreRules...)
	gitignore, err := readIgnoreLines(filepath.Join(root, ".gitignore"))
	if err != nil {
		return false, err
	}
	rules = append(rules, gitignore...)
	seen := make(map[string]struct{}, len(rules))
	unique := make([]string, 0, len(rules))
	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}
		if _, exists := seen[rule]; exists {
			continue
		}
		seen[rule] = struct{}{}
		unique = append(unique, rule)
	}
	file, err := os.OpenFile(ignorePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, rsyncError(lpkgo.CodeCommandFailed, "project.rsync.ignore", errors.New("cannot create ignore file"))
	}
	_, writeErr := io.WriteString(file, strings.Join(unique, "\n")+"\n")
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(ignorePath)
		return false, rsyncError(lpkgo.CodeCommandFailed, "project.rsync.ignore", errors.New("cannot write ignore file"))
	}
	return true, nil
}

func readIgnoreLines(filename string) ([]string, error) {
	data, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, rsyncError(lpkgo.CodeCommandFailed, "project.rsync.ignore", errors.New("cannot read gitignore"))
	}
	result := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			result = append(result, line)
		}
	}
	return result, nil
}

func validateVersion(ctx context.Context, executor Executor, binary string) (string, error) {
	var output limitedBuffer
	output.remaining = 64 << 10
	err := executor.Run(ctx, Process{Name: binary, Args: []string{"--version"}, Stdout: &output, Stderr: io.Discard})
	if err != nil {
		if ctx.Err() != nil {
			return "", rsyncError(lpkgo.CodeCancelled, "project.rsync.version", ctx.Err())
		}
		return "", rsyncError(lpkgo.CodeCommandFailed, "project.rsync.version", errors.New("rsync version check failed"))
	}
	matches := versionPattern.FindSubmatch(output.Bytes())
	if len(matches) != 4 {
		return "", rsyncError(lpkgo.CodeIncompatibleBackend, "project.rsync.version", errors.New("invalid rsync version output"))
	}
	parts := make([]int, 3)
	for index := range parts {
		parts[index], _ = strconv.Atoi(string(matches[index+1]))
	}
	if parts[0] < 3 || parts[0] == 3 && parts[1] < 2 {
		return "", rsyncError(lpkgo.CodeIncompatibleBackend, "project.rsync.version", errors.New("rsync 3.2.0 or newer is required"))
	}
	return strings.Join([]string{string(matches[1]), string(matches[2]), string(matches[3])}, "."), nil
}

func withPassword(environment []string) []string {
	result := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		key, _, found := strings.Cut(item, "=")
		if found && strings.EqualFold(key, "RSYNC_PASSWORD") {
			continue
		}
		result = append(result, item)
	}
	return append(result, "RSYNC_PASSWORD="+rsyncPassword)
}

type commandExecutor struct{}

func (commandExecutor) Run(ctx context.Context, process Process) error {
	command := exec.CommandContext(ctx, process.Name, process.Args...)
	command.Dir = process.Dir
	command.Env = process.Env
	command.Stdin = process.Stdin
	command.Stdout = process.Stdout
	command.Stderr = process.Stderr
	return command.Run()
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	remaining int64
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	if buffer.remaining > 0 {
		keep := min(int64(len(data)), buffer.remaining)
		_, _ = buffer.buffer.Write(data[:keep])
		buffer.remaining -= keep
	}
	return len(data), nil
}

func (buffer *limitedBuffer) Bytes() []byte { return buffer.buffer.Bytes() }

type changeDetector struct {
	destination io.Writer
	buffer      strings.Builder
	changed     bool
}

func (detector *changeDetector) Write(data []byte) (int, error) {
	if detector.destination != nil {
		written, err := detector.destination.Write(data)
		if err != nil {
			return written, err
		}
		if written != len(data) {
			return written, io.ErrShortWrite
		}
	}
	detector.buffer.Write(data)
	detector.consume(false)
	return len(data), nil
}

func (detector *changeDetector) flush() { detector.consume(true) }

func (detector *changeDetector) consume(force bool) {
	text := detector.buffer.String()
	lines := strings.Split(text, "\n")
	if !force {
		detector.buffer.Reset()
		detector.buffer.WriteString(lines[len(lines)-1])
		lines = lines[:len(lines)-1]
	} else {
		detector.buffer.Reset()
	}
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || line == "sending incremental file list" || strings.HasPrefix(line, "sent ") || strings.HasPrefix(line, "total size is ") {
			continue
		}
		detector.changed = true
	}
}
