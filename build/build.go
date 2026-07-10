package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/lpk"
	"github.com/lib-x/lpk-go/manifest"
	"github.com/lib-x/lpk-go/oci"
)

type preparedBuild struct {
	root      string
	config    LoadedConfig
	layout    lpk.Layout
	packageID string
	version   string
	warnings  []lpkgo.Warning
	templated bool
	images    oci.Report
	cleanup   func() error
}

// Build prepares and encodes a project as an LPK. It never closes dst.
func Build(ctx context.Context, dst io.Writer, request Request) (Result, error) {
	if dst == nil {
		return Result{}, &lpkgo.Error{Code: lpkgo.CodeInvalidArgument, Op: "build.build", Cause: errors.New("nil writer")}
	}
	prepared, err := prepare(ctx, request)
	if err != nil {
		return Result{}, err
	}
	defer prepared.cleanup() // best-effort removal of an internal temporary tree

	written, err := lpk.Write(ctx, dst, lpk.WriteRequest{
		Layout:                prepared.layout,
		Files:                 os.DirFS(prepared.root),
		Strict:                request.Strict,
		AllowManifestTemplate: prepared.templated,
	})
	result := prepared.result()
	result.Write = written
	return result, err
}

// BuildFile prepares a project and atomically replaces filename with the
// completed LPK.
func BuildFile(ctx context.Context, filename string, request Request) (Result, error) {
	if strings.TrimSpace(filename) == "" {
		return Result{}, &lpkgo.Error{Code: lpkgo.CodeInvalidArgument, Op: "build.build_file", Cause: errors.New("empty filename")}
	}
	prepared, err := prepare(ctx, request)
	if err != nil {
		return Result{}, err
	}
	defer prepared.cleanup()

	written, err := lpk.WriteFile(ctx, filename, lpk.WriteRequest{
		Layout:                prepared.layout,
		Files:                 os.DirFS(prepared.root),
		Strict:                request.Strict,
		AllowManifestTemplate: prepared.templated,
	})
	result := prepared.result()
	result.Write = written
	return result, err
}

func (p preparedBuild) result() Result {
	return Result{
		ConfigPath:     p.config.Path,
		Profile:        p.config.Profile,
		Layout:         p.layout,
		Package:        p.packageID,
		Version:        p.version,
		Warnings:       append([]lpkgo.Warning(nil), p.warnings...),
		ImageCount:     p.images.ImageCount,
		ResolvedImages: cloneStringMap(p.images.ResolvedByAlias),
	}
}

