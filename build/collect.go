package build

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/archive"
	"go.yaml.in/yaml/v3"
)

const maxResourceExportKinds = 100

var resourceNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func collectProjectContent(ctx context.Context, projectRoot, staging string, config Config) ([]lpkgo.Warning, error) {
	warnings := make([]lpkgo.Warning, 0)
	if strings.TrimSpace(config.ContentDir) != "" {
		source := resolveProjectPath(projectRoot, config.ContentDir)
		if err := requireDirectory(source, "build.content"); err != nil {
			return nil, err
		}
		output, err := os.Create(filepath.Join(staging, "content.tar"))
		if err != nil {
			return nil, buildPathError("build.content", source, err)
		}
		_, writeErr := archive.Write(ctx, output, os.DirFS(source), archive.WriteOptions{Format: archive.FormatTAR, Reproducible: true})
		closeErr := output.Close()
		if writeErr != nil {
			return nil, writeErr
		}
		if closeErr != nil {
			return nil, buildPathError("build.content", source, closeErr)
		}
	}

	iconWarnings, err := collectIcon(projectRoot, staging, config.Icon)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, iconWarnings...)

	deployWarnings, err := collectDeployParams(projectRoot, staging, config.DeployParams)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, deployWarnings...)

	if strings.TrimSpace(config.BrowserExtension) != "" {
		if err := collectBrowserExtension(ctx, projectRoot, staging, config.BrowserExtension); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(config.AIPodService) != "" {
		source := resolveProjectPath(projectRoot, config.AIPodService)
		if err := requireDirectory(source, "build.ai_pod_service"); err != nil {
			return nil, err
		}
		if err := copyTree(ctx, source, filepath.Join(staging, "ai-pod-service")); err != nil {
			return nil, err
		}
	}
	if config.ComposeOverride != nil {
		data, err := yaml.Marshal(config.ComposeOverride)
		if err != nil {
			return nil, configError("build.compose_override", "compose.override.yml", err)
		}
		if err := os.WriteFile(filepath.Join(staging, "compose.override.yml"), data, 0o644); err != nil {
			return nil, buildPathError("build.compose_override", staging, err)
		}
	}
	if err := collectResourceExports(ctx, projectRoot, staging, config.ResourceExports); err != nil {
		return nil, err
	}
	return warnings, nil
}

func collectIcon(projectRoot, staging, configured string) ([]lpkgo.Warning, error) {
	if strings.TrimSpace(configured) == "" {
		return []lpkgo.Warning{{Code: "build.icon.not_configured", Severity: lpkgo.SeverityWarning, Message: "icon is not configured"}}, nil
	}
	source := resolveProjectPath(projectRoot, configured)
	if strings.ToLower(filepath.Ext(source)) != ".png" {
		return []lpkgo.Warning{{Code: "build.icon.not_png", Severity: lpkgo.SeverityWarning, Path: filepath.ToSlash(source), Message: "icon is not a .png file"}}, nil
	}
	data, err := os.ReadFile(source)
	if errors.Is(err, os.ErrNotExist) {
		return []lpkgo.Warning{{Code: "build.icon.not_found", Severity: lpkgo.SeverityWarning, Path: filepath.ToSlash(source), Message: "icon file does not exist"}}, nil
	}
	if err != nil {
		return nil, buildPathError("build.icon", source, err)
	}
	if len(data) < 8 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
		return []lpkgo.Warning{{Code: "build.icon.invalid_png", Severity: lpkgo.SeverityWarning, Path: filepath.ToSlash(source), Message: "icon file is not PNG data"}}, nil
	}
	if err := os.WriteFile(filepath.Join(staging, "icon.png"), data, 0o644); err != nil {
		return nil, buildPathError("build.icon", source, err)
	}
	return nil, nil
}

