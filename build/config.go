package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
	"go.yaml.in/yaml/v3"
)

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// LoadConfig loads and normalizes an lzc-build configuration. In accordance
// with lzc-cli 2.0.8, lzc-build.dev.yml inherits lzc-build.yml using top-level
// replacement; other filenames do not have an implicit parent.
func LoadConfig(ctx context.Context, root, configFile string, environment map[string]string) (LoadedConfig, error) {
	if err := checkContext(ctx, "build.load_config"); err != nil {
		return LoadedConfig{}, err
	}
	resolvedRoot, err := resolveRoot(root)
	if err != nil {
		return LoadedConfig{}, err
	}
	if strings.TrimSpace(configFile) == "" {
		configFile = DefaultConfigFile
	}
	configPath := configFile
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(resolvedRoot, configPath)
	}
	configPath = filepath.Clean(configPath)

	profile := ProfileRelease
	parentPath := ""
	if filepath.Base(configPath) == "lzc-build.dev.yml" {
		profile = ProfileDevelopment
		candidate := filepath.Join(filepath.Dir(configPath), DefaultConfigFile)
		if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() {
			parentPath = candidate
		} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return LoadedConfig{}, configError("build.load_config", candidate, statErr)
		}
	}

	merged := make(map[string]any)
	if parentPath != "" {
		parent, readErr := readConfigMap(ctx, parentPath, environment)
		if readErr != nil {
			return LoadedConfig{}, readErr
		}
		mergeTopLevel(merged, parent)
	}
	top, err := readConfigMap(ctx, configPath, environment)
	if err != nil {
		return LoadedConfig{}, err
	}
	mergeTopLevel(merged, top)

	encoded, err := yaml.Marshal(merged)
	if err != nil {
		return LoadedConfig{}, configError("build.load_config", configPath, err)
	}
	var config Config
	if err := yaml.Unmarshal(encoded, &config); err != nil {
		return LoadedConfig{}, configError("build.load_config", configPath, err)
	}
	buildEnv, err := normalizeBuildEnv(config.Envs)
	if err != nil {
		return LoadedConfig{}, configError("build.load_config", configPath, err)
	}
	return LoadedConfig{
		Path:       configPath,
		ParentPath: parentPath,
		Profile:    profile,
		Config:     config,
		Raw:        cloneMap(merged),
		BuildEnv:   buildEnv,
	}, nil
}

func readConfigMap(ctx context.Context, filename string, environment map[string]string) (map[string]any, error) {
	if err := checkContext(ctx, "build.load_config"); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, configError("build.load_config", filename, err)
	}
	expanded := os.Expand(string(data), func(key string) string { return environment[key] })
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, configError("build.load_config", filename, err)
	}
	if raw == nil {
		raw = make(map[string]any)
	}
	return raw, nil
}

func normalizeBuildEnv(entries []string) (map[string]string, error) {
	result := make(map[string]string, len(entries))
	for index, raw := range entries {
		entry := strings.TrimSpace(raw)
		equal := strings.IndexByte(entry, '=')
		if equal <= 0 {
			return nil, fmt.Errorf("envs[%d] must use KEY=VALUE format", index)
		}
		key := strings.TrimSpace(entry[:equal])
		if !envKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("envs[%d] has invalid key %q", index, key)
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("envs contains duplicated key %q", key)
		}
		result[key] = entry[equal+1:]
	}
	return result, nil
}

func resolveRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	resolved, err := filepath.Abs(root)
	if err != nil {
		return "", configError("build.resolve_root", root, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", configError("build.resolve_root", resolved, err)
	}
	if !info.IsDir() {
		return "", configError("build.resolve_root", resolved, errors.New("project root is not a directory"))
	}
	return filepath.Clean(resolved), nil
}

func mergeTopLevel(dst, src map[string]any) {
	for key, value := range src {
		dst[key] = value
	}
}

func cloneMap(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func checkContext(ctx context.Context, op string) error {
	if ctx == nil {
		return &lpkgo.Error{Code: lpkgo.CodeInvalidArgument, Op: op, Cause: errors.New("nil context")}
	}
	if err := ctx.Err(); err != nil {
		return &lpkgo.Error{Code: lpkgo.CodeCancelled, Op: op, Cause: err}
	}
	return nil
}

func configError(op, path string, cause error) error {
	return &lpkgo.Error{Code: lpkgo.CodeInvalidConfig, Op: op, Path: filepath.ToSlash(path), Cause: cause}
}
