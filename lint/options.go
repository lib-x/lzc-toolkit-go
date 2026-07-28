package lint

import (
	"io/fs"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

// Option configures optional lint profiles.
type Option func(*options)

type options struct {
	official      bool
	icon          iconOption
	imageBuilds   map[string]ImageBuild
	embeddedImage map[string]EmbeddedImage
}

type iconOption struct {
	path string
	root fs.FS
	name string
}

// ImageBuild describes the subset of lzc-build.yml images.<alias> used by
// lzc-cli 2.0.9 official publish linting.
type ImageBuild struct {
	UpstreamMatch string
}

// EmbeddedImage describes one images.lock alias for official publish linting.
type EmbeddedImage struct {
	Upstream string
	Layers   []EmbeddedLayer
}

// EmbeddedLayer describes one images.lock layer for official publish linting.
type EmbeddedLayer struct {
	Digest     string
	Source     string
	BlobExists bool
}

// WithOfficial enables LazyCat official/developer-platform lint warnings.
//
// These warnings mirror lzc-cli 2.0.9 store/pre-publish preferences such as
// registry.lazycat.cloud image refs, icon size, locales, and semver. They are
// disabled by default because they do not necessarily indicate an uninstallable
// LPK package.
func WithOfficial() Option {
	return func(options *options) {
		options.official = true
	}
}

// WithIconPath provides a source-project icon path for official manifest lint.
// lzc-cli's manifest lint only reports icon size here when the file exists.
func WithIconPath(path string) Option {
	return func(options *options) {
		options.icon = iconOption{path: path}
	}
}

// WithIconFS provides an icon inside an fs.FS for official manifest lint.
func WithIconFS(root fs.FS, name string) Option {
	return func(options *options) {
		options.icon = iconOption{root: root, name: name}
	}
}

// WithImageBuilds provides lzc-build.yml images.<alias> metadata for official
// embed:<alias> image linting.
func WithImageBuilds(images map[string]ImageBuild) Option {
	return func(options *options) {
		if len(images) == 0 {
			options.imageBuilds = nil
			return
		}
		copied := make(map[string]ImageBuild, len(images))
		for alias, image := range images {
			copied[alias] = image
		}
		options.imageBuilds = copied
	}
}

// WithEmbeddedImages provides images.lock metadata for official embed:<alias>
// image linting.
func WithEmbeddedImages(images map[string]EmbeddedImage) Option {
	return func(options *options) {
		if len(images) == 0 {
			options.embeddedImage = nil
			return
		}
		copied := make(map[string]EmbeddedImage, len(images))
		for alias, image := range images {
			layers := append([]EmbeddedLayer(nil), image.Layers...)
			image.Layers = layers
			copied[alias] = image
		}
		options.embeddedImage = copied
	}
}

// IsOfficialWarning reports whether warning is part of the optional LazyCat
// official/developer-platform profile.
func IsOfficialWarning(warning lpkgo.Warning) bool {
	code := strings.TrimSpace(warning.Code)
	return strings.HasPrefix(code, "store-") ||
		code == "lpk-icon-invalid" ||
		code == "lpk-devshell-disallowed"
}

func applyOptions(values []Option) options {
	var result options
	for _, apply := range values {
		if apply != nil {
			apply(&result)
		}
	}
	return result
}
