package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/build"
	"github.com/lib-x/lzc-toolkit-go/internal/packageid"
	"github.com/lib-x/lzc-toolkit-go/manifest"
)

const inspectionSchemaVersion = 1

// Inspect reads and summarizes a local LazyCat source project. It never runs
// build scripts, builds images, writes an LPK, or contacts a remote service.
func Inspect(ctx context.Context, request InspectRequest) (Inspection, error) {
	if ctx == nil {
		return Inspection{}, inspectError(lpkgo.CodeInvalidArgument, "project.inspect", "", errors.New("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return Inspection{}, inspectError(lpkgo.CodeCancelled, "project.inspect", "", err)
	}
	root, err := inspectionRoot(request.Root)
	if err != nil {
		return Inspection{}, err
	}
	configName := strings.TrimSpace(request.ConfigFile)
	if configName == "" {
		configName = build.DefaultConfigFile
	}
	configPath, err := confinedPath(root, configName, false)
	if err != nil {
		return Inspection{}, err
	}
	if err := rejectPathSymlinks(root, configPath); err != nil {
		return Inspection{}, inspectError(lpkgo.CodeInvalidConfig, "project.inspect.path", filepath.ToSlash(configPath), err)
	}
	if filepath.Base(configPath) == "lzc-build.dev.yml" {
		parentPath := filepath.Join(filepath.Dir(configPath), build.DefaultConfigFile)
		if err := rejectPathSymlinks(root, parentPath); err != nil {
			return Inspection{}, inspectError(lpkgo.CodeInvalidConfig, "project.inspect.path", filepath.ToSlash(parentPath), err)
		}
	}
	packagePath, err := confinedPath(root, "package.yml", false)
	if err != nil {
		return Inspection{}, err
	}
	if err := rejectPathSymlinks(root, packagePath); err != nil {
		return Inspection{}, inspectError(lpkgo.CodeInvalidConfig, "project.inspect.path", filepath.ToSlash(packagePath), err)
	}
	preliminary, err := build.LoadConfig(ctx, root, configName, inspectionEnvironment(request))
	if err != nil {
		return Inspection{}, err
	}
	preliminaryManifest := strings.TrimSpace(preliminary.Config.Manifest)
	if preliminaryManifest == "" {
		preliminaryManifest = "lzc-manifest.yml"
	}
	preliminaryManifestPath, err := confinedPath(root, preliminaryManifest, false)
	if err != nil {
		return Inspection{}, err
	}
	if err := rejectPathSymlinks(root, preliminaryManifestPath); err != nil {
		return Inspection{}, inspectError(lpkgo.CodeInvalidConfig, "project.inspect.path", filepath.ToSlash(preliminaryManifestPath), err)
	}

	resolved, err := build.ResolveConfig(ctx, build.ConfigRequest{
		Root:               root,
		ConfigFile:         configName,
		Environment:        request.Environment,
		InheritEnvironment: request.InheritEnvironment,
	})
	if err != nil {
		return Inspection{}, err
	}
	loaded := resolved.Loaded
	for _, candidate := range []string{loaded.Path, loaded.ParentPath} {
		if candidate == "" {
			continue
		}
		if _, err := confinedPath(root, candidate, false); err != nil {
			return Inspection{}, err
		}
		if err := rejectPathSymlinks(root, candidate); err != nil {
			return Inspection{}, inspectError(lpkgo.CodeInvalidConfig, "project.inspect.path", filepath.ToSlash(candidate), err)
		}
	}

	manifestName := strings.TrimSpace(loaded.Config.Manifest)
	if manifestName == "" {
		manifestName = "lzc-manifest.yml"
	}
	manifestPath, err := confinedPath(root, manifestName, false)
	if err != nil {
		return Inspection{}, err
	}
	for _, candidate := range []string{manifestPath, packagePath} {
		if err := rejectPathSymlinks(root, candidate); err != nil {
			return Inspection{}, inspectError(lpkgo.CodeInvalidConfig, "project.inspect.path", filepath.ToSlash(candidate), err)
		}
	}

	manifestExists, err := regularInspectionFile(manifestPath)
	if err != nil {
		return Inspection{}, inspectError(lpkgo.CodeInvalidManifest, "project.inspect.manifest", filepath.ToSlash(manifestPath), err)
	}
	packageExists, err := regularInspectionFile(packagePath)
	if err != nil {
		return Inspection{}, inspectError(lpkgo.CodeInvalidManifest, "project.inspect.package", filepath.ToSlash(packagePath), err)
	}
	resourceOnly := len(loaded.Config.ResourceExports) > 0 && !manifestExists
	if !manifestExists && !resourceOnly {
		return Inspection{}, inspectError(lpkgo.CodeInvalidManifest, "project.inspect.manifest", filepath.ToSlash(manifestPath), os.ErrNotExist)
	}
	if resourceOnly && !packageExists {
		return Inspection{}, inspectError(lpkgo.CodeInvalidManifest, "project.inspect.package", filepath.ToSlash(packagePath), errors.New("package.yml is required for resource-only projects"))
	}

	var manifestSummary manifest.Summary
	if manifestExists {
		processed, err := manifest.PreprocessFile(ctx, manifestPath, manifest.BuildContext{
			Profile: string(loaded.Profile),
			Env:     loaded.BuildEnv,
		})
		if err != nil {
			return Inspection{}, err
		}
		analysis, err := manifest.Analyze(processed)
		if err != nil {
			return Inspection{}, inspectError(lpkgo.CodeInvalidManifest, "project.inspect.manifest", filepath.ToSlash(manifestPath), err)
		}
		manifestSummary = analysis.Summary()
	}

	packageSummary := manifestSummary.Package
	if packageExists {
		data, err := os.ReadFile(packagePath)
		if err != nil {
			return Inspection{}, inspectError(lpkgo.CodeInvalidManifest, "project.inspect.package", filepath.ToSlash(packagePath), err)
		}
		analysis, err := manifest.Analyze(data)
		if err != nil {
			return Inspection{}, inspectError(lpkgo.CodeInvalidManifest, "project.inspect.package", filepath.ToSlash(packagePath), err)
		}
		if analysis.Template().Present {
			return Inspection{}, inspectError(lpkgo.CodeInvalidManifest, "project.inspect.package", filepath.ToSlash(packagePath), errors.New("package identity must be static"))
		}
		packageSummary = analysis.Summary().Package
	}
	packageSummary, err = effectivePackageSummary(packageSummary, loaded.Config, request.VersionOverride)
	if err != nil {
		return Inspection{}, inspectError(lpkgo.CodeInvalidConfig, "project.inspect.package", filepath.ToSlash(packagePath), err)
	}
	if !packageid.Valid(strings.TrimSpace(packageSummary.Package)) {
		return Inspection{}, inspectError(lpkgo.CodeInvalidManifest, "project.inspect.package", filepath.ToSlash(packagePath), errors.New("invalid or templated package ID"))
	}
	if strings.TrimSpace(packageSummary.Version) == "" {
		return Inspection{}, inspectError(lpkgo.CodeInvalidManifest, "project.inspect.package", filepath.ToSlash(packagePath), errors.New("version is required and must be static"))
	}
	if !resourceOnly && strings.TrimSpace(manifestSummary.Application.Subdomain) == "" {
		return Inspection{}, inspectError(lpkgo.CodeInvalidManifest, "project.inspect.manifest", filepath.ToSlash(manifestPath), errors.New("application.subdomain is required and must be static"))
	}

	files, buildInfo, err := inspectBuildFiles(root, loaded)
	if err != nil {
		return Inspection{}, err
	}
	files.BuildConfig = loaded.Path
	files.BuildParent = loaded.ParentPath
	files.PackageFile = packagePath
	files.ManifestFile = manifestPath

	kind := KindStatic
	if manifestSummary.Application.HasServices {
		kind = KindService
	} else if manifestSummary.Application.HasExecLaunch {
		kind = KindExec
	}
	return Inspection{
		SchemaVersion: inspectionSchemaVersion,
		Kind:          kind,
		Layout:        build.PredictLayout(request.ForceV2, loaded, packageExists, resourceOnly),
		ResourceOnly:  resourceOnly,
		Files:         files,
		Package:       packageSummary,
		Build:         buildInfo,
		Application:   manifestSummary.Application,
		Services:      manifestSummary.Services,
		Images:        manifestSummary.Images,
		Template:      manifestSummary.Template,
		Diagnostics:   manifestSummary.Diagnostics,
	}, nil
}

func inspectionEnvironment(request InspectRequest) map[string]string {
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
	return environment
}

func inspectionRoot(configured string) (string, error) {
	if strings.TrimSpace(configured) == "" {
		configured = "."
	}
	root, err := filepath.Abs(configured)
	if err != nil {
		return "", inspectError(lpkgo.CodeInvalidConfig, "project.inspect.path", filepath.ToSlash(configured), err)
	}
	root = filepath.Clean(root)
	info, err := os.Lstat(root)
	if err != nil {
		return "", inspectError(lpkgo.CodeInvalidConfig, "project.inspect.path", filepath.ToSlash(root), err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", inspectError(lpkgo.CodeInvalidConfig, "project.inspect.path", filepath.ToSlash(root), errors.New("project root must be a real directory"))
	}
	evaluated, err := filepath.EvalSymlinks(root)
	if err != nil || filepath.Clean(evaluated) != root {
		return "", inspectError(lpkgo.CodeInvalidConfig, "project.inspect.path", filepath.ToSlash(root), errors.New("project root contains a symbolic link"))
	}
	return root, nil
}

func confinedPath(root, configured string, allowEmpty bool) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" && allowEmpty {
		return "", nil
	}
	if configured == "" {
		return "", inspectError(lpkgo.CodeInvalidConfig, "project.inspect.path", "", errors.New("empty project path"))
	}
	path := configured
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, filepath.FromSlash(path))
	}
	path = filepath.Clean(path)
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", inspectError(lpkgo.CodeInvalidConfig, "project.inspect.path", filepath.ToSlash(path), errors.New("path escapes project root"))
	}
	return path, nil
}

