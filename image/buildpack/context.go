package buildpack

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	lpkgo "github.com/lib-x/lpk-go"
	imagebuild "github.com/lib-x/lpk-go/image"
	"github.com/lib-x/lpk-go/oci"
)

func writeContextTar(ctx context.Context, destination string, entry imagebuild.Entry, limit int64) (oci.Digest, error) {
	if err := contextError(ctx, "buildpack.context"); err != nil {
		return "", err
	}
	dockerfile, err := readDockerfile(entry)
	if err != nil {
		return "", err
	}
	paths, err := collectContextPaths(ctx, entry.ContextDir, dockerfile)
	if err != nil {
		return "", err
	}
	file, err := os.Create(destination)
	if err != nil {
		return "", packError(lpkgo.CodeCommandFailed, "buildpack.context", err)
	}
	hash := sha256.New()
	counted := &limitWriter{writer: io.MultiWriter(file, hash), limit: limit}
	writer := tar.NewWriter(counted)
	writeErr := writeTarBytes(writer, "Dockerfile", dockerfile, 0o644)
	rootDockerfile := filepath.Clean(filepath.Join(entry.ContextDir, "Dockerfile"))
	for _, name := range paths {
		if writeErr != nil {
			break
		}
		absolute := filepath.Join(entry.ContextDir, filepath.FromSlash(name))
		if filepath.Clean(absolute) == rootDockerfile {
			continue
		}
		writeErr = writeTarPath(ctx, writer, absolute, name)
	}
	closeTarErr := writer.Close()
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || closeTarErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(destination)
		return "", packError(lpkgo.CodeCommandFailed, "buildpack.context", errors.Join(writeErr, closeTarErr, syncErr, closeErr))
	}
	digest, _ := oci.ParseDigest(fmt.Sprintf("sha256:%x", hash.Sum(nil)))
	return digest, nil
}

func readDockerfile(entry imagebuild.Entry) ([]byte, error) {
	if entry.DockerfileContent != "" {
		return []byte(entry.DockerfileContent), nil
	}
	data, err := os.ReadFile(entry.DockerfilePath)
	if err != nil {
		return nil, packError(lpkgo.CodeCommandFailed, "buildpack.dockerfile", err)
	}
	return data, nil
}

func collectContextPaths(ctx context.Context, root string, dockerfile []byte) ([]string, error) {
	selectors, err := dockerSourceSelectors(root, dockerfile)
	if err != nil {
		return nil, err
	}
	ignoreRules, err := readDockerignore(root)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]struct{})
	err = filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := contextError(ctx, "buildpack.context"); err != nil {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		slash := filepath.ToSlash(relative)
		if slash == ".lzc-cli-cache" || strings.HasPrefix(slash, ".lzc-cli-cache/") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !matchesDockerSource(slash, entry.IsDir(), selectors) || dockerignored(slash, entry.IsDir(), ignoreRules) {
			return nil
		}
		selected[slash] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, packError(lpkgo.CodeCommandFailed, "buildpack.context", err)
	}
	paths := make([]string, 0, len(selected))
	for name := range selected {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	return paths, nil
}

type sourceSelector struct {
	exact  string
	prefix string
	match  *regexp.Regexp
}

func dockerSourceSelectors(root string, dockerfile []byte) ([]sourceSelector, error) {
	var selectors []sourceSelector
	for _, instruction := range dockerfileInstructions(string(dockerfile)) {
		fields := splitInstruction(instruction)
		if len(fields) < 3 {
			continue
		}
		kind := strings.ToUpper(fields[0])
		if kind != "COPY" && kind != "ADD" {
			continue
		}
		arguments, stageSource, err := dockerInstructionArguments(strings.TrimSpace(instruction[len(fields[0]):]))
		if err != nil {
			return nil, packError(lpkgo.CodeInvalidConfig, "buildpack.dockerfile", err)
		}
		if stageSource || len(arguments) < 2 {
			continue
		}
		for _, source := range arguments[:len(arguments)-1] {
			source = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(source)), "/")
			if source == "" || strings.Contains(source, "://") {
				continue
			}
			clean := path.Clean(source)
			if clean == ".." || strings.HasPrefix(clean, "../") {
				return nil, packError(lpkgo.CodeInvalidConfig, "buildpack.dockerfile", errors.New("Dockerfile source escapes context"))
			}
			if clean == "." {
				selectors = append(selectors, sourceSelector{prefix: ""})
				continue
			}
			if strings.ContainsAny(clean, "*?[") {
				pattern, err := dockerGlobRegexp(clean)
				if err != nil {
					return nil, packError(lpkgo.CodeInvalidConfig, "buildpack.dockerfile", err)
				}
				selectors = append(selectors, sourceSelector{match: pattern})
				continue
			}
			info, err := os.Stat(filepath.Join(root, filepath.FromSlash(clean)))
			if err == nil && info.IsDir() {
				selectors = append(selectors, sourceSelector{exact: clean, prefix: clean + "/"})
			} else {
				selectors = append(selectors, sourceSelector{exact: clean})
			}
		}
	}
	return selectors, nil
}

