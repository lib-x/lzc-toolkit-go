// Package image contains dependency-light image build configuration shared by
// local and remote adapters.
package image

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/manifest"
	"go.yaml.in/yaml/v3"
)

const DefaultUpstreamMatch = "registry.lazycat.cloud"

type Builder string

const (
	BuilderRemote Builder = "remote"
	BuilderLocal  Builder = "local"
)

type Entry struct {
	Alias             string
	Builder           Builder
	ContextDir        string
	DockerfilePath    string
	DockerfileContent string
	ImageLabel        string
	UpstreamMatch     string
}

var aliasPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Normalize validates and resolves the lzc-cli 2.0.9 images configuration.
func Normalize(ctx context.Context, root string, appManifest manifest.Manifest, raw any) ([]Entry, error) {
	if err := contextError(ctx, "image.normalize"); err != nil {
		return nil, err
	}
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, imageError("image.normalize", root, err)
	}
	data, err := yaml.Marshal(raw)
	if err != nil {
		return nil, imageError("image.normalize", resolvedRoot, err)
	}
	var config map[string]any
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, imageError("image.normalize", resolvedRoot, err)
	}
	if config == nil {
		return nil, imageError("image.normalize", resolvedRoot, errors.New("images config must be an object"))
	}

	aliases := make([]string, 0, len(config))
	for alias := range config {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	packageName := sanitizeTagPart(appManifest.Package, "local-app")
	version := sanitizeTagPart(appManifest.Version, "latest")
	entries := make([]Entry, 0, len(aliases))
	for _, alias := range aliases {
		if err := contextError(ctx, "image.normalize"); err != nil {
			return nil, err
		}
		if !aliasPattern.MatchString(alias) {
			return nil, imageError("image.normalize", resolvedRoot, fmt.Errorf("invalid image alias %q", alias))
		}
		entry, err := normalizeEntry(resolvedRoot, alias, config[alias])
		if err != nil {
			return nil, err
		}
		entry.ImageLabel = packageName + "-image-" + sanitizeTagPart(alias, "image") + ":" + version
		entries = append(entries, entry)
	}

	configured := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		configured[entry.Alias] = struct{}{}
	}
	manifestAliases, err := manifestEmbedAliases(appManifest)
	if err != nil {
		return nil, imageError("image.normalize", resolvedRoot, err)
	}
	for _, alias := range manifestAliases {
		if _, exists := configured[alias]; !exists {
			return nil, imageError("image.normalize", resolvedRoot, fmt.Errorf("manifest embed alias %q is missing from images config", alias))
		}
	}
	return entries, nil
}

