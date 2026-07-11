package lint

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/manifest"
	"go.yaml.in/yaml/v3"
)

// Package reports compatibility warnings for an extracted LPK package root.
//
// By default this reports package/manifest compatibility warnings only. Pass
// WithOfficial to additionally report LazyCat official/developer-platform
// pre-publish warnings, matching lzc-cli 2.0.8's lpk lint distinction where
// official registry, icon, locales, and semver rules are warnings rather than
// package construction errors.
func Package(ctx context.Context, root fs.FS, optionValues ...Option) ([]lpkgo.Warning, error) {
	if err := lintContextError(ctx, "lint.package"); err != nil {
		return nil, err
	}
	if root == nil {
		return nil, &lpkgo.Error{Code: lpkgo.CodeInvalidArgument, Op: "lint.package", Cause: errors.New("nil filesystem")}
	}
	if isResourceOnlyPackageRoot(root) {
		return ResourcePackage(ctx, root)
	}

	options := applyOptions(optionValues)
	manifestDocument, err := readRequiredManifestDocument(ctx, root)
	if err != nil {
		return nil, err
	}
	packageDocument, hasPackageDocument, err := readOptionalPackageDocument(ctx, root, "package.yml")
	if err != nil {
		return nil, err
	}
	effective, err := manifest.LoadEffective(manifestDocument, packageDocument, hasPackageDocument)
	if err != nil {
		return nil, err
	}

	warnings := make([]lpkgo.Warning, 0)
	if options.official {
		precheckWarnings, err := collectOfficialPackagePrecheckWarnings(ctx, root)
		if err != nil {
			return nil, err
		}
		warnings = append(warnings, precheckWarnings...)
		options.icon = iconOption{root: root, name: "icon.png"}
		embeddedImages, err := readOfficialEmbeddedImages(ctx, root)
		if err != nil {
			return nil, err
		}
		options.embeddedImage = embeddedImages
	}
	manifestWarnings, err := Manifest(effective.Source, effective.Manifest, optionsFromStruct(options)...)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, manifestWarnings...)
	return warnings, nil
}

func readRequiredManifestDocument(ctx context.Context, root fs.FS) (*manifest.Document, error) {
	if err := lintContextError(ctx, "lint.package"); err != nil {
		return nil, err
	}
	info, err := fs.Stat(root, "manifest.yml")
	if err != nil || !info.Mode().IsRegular() {
		return nil, packageLintError("manifest.yml", err)
	}
	data, err := fs.ReadFile(root, "manifest.yml")
	if err != nil {
		return nil, packageLintError("manifest.yml", err)
	}
	if err := lintContextError(ctx, "lint.package"); err != nil {
		return nil, err
	}
	analysis, err := manifest.Analyze(data)
	if err != nil {
		return nil, err
	}
	return analysis.Document(), nil
}

func optionsFromStruct(value options) []Option {
	return []Option{func(options *options) {
		*options = value
	}}
}

func isResourceOnlyPackageRoot(root fs.FS) bool {
	if _, err := fs.Stat(root, "manifest.yml"); err == nil {
		return false
	}
	info, err := fs.Stat(root, "exports")
	return err == nil && info.IsDir()
}

func readRequiredPackageDocument(ctx context.Context, root fs.FS, name string) (*manifest.Document, error) {
	document, found, err := readPackageDocument(ctx, root, name)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, packageLintError(name, fs.ErrNotExist)
	}
	return document, nil
}

func readOptionalPackageDocument(ctx context.Context, root fs.FS, name string) (*manifest.Document, bool, error) {
	document, found, err := readPackageDocument(ctx, root, name)
	return document, found, err
}

func readPackageDocument(ctx context.Context, root fs.FS, name string) (*manifest.Document, bool, error) {
	if err := lintContextError(ctx, "lint.package"); err != nil {
		return nil, false, err
	}
	info, err := fs.Stat(root, name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return nil, false, packageLintError(name, err)
	}
	data, err := fs.ReadFile(root, name)
	if err != nil {
		return nil, false, packageLintError(name, err)
	}
	if err := lintContextError(ctx, "lint.package"); err != nil {
		return nil, false, err
	}
	document, err := manifest.Parse(data)
	if err != nil {
		return nil, false, err
	}
	return document, true, nil
}