func dockerfileInstructions(source string) []string {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	var result []string
	var current string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || (current == "" && strings.HasPrefix(trimmed, "#")) {
			continue
		}
		continued := strings.HasSuffix(trimmed, "\\")
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "\\"))
		if current == "" {
			current = trimmed
		} else {
			current += " " + trimmed
		}
		if !continued {
			result = append(result, current)
			current = ""
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func splitInstruction(value string) []string {
	return strings.Fields(value)
}

func dockerInstructionArguments(value string) ([]string, bool, error) {
	value = strings.TrimSpace(value)
	stageSource := false
	for strings.HasPrefix(value, "--") {
		fields := strings.Fields(value)
		if len(fields) == 0 {
			break
		}
		if strings.HasPrefix(fields[0], "--from=") {
			stageSource = true
		}
		value = strings.TrimSpace(strings.TrimPrefix(value, fields[0]))
	}
	if strings.HasPrefix(value, "[") {
		var arguments []string
		if err := json.Unmarshal([]byte(value), &arguments); err != nil {
			return nil, false, errors.New("invalid JSON COPY/ADD arguments")
		}
		return arguments, stageSource, nil
	}
	arguments, err := shellWords(value)
	return arguments, stageSource, err
}

func shellWords(value string) ([]string, error) {
	var result []string
	var word strings.Builder
	quote := rune(0)
	escaped := false
	flush := func() {
		if word.Len() > 0 {
			result = append(result, word.String())
			word.Reset()
		}
	}
	for _, current := range value {
		if escaped {
			word.WriteRune(current)
			escaped = false
			continue
		}
		if current == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if current == quote {
				quote = 0
			} else {
				word.WriteRune(current)
			}
			continue
		}
		if current == '\'' || current == '"' {
			quote = current
			continue
		}
		if current == ' ' || current == '\t' {
			flush()
			continue
		}
		word.WriteRune(current)
	}
	if escaped || quote != 0 {
		return nil, errors.New("unterminated Dockerfile COPY/ADD argument")
	}
	flush()
	return result, nil
}

func dockerGlobRegexp(pattern string) (*regexp.Regexp, error) {
	var expression strings.Builder
	expression.WriteString("^")
	for index := 0; index < len(pattern); index++ {
		current := pattern[index]
		switch current {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				expression.WriteString(".*")
				index++
			} else {
				expression.WriteString("[^/]*")
			}
		case '?':
			expression.WriteString("[^/]")
		default:
			expression.WriteString(regexp.QuoteMeta(string(current)))
		}
	}
	expression.WriteString("$")
	return regexp.Compile(expression.String())
}

func matchesDockerSource(name string, directory bool, selectors []sourceSelector) bool {
	for _, selector := range selectors {
		if selector.prefix == "" && selector.exact == "" && selector.match == nil {
			return true
		}
		if name == selector.exact || (selector.prefix != "" && strings.HasPrefix(name, selector.prefix)) || (selector.match != nil && selector.match.MatchString(name)) {
			return true
		}
		if directory && selector.exact != "" && strings.HasPrefix(selector.exact, name+"/") {
			return true
		}
	}
	return false
}

type ignoreRule struct {
	negate  bool
	pattern string
}

func readDockerignore(root string) ([]ignoreRule, error) {
	data, err := os.ReadFile(filepath.Join(root, ".dockerignore"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, packError(lpkgo.CodeCommandFailed, "buildpack.dockerignore", err)
	}
	var rules []ignoreRule
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule := ignoreRule{}
		if strings.HasPrefix(line, "!") {
			rule.negate = true
			line = strings.TrimSpace(strings.TrimPrefix(line, "!"))
		}
		rule.pattern = strings.TrimPrefix(filepath.ToSlash(line), "/")
		if rule.pattern != "" {
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

func dockerignored(name string, directory bool, rules []ignoreRule) bool {
	ignored := false
	for _, rule := range rules {
		pattern := strings.TrimSuffix(rule.pattern, "/")
		matched := false
		if strings.Contains(pattern, "/") {
			matched, _ = path.Match(pattern, name)
			matched = matched || name == pattern || strings.HasPrefix(name, pattern+"/")
		} else {
			for _, component := range strings.Split(name, "/") {
				if component == pattern {
					matched = true
					break
				}
			}
			if !matched {
				matched, _ = path.Match(pattern, path.Base(name))
			}
		}
		if matched || (directory && strings.HasPrefix(pattern, name+"/")) {
			ignored = !rule.negate
		}
	}
	return ignored
}

func writeTarBytes(writer *tar.Writer, name string, data []byte, mode int64) error {
	header := &tar.Header{Name: name, Mode: mode, Size: int64(len(data)), Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0).UTC()}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	_, err := writer.Write(data)
	return err
}

func writeTarPath(ctx context.Context, writer *tar.Writer, absolute, name string) error {
	info, err := os.Lstat(absolute)
	if err != nil {
		return err
	}
	link := ""
	if info.Mode()&fs.ModeSymlink != 0 {
		link, err = os.Readlink(absolute)
		if err != nil {
			return err
		}
	}
	header, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return err
	}
	header.Name = name
	header.Uid = 0
	header.Gid = 0
	header.Uname = ""
	header.Gname = ""
	header.ModTime = time.Unix(0, 0).UTC()
	header.AccessTime = time.Time{}
	header.ChangeTime = time.Time{}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	file, err := os.Open(absolute)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(writer, &contextFileReader{ctx: ctx, reader: file})
	closeErr := file.Close()
	return errors.Join(copyErr, closeErr)
}

func contextError(ctx context.Context, op string) error {
	if ctx == nil {
		return packError(lpkgo.CodeInvalidArgument, op, errors.New("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return packError(lpkgo.CodeCancelled, op, err)
	}
	return nil
}

type contextFileReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextFileReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
}

type limitWriter struct {
	writer  io.Writer
	written int64
	limit   int64
}

func (writer *limitWriter) Write(data []byte) (int, error) {
	if writer.limit > 0 && writer.written+int64(len(data)) > writer.limit {
		return 0, errors.New("build context exceeds limit")
	}
	written, err := writer.writer.Write(data)
	writer.written += int64(written)
	return written, err
}
