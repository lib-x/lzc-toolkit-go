package lint

import (
	"context"
	"errors"
	"io/fs"
	"regexp"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
	manifestpkg "github.com/lib-x/lpk-go/manifest"
)

const maxResourceExportKinds = 100

var packageNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*(\.[a-z][a-z0-9]*(-[a-z0-9]+)*)*$`)
var resourceExportNamePattern = regexp.MustCompile(`^[a-z0-9._-]+$`)

var errResourcePayloadFound = errors.New("resource payload found")

// ResourcePackage reports compatibility problems in a resource-only package
// rooted at root.
func ResourcePackage(ctx context.Context, root fs.FS) ([]lpkgo.Warning, error) {
	if err := lintContextError(ctx, "lint.resource_package"); err != nil {
		return nil, err
	}
	if root == nil {
		return nil, &lpkgo.Error{Code: lpkgo.CodeInvalidArgument, Op: "lint.resource_package", Cause: errors.New("nil filesystem")}
	}
	warnings := make([]lpkgo.Warning, 0)
	packageFileInfo, err := fs.Stat(root, "package.yml")
	if errors.Is(err, fs.ErrNotExist) || err == nil && !packageFileInfo.Mode().IsRegular() {
		warnings = append(warnings, resourceWarning("resource-package-file-missing", "package.yml", "package.yml is required for resource-only LPK packages."))
	} else if err != nil {
		return nil, resourceLintError("package.yml")
	} else {
		packageWarnings, err := lintResourcePackageFile(ctx, root)
		if err != nil {
			return nil, err
		}
		warnings = append(warnings, packageWarnings...)
	}

	exportsInfo, err := fs.Stat(root, "exports")
	if errors.Is(err, fs.ErrNotExist) || err == nil && !exportsInfo.IsDir() {
		warnings = append(warnings, resourceWarning("resource-exports-missing", "exports", "exports directory is required for resource-only LPK packages."))
	} else if err != nil {
		return nil, resourceLintError("exports")
	} else {
		exportWarnings, err := lintResourceExports(ctx, root)
		if err != nil {
			return nil, err
		}
		warnings = append(warnings, exportWarnings...)
	}
	return warnings, nil
}

func lintResourcePackageFile(ctx context.Context, root fs.FS) ([]lpkgo.Warning, error) {
	if err := lintContextError(ctx, "lint.resource_package"); err != nil {
		return nil, err
	}
	data, err := fs.ReadFile(root, "package.yml")
	if err != nil {
		return nil, resourceLintError("package.yml")
	}
	if err := lintContextError(ctx, "lint.resource_package"); err != nil {
		return nil, err
	}
	document, err := manifestpkg.Parse(data)
	if err != nil {
		return nil, err
	}
	var packageInfo manifestpkg.PackageInfo
	if err := document.Decode(&packageInfo); err != nil {
		return nil, err
	}

	warnings := make([]lpkgo.Warning, 0, 2)
	if !packageNamePattern.MatchString(packageInfo.Package) {
		warnings = append(warnings, resourceWarning("resource-package-name-invalid", "package.yml.package", "package.yml package must be a valid package name."))
	}
	if strings.TrimSpace(packageInfo.Version) == "" {
		warnings = append(warnings, resourceWarning("resource-package-version-missing", "package.yml.version", "package.yml version is required."))
	}
	return warnings, nil
}

func lintResourceExports(ctx context.Context, root fs.FS) ([]lpkgo.Warning, error) {
	if err := lintContextError(ctx, "lint.resource_package"); err != nil {
		return nil, err
	}
	kindEntries, err := fs.ReadDir(root, "exports")
	if err != nil {
		return nil, resourceLintError("exports")
	}
	kindEntries = visibleEntries(kindEntries)
	if len(kindEntries) == 0 {
		return []lpkgo.Warning{resourceWarning("resource-exports-empty", "exports", "exports directory must contain at least one resource kind.")}, nil
	}

	warnings := make([]lpkgo.Warning, 0)
	if len(kindEntries) > maxResourceExportKinds {
		warnings = append(warnings, resourceWarning("resource-exports-too-many-kinds", "exports", "resource-only packages may export at most 100 visible kinds."))
	}
	seenKinds := make(map[string]struct{}, len(kindEntries))
	for _, kindEntry := range kindEntries {
		if err := lintContextError(ctx, "lint.resource_package"); err != nil {
			return nil, err
		}
		kind := kindEntry.Name()
		kindPath := "exports/" + kind
		if !kindEntry.IsDir() {
			warnings = append(warnings, resourceWarning("resource-export-kind-not-directory", kindPath, "resource export kind must be a directory."))
			continue
		}
		if !validResourceExportName(kind) {
			warnings = append(warnings, resourceWarning("resource-export-kind-invalid", kindPath, "resource export kind name is invalid."))
			continue
		}
		if _, exists := seenKinds[kind]; exists {
			warnings = append(warnings, resourceWarning("resource-export-kind-duplicated", kindPath, "resource export kind is duplicated."))
			continue
		}
		seenKinds[kind] = struct{}{}

		resourceEntries, err := fs.ReadDir(root, kindPath)
		if err != nil {
			return nil, resourceLintError(kindPath)
		}
		resourceEntries = visibleEntries(resourceEntries)
		if len(resourceEntries) == 0 {
			warnings = append(warnings, resourceWarning("resource-export-kind-empty", kindPath, "resource export kind is empty."))
			continue
		}
		for _, resourceEntry := range resourceEntries {
			if err := lintContextError(ctx, "lint.resource_package"); err != nil {
				return nil, err
			}
			resourcePath := kindPath + "/" + resourceEntry.Name()
			if !resourceEntry.IsDir() {
				warnings = append(warnings, resourceWarning("resource-export-id-not-directory", resourcePath, "resource export kind must contain only resource directories."))
				continue
			}
			if !validResourceExportName(resourceEntry.Name()) {
				warnings = append(warnings, resourceWarning("resource-export-id-invalid", resourcePath, "resource export ID name is invalid."))
				continue
			}
			hasPayload, err := hasResourcePayload(ctx, root, resourcePath)
			if err != nil {
				return nil, err
			}
			if !hasPayload {
				warnings = append(warnings, resourceWarning("resource-export-payload-empty", resourcePath, "resource export payload is empty."))
			}
		}
	}
	return warnings, nil
}

func visibleEntries(entries []fs.DirEntry) []fs.DirEntry {
	visible := make([]fs.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".") {
			visible = append(visible, entry)
		}
	}
	return visible
}

func validResourceExportName(name string) bool {
	return name != "" && !strings.HasPrefix(name, ".") && resourceExportNamePattern.MatchString(name)
}

func hasResourcePayload(ctx context.Context, root fs.FS, resourcePath string) (bool, error) {
	err := fs.WalkDir(root, resourcePath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := lintContextError(ctx, "lint.resource_package"); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			return errResourcePayloadFound
		}
		return nil
	})
	if errors.Is(err, errResourcePayloadFound) {
		return true, nil
	}
	if err != nil {
		var structured *lpkgo.Error
		if errors.As(err, &structured) {
			return false, err
		}
		return false, resourceLintError(resourcePath)
	}
	return false, nil
}

func resourceWarning(code string, path string, message string) lpkgo.Warning {
	return lpkgo.Warning{Code: code, Severity: lpkgo.SeverityWarning, Path: path, Message: message}
}

func lintContextError(ctx context.Context, op string) error {
	if ctx == nil {
		return &lpkgo.Error{Code: lpkgo.CodeInvalidArgument, Op: op, Cause: errors.New("nil context")}
	}
	if err := ctx.Err(); err != nil {
		return &lpkgo.Error{Code: lpkgo.CodeCancelled, Op: op, Cause: err}
	}
	return nil
}

func resourceLintError(path string) error {
	return &lpkgo.Error{Code: lpkgo.CodeInvalidManifest, Op: "lint.resource_package", Path: path, Cause: errors.New("resource package traversal failed")}
}
