package build

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

func buildEnvironment(request Request) map[string]string {
	environment := collectEnvironment(request.InheritEnvironment, request.Environment)
	if request.LocalIP != "" {
		environment["LocalIP"] = request.LocalIP
	}
	return environment
}

func hasConfiguredImages(value any) bool {
	if value == nil {
		return false
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array:
		return reflected.Len() > 0
	default:
		return true
	}
}

func rejectRemovedOptions(loaded LoadedConfig) error {
	removed := map[string]string{
		"embed_images":      "use images and manifest embed:alias references",
		"embed_all_images":  "the option was removed",
		"upstream_registry": "use images.<alias>.upstream-match",
		"upstream_match":    "move it to images.<alias>.upstream-match",
		"upstream-match":    "move it to images.<alias>.upstream-match",
	}
	for field, replacement := range removed {
		if _, exists := loaded.Raw[field]; exists {
			return configError("build.compatibility", loaded.Path, fmt.Errorf("%s is removed: %s", field, replacement))
		}
	}
	return nil
}

func regularFileExists(filename string) (bool, error) {
	info, err := os.Stat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

func resolveProjectPath(root, configured string) string {
	if filepath.IsAbs(configured) {
		return filepath.Clean(configured)
	}
	return filepath.Join(root, filepath.FromSlash(configured))
}

func buildPathError(op, path string, cause error) error {
	return &lpkgo.Error{Code: lpkgo.CodeInvalidConfig, Op: op, Path: filepath.ToSlash(path), Cause: cause}
}

func cloneStringMap(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