func collectDeployParams(projectRoot, staging, configured string) ([]lpkgo.Warning, error) {
	source := strings.TrimSpace(configured)
	if source == "" {
		candidate := filepath.Join(projectRoot, "lzc-deploy-params.yml")
		if exists, err := regularFileExists(candidate); err != nil {
			return nil, buildPathError("build.deploy_params", candidate, err)
		} else if !exists {
			return nil, nil
		}
		source = candidate
	} else {
		source = resolveProjectPath(projectRoot, source)
	}
	data, err := os.ReadFile(source)
	if errors.Is(err, os.ErrNotExist) {
		return []lpkgo.Warning{{Code: "build.deploy_params.not_found", Severity: lpkgo.SeverityWarning, Path: filepath.ToSlash(source), Message: "deploy_params file does not exist"}}, nil
	}
	if err != nil {
		return nil, buildPathError("build.deploy_params", source, err)
	}
	if err := os.WriteFile(filepath.Join(staging, "deploy_params.yml"), data, 0o644); err != nil {
		return nil, buildPathError("build.deploy_params", source, err)
	}
	return nil, nil
}

func collectBrowserExtension(ctx context.Context, projectRoot, staging, configured string) error {
	source := resolveProjectPath(projectRoot, configured)
	info, err := os.Stat(source)
	if err != nil {
		return buildPathError("build.browser_extension", source, err)
	}
	destination := filepath.Join(staging, "extension.zip")
	if info.IsDir() {
		output, err := os.Create(destination)
		if err != nil {
			return buildPathError("build.browser_extension", source, err)
		}
		_, writeErr := archive.Write(ctx, output, os.DirFS(source), archive.WriteOptions{Format: archive.FormatZIP, Reproducible: true})
		closeErr := output.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return buildPathError("build.browser_extension", source, closeErr)
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return buildPathError("build.browser_extension", source, errors.New("unsupported file type"))
	}
	return copyFile(source, destination)
}

func collectResourceExports(ctx context.Context, projectRoot, staging string, exports []ResourceExport) error {
	if len(exports) > maxResourceExportKinds {
		return configError("build.resource_exports", projectRoot, fmt.Errorf("too many resource export kinds: %d > %d", len(exports), maxResourceExportKinds))
	}
	seen := make(map[string]struct{}, len(exports))
	for index, export := range exports {
		if err := checkContext(ctx, "build.resource_exports"); err != nil {
			return err
		}
		kind := strings.TrimSpace(export.Kind)
		if !resourceNamePattern.MatchString(kind) {
			return configError("build.resource_exports", projectRoot, fmt.Errorf("invalid resource export kind at index %d: %q", index, kind))
		}
		if _, exists := seen[kind]; exists {
			return configError("build.resource_exports", projectRoot, fmt.Errorf("duplicated resource export kind: %s", kind))
		}
		seen[kind] = struct{}{}
		source := resolveProjectPath(projectRoot, strings.TrimSpace(export.Source))
		if err := requireDirectory(source, "build.resource_exports"); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return buildPathError("build.resource_exports", source, err)
		}
		count := 0
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			id := entry.Name()
			if strings.HasPrefix(id, ".") {
				continue
			}
			if !entry.IsDir() {
				return configError("build.resource_exports", source, fmt.Errorf("resource export source must contain only directories: %s/%s", kind, id))
			}
			if !resourceNamePattern.MatchString(id) {
				return configError("build.resource_exports", source, fmt.Errorf("invalid resource export id: %s", id))
			}
			resourceSource := filepath.Join(source, id)
			hasPayload, err := treeHasPayload(resourceSource)
			if err != nil {
				return buildPathError("build.resource_exports", resourceSource, err)
			}
			if !hasPayload {
				return configError("build.resource_exports", resourceSource, fmt.Errorf("empty resource export payload: %s/%s", kind, id))
			}
			if err := copyTree(ctx, resourceSource, filepath.Join(staging, "exports", kind, id)); err != nil {
				return err
			}
			count++
		}
		if count == 0 {
			return configError("build.resource_exports", source, fmt.Errorf("resource export source is empty: %s", kind))
		}
	}
	return nil
}