func prepare(ctx context.Context, request Request) (preparedBuild, error) {
	if err := checkContext(ctx, "build.prepare"); err != nil {
		return preparedBuild{}, err
	}
	root, err := resolveRoot(request.Root)
	if err != nil {
		return preparedBuild{}, err
	}
	environment := buildEnvironment(request)
	loaded, err := LoadConfig(ctx, root, request.ConfigFile, environment)
	if err != nil {
		return preparedBuild{}, err
	}
	baseTemplateEnv := cloneStringMap(environment)
	templateValues, err := loadManifestTemplateValues(root, loaded.Config)
	if err != nil {
		return preparedBuild{}, err
	}
	for key, value := range templateValues {
		baseTemplateEnv[key] = value
	}
	firstPass, err := LoadConfig(ctx, root, request.ConfigFile, baseTemplateEnv)
	if err != nil {
		return preparedBuild{}, err
	}
	finalTemplateEnv := cloneStringMap(baseTemplateEnv)
	for key, value := range firstPass.BuildEnv {
		finalTemplateEnv[key] = value
	}
	loaded, err = LoadConfig(ctx, root, request.ConfigFile, finalTemplateEnv)
	if err != nil {
		return preparedBuild{}, err
	}
	if err := rejectRemovedOptions(loaded); err != nil {
		return preparedBuild{}, err
	}
	if request.RunBuildScript && strings.TrimSpace(loaded.Config.BuildScript) != "" {
		runner := request.Runner
		if runner == nil {
			runner = ShellRunner{}
		}
		commandEnv := cloneStringMap(environment)
		for key, value := range loaded.BuildEnv {
			commandEnv[key] = value
		}
		if err := runner.Run(ctx, Command{Script: loaded.Config.BuildScript, Dir: root, Env: commandEnv}); err != nil {
			return preparedBuild{}, &lpkgo.Error{Code: lpkgo.CodeCommandFailed, Op: "build.run_script", Cause: err}
		}
	}

	staging, err := os.MkdirTemp("", "lpk-go-build-*")
	if err != nil {
		return preparedBuild{}, &lpkgo.Error{Code: lpkgo.CodeCommandFailed, Op: "build.stage", Cause: err}
	}
	prepared := preparedBuild{root: staging, config: loaded, cleanup: func() error { return os.RemoveAll(staging) }}
	fail := func(err error) (preparedBuild, error) {
		_ = prepared.cleanup()
		return preparedBuild{}, err
	}

	manifestName := strings.TrimSpace(loaded.Config.Manifest)
	if manifestName == "" {
		manifestName = "lzc-manifest.yml"
	}
	manifestPath := resolveProjectPath(root, manifestName)
	packagePath := filepath.Join(root, "package.yml")
	manifestExists, err := regularFileExists(manifestPath)
	if err != nil {
		return fail(buildPathError("build.manifest", manifestPath, err))
	}
	packageExists, err := regularFileExists(packagePath)
	if err != nil {
		return fail(buildPathError("build.package_info", packagePath, err))
	}
	resourceOnly := len(loaded.Config.ResourceExports) > 0 && !manifestExists
	if !manifestExists && !resourceOnly {
		return fail(buildPathError("build.manifest", manifestPath, fs.ErrNotExist))
	}
	if resourceOnly && !packageExists {
		return fail(configError("build.package_info", packagePath, errors.New("package.yml is required for resource-only LPK packages")))
	}

	var manifestDocument *manifest.Document
	var processedManifest []byte
	if manifestExists {
		processed, preprocessErr := manifest.PreprocessFile(ctx, manifestPath, manifest.BuildContext{
			Profile: string(loaded.Profile),
			Env:     loaded.BuildEnv,
		})
		if preprocessErr != nil {
			return fail(preprocessErr)
		}
		processedManifest = processed
		manifestDocument, prepared.templated, err = parseManifestForBuild(processed)
		if err != nil {
			return fail(err)
		}
	}
	var packageDocument *manifest.Document
	if packageExists {
		data, readErr := os.ReadFile(packagePath)
		if readErr != nil {
			return fail(buildPathError("build.package_info", packagePath, readErr))
		}
		packageDocument, err = manifest.Parse(data)
		if err != nil {
			return fail(err)
		}
	}

	metadataTarget := packageDocument
	if metadataTarget == nil {
		metadataTarget = manifestDocument
	}
	if metadataTarget == nil {
		return fail(configError("build.metadata", loaded.Path, errors.New("missing metadata document")))
	}
	if err := applyMetadataOverrides(metadataTarget, loaded.Config, request.VersionOverride); err != nil {
		return fail(err)
	}

	var effective manifest.Effective
	if resourceOnly {
		var packageInfo manifest.PackageInfo
		if err := packageDocument.Decode(&packageInfo); err != nil {
			return fail(err)
		}
		prepared.packageID = strings.TrimSpace(packageInfo.Package)
		prepared.version = strings.TrimSpace(packageInfo.Version)
	} else {
		effective, err = manifest.LoadEffective(manifestDocument, packageDocument, false)
		if err != nil {
			return fail(err)
		}
		prepared.packageID = strings.TrimSpace(effective.Manifest.Package)
		prepared.version = strings.TrimSpace(effective.Manifest.Version)
		if strings.TrimSpace(effective.Manifest.Application.Subdomain) == "" {
			return fail(configError("build.manifest", manifestPath, errors.New("application.subdomain is required")))
		}
	}
	if !packageNamePattern.MatchString(prepared.packageID) {
		return fail(configError("build.metadata", metadataPath(resourceOnly, manifestPath, packagePath), fmt.Errorf("invalid package name %q", prepared.packageID)))
	}
	if prepared.version == "" {
		return fail(configError("build.metadata", metadataPath(resourceOnly, manifestPath, packagePath), errors.New("version is required")))
	}
	if hasConfiguredImages(loaded.Config.Images) {
		if request.ImageBuilder == nil {
			return fail(&lpkgo.Error{Code: lpkgo.CodeIncompatibleBackend, Op: "build.prepare_images", Path: filepath.ToSlash(loaded.Path), Cause: errors.New("image builder is required")})
		}
		artifact, buildErr := request.ImageBuilder.Build(ctx, ImageBuildRequest{
			Root:     root,
			Config:   loaded.Config.Images,
			Manifest: effective.Manifest,
		})
		if buildErr != nil {
			if artifact != nil {
				_ = artifact.Close()
			}
			return fail(&lpkgo.Error{Code: lpkgo.CodeCommandFailed, Op: "build.prepare_images", Cause: buildErr})
		}
		if artifact == nil {
			return fail(configError("build.prepare_images", loaded.Path, errors.New("image builder returned a nil artifact")))
		}
		artifactFS := artifact.FS()
		if artifactFS == nil {
			if artifact != nil {
				_ = artifact.Close()
			}
			return fail(configError("build.prepare_images", loaded.Path, errors.New("image builder returned a nil artifact")))
		}
		prepared.images, err = oci.Validate(ctx, artifactFS)
		if err == nil {
			err = copyFilesystem(ctx, artifactFS, staging)
		}
		closeErr := artifact.Close()
		if err != nil {
			return fail(err)
		}
		if closeErr != nil {
			return fail(&lpkgo.Error{Code: lpkgo.CodeCommandFailed, Op: "build.prepare_images.close", Cause: closeErr})
		}
	}

	prepared.layout = selectLayout(request, loaded, packageExists, resourceOnly)
	if prepared.layout == lpk.LayoutV1 {
		data := processedManifest
		if !prepared.templated {
			var bytesErr error
			data, bytesErr = manifestDocument.Bytes()
			if bytesErr != nil {
				return fail(bytesErr)
			}
		} else if strings.TrimSpace(request.VersionOverride) != "" {
			data = rewriteTopLevelScalar(data, "version", strings.TrimSpace(request.VersionOverride))
		}
		if err := os.WriteFile(filepath.Join(staging, "manifest.yml"), data, 0o644); err != nil {
			return fail(buildPathError("build.stage_manifest", manifestPath, err))
		}
	} else if resourceOnly {
		data, bytesErr := packageDocument.Bytes()
		if bytesErr != nil {
			return fail(bytesErr)
		}
		if err := os.WriteFile(filepath.Join(staging, "package.yml"), data, 0o644); err != nil {
			return fail(buildPathError("build.stage_package_info", packagePath, err))
		}
	} else {
		manifestOutput, packageOutput, splitErr := manifest.SplitEffective(manifestDocument, effective.PackageInfo, nil)
		if splitErr != nil {
			return fail(splitErr)
		}
		manifestBytes := processedManifest
		if prepared.templated {
			manifestBytes = removeTopLevelFields(manifestBytes, manifest.StaticPackageFields())
		} else {
			var bytesErr error
			manifestBytes, bytesErr = manifestOutput.Bytes()
			if bytesErr != nil {
				return fail(bytesErr)
			}
		}
		packageBytes, bytesErr := packageOutput.Bytes()
		if bytesErr != nil {
			return fail(bytesErr)
		}
		if err := os.WriteFile(filepath.Join(staging, "manifest.yml"), manifestBytes, 0o644); err != nil {
			return fail(buildPathError("build.stage_manifest", manifestPath, err))
		}
		if err := os.WriteFile(filepath.Join(staging, "package.yml"), packageBytes, 0o644); err != nil {
			return fail(buildPathError("build.stage_package_info", packagePath, err))
		}
	}
	if prepared.images.ImageCount > 0 {
		manifestOutputPath := filepath.Join(staging, "manifest.yml")
		if data, readErr := os.ReadFile(manifestOutputPath); readErr == nil {
			data, err = rewriteEmbeddedImages(data, prepared.images.ResolvedByAlias)
			if err != nil {
				return fail(err)
			}
			if err := os.WriteFile(manifestOutputPath, data, 0o644); err != nil {
				return fail(buildPathError("build.rewrite_images", manifestOutputPath, err))
			}
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return fail(buildPathError("build.rewrite_images", manifestOutputPath, readErr))
		}
	}

	warnings, err := collectProjectContent(ctx, root, staging, loaded.Config)
	if err != nil {
		return fail(err)
	}
	prepared.warnings = warnings
	if prepared.images.ImageCount > 0 {
		if err := gzipContentArchive(ctx, staging); err != nil {
			return fail(err)
		}
	}
	return prepared, nil
}