func normalizeEntry(root, alias string, raw any) (Entry, error) {
	config := make(map[string]any)
	switch value := raw.(type) {
	case string:
		config["dockerfile"] = value
	case map[string]any:
		config = value
	default:
		data, err := yaml.Marshal(raw)
		if err != nil || yaml.Unmarshal(data, &config) != nil || config == nil {
			return Entry{}, imageError("image.normalize", root, fmt.Errorf("images.%s must be an object", alias))
		}
	}
	if _, exists := config["upstream_match"]; exists {
		return Entry{}, imageError("image.normalize", root, fmt.Errorf("images.%s.upstream_match is invalid; use upstream-match", alias))
	}
	if _, exists := config["dockerfile_content"]; exists {
		return Entry{}, imageError("image.normalize", root, fmt.Errorf("images.%s.dockerfile_content is invalid; use dockerfile-content", alias))
	}
	dockerfile := strings.TrimSpace(stringValue(config["dockerfile"]))
	inline, hasInline := config["dockerfile-content"]
	inlineContent := ""
	if hasInline {
		inlineContent = stringValue(inline)
		if strings.TrimSpace(inlineContent) == "" {
			inlineContent = ""
		}
	}
	if (dockerfile == "") == (inlineContent == "") {
		return Entry{}, imageError("image.normalize", root, fmt.Errorf("images.%s requires exactly one Dockerfile source", alias))
	}
	contextValue := strings.TrimSpace(stringValue(config["context"]))
	entry := Entry{Alias: alias, UpstreamMatch: DefaultUpstreamMatch, DockerfileContent: inlineContent}
	if dockerfile != "" {
		entry.DockerfilePath = resolvePath(root, dockerfile)
		info, err := os.Stat(entry.DockerfilePath)
		if err != nil || !info.Mode().IsRegular() {
			if err == nil {
				err = errors.New("dockerfile is not a regular file")
			}
			return Entry{}, imageError("image.normalize", entry.DockerfilePath, err)
		}
		if contextValue == "" {
			entry.ContextDir = filepath.Dir(entry.DockerfilePath)
		} else {
			entry.ContextDir = resolvePath(root, contextValue)
		}
	} else if contextValue == "" {
		entry.ContextDir = root
	} else {
		entry.ContextDir = resolvePath(root, contextValue)
	}
	info, err := os.Stat(entry.ContextDir)
	if err != nil || !info.IsDir() {
		if err == nil {
			err = errors.New("context is not a directory")
		}
		return Entry{}, imageError("image.normalize", entry.ContextDir, err)
	}
	if entry.DockerfilePath != "" {
		relative, err := filepath.Rel(entry.ContextDir, entry.DockerfilePath)
		if err != nil || relative == "." || relative == "" || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return Entry{}, imageError("image.normalize", entry.DockerfilePath, errors.New("dockerfile must be inside context"))
		}
	}
	if value, exists := config["upstream-match"]; exists {
		entry.UpstreamMatch = strings.TrimSpace(stringValue(value))
		if entry.UpstreamMatch == "" {
			return Entry{}, imageError("image.normalize", root, fmt.Errorf("images.%s.upstream-match must not be empty", alias))
		}
	}
	entry.Builder = BuilderRemote
	if value, exists := config["builder"]; exists {
		entry.Builder = Builder(strings.ToLower(strings.TrimSpace(stringValue(value))))
	}
	if entry.Builder != BuilderRemote && entry.Builder != BuilderLocal {
		return Entry{}, imageError("image.normalize", root, fmt.Errorf("images.%s.builder must be local or remote", alias))
	}
	return entry, nil
}

func manifestEmbedAliases(appManifest manifest.Manifest) ([]string, error) {
	data, _ := yaml.Marshal(appManifest)
	var root any
	_ = yaml.Unmarshal(data, &root)
	aliases := make(map[string]struct{})
	var parseErr error
	var walk func(any)
	walk = func(value any) {
		if parseErr != nil {
			return
		}
		switch typed := value.(type) {
		case string:
			trimmed := strings.TrimSpace(typed)
			if strings.HasPrefix(strings.ToLower(trimmed), "emebd:") {
				parseErr = fmt.Errorf("invalid image reference prefix %q; use embed:<alias>", trimmed)
				return
			}
			if !strings.HasPrefix(trimmed, "embed:") {
				return
			}
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "embed:"))
			if rest == "" {
				parseErr = fmt.Errorf("image alias is required in %q", trimmed)
				return
			}
			if at := strings.IndexByte(rest, '@'); at >= 0 {
				if strings.TrimSpace(rest[at+1:]) == "" {
					parseErr = fmt.Errorf("image digest is required in %q", trimmed)
					return
				}
				rest = strings.TrimSpace(rest[:at])
			}
			if rest != "" {
				aliases[rest] = struct{}{}
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(root)
	if parseErr != nil {
		return nil, parseErr
	}
	result := make([]string, 0, len(aliases))
	for alias := range aliases {
		result = append(result, alias)
	}
	sort.Strings(result)
	return result, nil
}

func sanitizeTagPart(value, fallback string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return fallback
	}
	var output strings.Builder
	for _, char := range normalized {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '.' || char == '_' || char == '-' {
			output.WriteRune(char)
		} else {
			output.WriteByte('-')
		}
	}
	return output.String()
}

func resolvePath(root, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(root, filepath.FromSlash(value))
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func contextError(ctx context.Context, op string) error {
	if ctx == nil {
		return &lpkgo.Error{Code: lpkgo.CodeInvalidArgument, Op: op, Cause: errors.New("nil context")}
	}
	if err := ctx.Err(); err != nil {
		return &lpkgo.Error{Code: lpkgo.CodeCancelled, Op: op, Cause: err}
	}
	return nil
}

func imageError(op, path string, cause error) error {
	return &lpkgo.Error{Code: lpkgo.CodeInvalidConfig, Op: op, Path: filepath.ToSlash(path), Cause: cause}
}