func rejectPathSymlinks(root, path string) error {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("path contains a symbolic link")
		}
	}
	return nil
}

func regularInspectionFile(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("path is not a regular file")
	}
	return true, nil
}

func effectivePackageSummary(summary manifest.PackageSummary, config build.Config, versionOverride string) (manifest.PackageSummary, error) {
	overrides := config.PackageOverride
	if config.PackageID != nil {
		if _, exists := overrides["package"]; !exists {
			summary.Package = strings.TrimSpace(*config.PackageID)
		}
	}
	if config.PackageName != nil {
		if _, exists := overrides["name"]; !exists {
			summary.Name = strings.TrimSpace(*config.PackageName)
		}
	}
	for field, value := range overrides {
		if err := setPackageSummaryField(&summary, field, value); err != nil {
			return manifest.PackageSummary{}, err
		}
	}
	if strings.TrimSpace(versionOverride) != "" {
		summary.Version = strings.TrimSpace(versionOverride)
	}
	sort.Strings(summary.LocaleCodes)
	sort.Strings(summary.UnsupportedPlatforms)
	return summary, nil
}

func setPackageSummaryField(summary *manifest.PackageSummary, field string, value any) error {
	if field == "unsupported_platforms" {
		if value == nil {
			summary.UnsupportedPlatforms = []string{}
			return nil
		}
		values, err := stringSlice(value)
		if err != nil {
			return fmt.Errorf("package override %q: %w", field, err)
		}
		summary.UnsupportedPlatforms = values
		return nil
	}
	if field == "locales" {
		if value == nil {
			summary.LocaleCodes = []string{}
			return nil
		}
		mapping, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("package override %q must be a mapping or null", field)
		}
		summary.LocaleCodes = summary.LocaleCodes[:0]
		for code := range mapping {
			if code = strings.TrimSpace(code); code != "" {
				summary.LocaleCodes = append(summary.LocaleCodes, code)
			}
		}
		return nil
	}
	if value == nil {
		value = ""
	}
	stringValue, ok := value.(string)
	if !ok {
		return fmt.Errorf("package override %q must be a string or null", field)
	}
	switch field {
	case "package":
		summary.Package = stringValue
	case "version":
		summary.Version = stringValue
	case "name":
		summary.Name = stringValue
	case "description":
		summary.Description = stringValue
	case "author":
		summary.Author = stringValue
	case "license":
		summary.License = stringValue
	case "homepage":
		summary.Homepage = stringValue
	case "min_os_version":
		summary.MinOSVersion = stringValue
	}
	return nil
}