func collectOfficialPackagePrecheckWarnings(ctx context.Context, root fs.FS) ([]lpkgo.Warning, error) {
	warnings := make([]lpkgo.Warning, 0, 2)
	if err := lintContextError(ctx, "lint.package"); err != nil {
		return nil, err
	}
	if info, err := fs.Stat(root, "devshell"); err == nil && info.Mode().IsRegular() {
		warnings = append(warnings, officialWarning(
			"lpk-devshell-disallowed",
			"devshell",
			"Cannot publish a devshell package. Rebuild it with lzc-cli project build.",
		))
	}
	if !officialPackageIconValid(root) {
		warnings = append(warnings, officialWarning(
			"lpk-icon-invalid",
			"icon.png",
			"icon.png must exist and be a PNG file.",
		))
	}
	return warnings, nil
}

func officialPackageIconValid(root fs.FS) bool {
	info, err := fs.Stat(root, "icon.png")
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	data, err := fs.ReadFile(root, "icon.png")
	return err == nil && isPNG(data)
}

func isPNG(data []byte) bool {
	return len(data) >= 8 &&
		data[0] == 0x89 &&
		data[1] == 0x50 &&
		data[2] == 0x4e &&
		data[3] == 0x47 &&
		data[4] == 0x0d &&
		data[5] == 0x0a &&
		data[6] == 0x1a &&
		data[7] == 0x0a
}

type officialImagesLock struct {
	Images map[string]officialImagesLockImage `yaml:"images"`
}

type officialImagesLockImage struct {
	Upstream string                    `yaml:"upstream"`
	Layers   []officialImagesLockLayer `yaml:"layers"`
}

type officialImagesLockLayer struct {
	Digest string `yaml:"digest"`
	Source string `yaml:"source"`
}

func readOfficialEmbeddedImages(ctx context.Context, root fs.FS) (map[string]EmbeddedImage, error) {
	if err := lintContextError(ctx, "lint.package"); err != nil {
		return nil, err
	}
	info, err := fs.Stat(root, "images.lock")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return nil, packageLintError("images.lock", err)
	}
	data, err := fs.ReadFile(root, "images.lock")
	if err != nil {
		return nil, packageLintError("images.lock", err)
	}
	if err := lintContextError(ctx, "lint.package"); err != nil {
		return nil, err
	}
	var lock officialImagesLock
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return nil, packageLintError("images.lock", fmt.Errorf("invalid images.lock YAML"))
	}
	images := make(map[string]EmbeddedImage, len(lock.Images))
	for _, alias := range sortedOfficialImageAliases(lock.Images) {
		image := lock.Images[alias]
		layers := make([]EmbeddedLayer, 0, len(image.Layers))
		for _, layer := range image.Layers {
			digest := strings.ToLower(strings.TrimSpace(layer.Digest))
			digestHex := ""
			if strings.HasPrefix(digest, "sha256:") {
				digestHex = strings.TrimPrefix(digest, "sha256:")
			}
			blobExists := false
			if digestHex != "" {
				if _, err := fs.Stat(root, "images/blobs/sha256/"+digestHex); err == nil {
					blobExists = true
				}
			}
			layers = append(layers, EmbeddedLayer{
				Digest:     digest,
				Source:     layer.Source,
				BlobExists: blobExists,
			})
		}
		images[alias] = EmbeddedImage{
			Upstream: strings.TrimSpace(image.Upstream),
			Layers:   layers,
		}
	}
	return images, nil
}

func sortedOfficialImageAliases(images map[string]officialImagesLockImage) []string {
	keys := make([]string, 0, len(images))
	for key := range images {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func packageLintError(path string, cause error) error {
	if cause == nil {
		cause = errors.New("package traversal failed")
	}
	return &lpkgo.Error{Code: lpkgo.CodeInvalidManifest, Op: "lint.package", Path: path, Cause: cause}
}
