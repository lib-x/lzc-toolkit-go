package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lib-x/lzc-toolkit-go/lpk"
	"github.com/lib-x/lzc-toolkit-go/manifest"
	"go.yaml.in/yaml/v3"
)

// ConfigRequest describes the project configuration inputs resolved before a
// build or inspection operation.
type ConfigRequest struct {
	Root               string
	ConfigFile         string
	Environment        map[string]string
	InheritEnvironment bool
}

// ResolvedConfig contains a normalized project root and its effective build
// configuration.
type ResolvedConfig struct {
	Root   string
	Loaded LoadedConfig
}

// ResolveConfig resolves the effective three-pass build configuration used by
// project operations.
func ResolveConfig(ctx context.Context, request ConfigRequest) (ResolvedConfig, error) {
	if err := checkContext(ctx, "build.resolve_config"); err != nil {
		return ResolvedConfig{}, err
	}
	root, err := resolveRoot(request.Root)
	if err != nil {
		return ResolvedConfig{}, err
	}
	environment := configEnvironment(request)
	loaded, err := LoadConfig(ctx, root, request.ConfigFile, environment)
	if err != nil {
		return ResolvedConfig{}, err
	}
	baseTemplateEnv := cloneStringMap(environment)
	templateValues, err := loadStaticPackageValues(ctx, root, loaded)
	if err != nil {
		return ResolvedConfig{}, err
	}
	for key, value := range templateValues {
		baseTemplateEnv[key] = value
	}
	firstPass, err := LoadConfig(ctx, root, request.ConfigFile, baseTemplateEnv)
	if err != nil {
		return ResolvedConfig{}, err
	}
	finalTemplateEnv := cloneStringMap(baseTemplateEnv)
	for key, value := range firstPass.BuildEnv {
		finalTemplateEnv[key] = value
	}
	loaded, err = LoadConfig(ctx, root, request.ConfigFile, finalTemplateEnv)
	if err != nil {
		return ResolvedConfig{}, err
	}
	if err := rejectRemovedOptions(loaded); err != nil {
		return ResolvedConfig{}, err
	}
	return ResolvedConfig{Root: root, Loaded: cloneLoadedConfig(loaded)}, nil
}

// PredictLayout reports the layout selected by the current build rules.
func PredictLayout(forceV2 bool, loaded LoadedConfig, packageExists, resourceOnly bool) lpk.Layout {
	if forceV2 || packageExists || loaded.Config.Images != nil || len(loaded.Config.Envs) > 0 || len(loaded.Config.PackageOverride) > 0 || loaded.Config.PackageID != nil || loaded.Config.PackageName != nil || resourceOnly {
		return lpk.LayoutV2
	}
	return lpk.LayoutV1
}

func configEnvironment(request ConfigRequest) map[string]string {
	return collectEnvironment(request.InheritEnvironment, request.Environment)
}

func collectEnvironment(inherit bool, explicit map[string]string) map[string]string {
	environment := make(map[string]string)
	if inherit {
		for _, entry := range os.Environ() {
			if equal := strings.IndexByte(entry, '='); equal > 0 {
				environment[entry[:equal]] = entry[equal+1:]
			}
		}
	}
	for key, value := range explicit {
		environment[key] = value
	}
	return environment
}

func loadStaticPackageValues(ctx context.Context, root string, loaded LoadedConfig) (map[string]string, error) {
	values := make(map[string]string)
	manifestName := strings.TrimSpace(loaded.Config.Manifest)
	if manifestName == "" {
		manifestName = "lzc-manifest.yml"
	}
	manifestPath := resolveProjectPath(root, manifestName)
	if _, err := os.Stat(manifestPath); err == nil {
		data, preprocessErr := manifest.PreprocessFile(ctx, manifestPath, manifest.BuildContext{
			Profile: string(loaded.Profile),
			Env:     loaded.BuildEnv,
		})
		if preprocessErr != nil {
			return nil, preprocessErr
		}
		analysis, analyzeErr := manifest.Analyze(data)
		if analyzeErr != nil {
			return nil, analyzeErr
		}
		summary := analysis.Summary()
		templated := make(map[string]struct{})
		for _, diagnostic := range summary.Diagnostics {
			if diagnostic.Code == "TEMPLATED_FIELD" {
				templated[diagnostic.Path] = struct{}{}
			}
		}
		document := analysis.Document()
		for key, value := range map[string]string{
			"package":        summary.Package.Package,
			"version":        summary.Package.Version,
			"name":           summary.Package.Name,
			"description":    summary.Package.Description,
			"author":         summary.Package.Author,
			"license":        summary.Package.License,
			"homepage":       summary.Package.Homepage,
			"min_os_version": summary.Package.MinOSVersion,
		} {
			if _, dynamic := templated[key]; dynamic {
				continue
			}
			if _, found, lookupErr := document.Lookup(key); lookupErr != nil {
				return nil, lookupErr
			} else if found {
				values[key] = value
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, buildPathError("build.template_manifest", manifestPath, err)
	}

	packagePath := filepath.Join(root, "package.yml")
	if data, err := os.ReadFile(packagePath); err == nil {
		var packageValues map[string]any
		if err := yaml.Unmarshal(data, &packageValues); err != nil {
			return nil, configError("build.template_package", packagePath, err)
		}
		for key, value := range packageValues {
			switch typed := value.(type) {
			case nil:
				values[key] = ""
			case string:
				values[key] = typed
			case bool, int, int64, uint64, float64:
				values[key] = fmt.Sprint(typed)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, buildPathError("build.template_package", packagePath, err)
	}
	return values, nil
}

func cloneLoadedConfig(source LoadedConfig) LoadedConfig {
	clone := source
	clone.Raw = cloneAnyMap(source.Raw)
	clone.BuildEnv = cloneStringMap(source.BuildEnv)
	clone.Config.ComposeOverride = cloneAnyMap(source.Config.ComposeOverride)
	clone.Config.Envs = append([]string(nil), source.Config.Envs...)
	clone.Config.PackageOverride = cloneAnyMap(source.Config.PackageOverride)
	clone.Config.Images = cloneAny(source.Config.Images)
	clone.Config.ResourceExports = append([]ResourceExport(nil), source.Config.ResourceExports...)
	clone.Config.Remote = cloneAnyMap(source.Config.Remote)
	return clone
}

func cloneAnyMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = cloneAny(value)
	}
	return clone
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		clone := make([]any, len(typed))
		for index, entry := range typed {
			clone[index] = cloneAny(entry)
		}
		return clone
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}