func stringSlice(value any) ([]string, error) {
	entries, ok := value.([]any)
	if !ok {
		if strings, ok := value.([]string); ok {
			return append([]string(nil), strings...), nil
		}
		return nil, errors.New("must be a string list")
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		text, ok := entry.(string)
		if !ok {
			return nil, errors.New("must contain only strings")
		}
		result = append(result, text)
	}
	return result, nil
}

func inspectBuildFiles(root string, loaded build.LoadedConfig) (FileInfo, BuildInfo, error) {
	files := FileInfo{Root: root}
	for _, entry := range []struct {
		configured string
		target     *string
	}{
		{configured: loaded.Config.PackageOutputDir, target: &files.PackageOutputDir},
		{configured: loaded.Config.LPKPath, target: &files.LPKPath},
		{configured: loaded.Config.ContentDir, target: &files.ContentDir},
		{configured: loaded.Config.Icon, target: &files.Icon},
		{configured: loaded.Config.DeployParams, target: &files.DeployParams},
	} {
		resolved, err := confinedPath(root, entry.configured, true)
		if err != nil {
			return FileInfo{}, BuildInfo{}, err
		}
		if resolved != "" {
			if err := rejectPathSymlinks(root, resolved); err != nil {
				return FileInfo{}, BuildInfo{}, inspectError(lpkgo.CodeInvalidConfig, "project.inspect.path", filepath.ToSlash(resolved), err)
			}
		}
		*entry.target = resolved
	}
	resourceExports := make([]ResourceExportInfo, 0, len(loaded.Config.ResourceExports))
	for _, export := range loaded.Config.ResourceExports {
		source, err := confinedPath(root, export.Source, false)
		if err != nil {
			return FileInfo{}, BuildInfo{}, err
		}
		if err := rejectPathSymlinks(root, source); err != nil {
			return FileInfo{}, BuildInfo{}, inspectError(lpkgo.CodeInvalidConfig, "project.inspect.path", filepath.ToSlash(source), err)
		}
		resourceExports = append(resourceExports, ResourceExportInfo{Kind: strings.TrimSpace(export.Kind), Source: source})
	}
	sort.Slice(resourceExports, func(i, j int) bool {
		if resourceExports[i].Kind == resourceExports[j].Kind {
			return resourceExports[i].Source < resourceExports[j].Source
		}
		return resourceExports[i].Kind < resourceExports[j].Kind
	})
	aliases := configuredImageAliases(loaded.Config.Images)
	hasContent := false
	if files.ContentDir != "" {
		if info, err := os.Stat(files.ContentDir); err == nil && info.IsDir() {
			hasContent = true
		}
	}
	return files, BuildInfo{
		Profile:                loaded.Profile,
		HasBuildScript:         strings.TrimSpace(loaded.Config.BuildScript) != "",
		HasContent:             hasContent,
		HasComposeOverride:     len(loaded.Config.ComposeOverride) > 0,
		ConfiguredImageAliases: aliases,
		ResourceExports:        resourceExports,
	}, nil
}

func configuredImageAliases(value any) []string {
	aliases := make([]string, 0)
	switch typed := value.(type) {
	case map[string]any:
		for alias := range typed {
			if alias = strings.TrimSpace(alias); alias != "" {
				aliases = append(aliases, alias)
			}
		}
	case []any:
		for _, entry := range typed {
			mapping, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if alias, ok := mapping["id"].(string); ok && strings.TrimSpace(alias) != "" {
				aliases = append(aliases, strings.TrimSpace(alias))
			}
		}
	}
	sort.Strings(aliases)
	return aliases
}

func inspectError(code lpkgo.Code, op, path string, cause error) error {
	return &lpkgo.Error{Code: code, Op: op, Path: path, Cause: cause}
}
