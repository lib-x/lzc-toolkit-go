package build

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/lpk"
)

func buildEnvironment(request Request) map[string]string {
	environment := make(map[string]string)
	if request.InheritEnvironment {
		for _, entry := range os.Environ() {
			if equal := strings.IndexByte(entry, '='); equal > 0 {
				environment[entry[:equal]] = entry[equal+1:]
			}
		}
	}
	for key, value := range request.Environment {
		environment[key] = value
	}
	if request.LocalIP != "" {
		environment["LocalIP"] = request.LocalIP
	}
	return environment
}

func selectLayout(request Request, loaded LoadedConfig, packageExists, resourceOnly bool) lpk.Layout {
	if request.ForceV2 || packageExists || loaded.Config.Images != nil || len(loaded.Config.Envs) > 0 || len(loaded.Config.PackageOverride) > 0 || loaded.Config.PackageID != nil || loaded.Config.PackageName != nil || resourceOnly {
		return lpk.LayoutV2
	}
	return lpk.LayoutV1
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