func copyTree(ctx context.Context, source, destination string) error {
	return filepath.WalkDir(source, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return buildPathError("build.copy_tree", name, walkErr)
		}
		if err := checkContext(ctx, "build.copy_tree"); err != nil {
			return err
		}
		relative, err := filepath.Rel(source, name)
		if err != nil {
			return buildPathError("build.copy_tree", name, err)
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return buildPathError("build.copy_tree", name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return buildPathError("build.copy_tree", name, errors.New("unsupported file type"))
		}
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		return copyFile(name, target)
	})
}

func copyFilesystem(ctx context.Context, source fs.FS, destination string) error {
	return fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return buildPathError("build.copy_filesystem", name, walkErr)
		}
		if err := checkContext(ctx, "build.copy_filesystem"); err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if name != "images.lock" && name != "images" && !strings.HasPrefix(name, "images/") {
			return buildPathError("build.copy_filesystem", name, errors.New("image artifact contains a non-image path"))
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return buildPathError("build.copy_filesystem", name, err)
		}
		if !info.Mode().IsRegular() {
			return buildPathError("build.copy_filesystem", name, errors.New("unsupported artifact file type"))
		}
		input, err := source.Open(name)
		if err != nil {
			return buildPathError("build.copy_filesystem", name, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			_ = input.Close()
			return buildPathError("build.copy_filesystem", target, err)
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			_ = input.Close()
			return buildPathError("build.copy_filesystem", target, err)
		}
		_, copyErr := io.Copy(output, &contextReader{ctx: ctx, reader: input})
		inputCloseErr := input.Close()
		outputCloseErr := output.Close()
		if copyErr != nil {
			return buildPathError("build.copy_filesystem", name, copyErr)
		}
		if inputCloseErr != nil {
			return buildPathError("build.copy_filesystem", name, inputCloseErr)
		}
		if outputCloseErr != nil {
			return buildPathError("build.copy_filesystem", target, outputCloseErr)
		}
		return nil
	})
}

func gzipContentArchive(ctx context.Context, staging string) error {
	sourcePath := filepath.Join(staging, "content.tar")
	input, err := os.Open(sourcePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return buildPathError("build.gzip_content", sourcePath, err)
	}
	destinationPath := filepath.Join(staging, "content.tar.gz")
	output, err := os.Create(destinationPath)
	if err != nil {
		_ = input.Close()
		return buildPathError("build.gzip_content", destinationPath, err)
	}
	compressed := gzip.NewWriter(output)
	_, copyErr := io.Copy(compressed, &contextReader{ctx: ctx, reader: input})
	gzipCloseErr := compressed.Close()
	inputCloseErr := input.Close()
	outputCloseErr := output.Close()
	if copyErr != nil {
		return buildPathError("build.gzip_content", sourcePath, copyErr)
	}
	if gzipCloseErr != nil || inputCloseErr != nil || outputCloseErr != nil {
		return buildPathError("build.gzip_content", destinationPath, errors.Join(gzipCloseErr, inputCloseErr, outputCloseErr))
	}
	if err := os.Remove(sourcePath); err != nil {
		return buildPathError("build.gzip_content", sourcePath, err)
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return buildPathError("build.copy_file", source, err)
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return buildPathError("build.copy_file", destination, err)
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return buildPathError("build.copy_file", destination, err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return buildPathError("build.copy_file", source, copyErr)
	}
	if closeErr != nil {
		return buildPathError("build.copy_file", destination, closeErr)
	}
	return nil
}

func requireDirectory(path, op string) error {
	info, err := os.Stat(path)
	if err != nil {
		return buildPathError(op, path, err)
	}
	if !info.IsDir() {
		return buildPathError(op, path, errors.New("path is not a directory"))
	}
	return nil
}

func treeHasPayload(root string) (bool, error) {
	hasPayload := false
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			hasPayload = true
			return fs.SkipAll
		}
		return nil
	})
	return hasPayload, err
}
