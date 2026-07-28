package lint

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/manifest"
)

const (
	// OfficialIconMaxBytes is lzc-cli 2.0.9's App Store icon limit.
	OfficialIconMaxBytes = 200 * 1024
	// OfficialImageRegistryPrefix is lzc-cli 2.0.9's official image prefix.
	OfficialImageRegistryPrefix = "registry.lazycat.cloud"
)

var officialSemverPattern = regexp.MustCompile(`^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9A-Za-z-][0-9A-Za-z-]*))*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

type officialImageRef struct {
	path  string
	value string
}

func collectOfficialWarnings(root map[string]any, typed manifest.Manifest, options options) []lpkgo.Warning {
	warnings := make([]lpkgo.Warning, 0)
	warnings = append(warnings, collectOfficialVersionWarnings(root, typed)...)
	if !hasOfficialLocales(root, typed) {
		warnings = append(warnings, officialWarning("store-locales-required", "locales", "LazyCat official submission requires locales."))
	}
	warnings = append(warnings, collectOfficialImageWarnings(root, options)...)
	warnings = append(warnings, collectOfficialIconSizeWarnings(options.icon)...)
	return warnings
}

func collectOfficialVersionWarnings(root map[string]any, typed manifest.Manifest) []lpkgo.Warning {
	version := normalizeLintString(typed.Version)
	if version == "" {
		version = normalizeLintString(root["version"])
	}
	if version == "" {
		return nil
	}
	if officialSemverPattern.MatchString(version) {
		return nil
	}
	return []lpkgo.Warning{
		officialWarning(
			"store-version-invalid-semver",
			"version",
			fmt.Sprintf("LazyCat official submission requires a valid semver version. Current version is %q.", version),
		),
	}
}

func hasOfficialLocales(root map[string]any, typed manifest.Manifest) bool {
	if typed.Locales != nil {
		return true
	}
	_, found := root["locales"]
	return found
}

func collectOfficialImageWarnings(root map[string]any, options options) []lpkgo.Warning {
	warnings := make([]lpkgo.Warning, 0)
	for _, ref := range collectOfficialImageRefs(root) {
		embedAlias := parseOfficialEmbedAlias(ref.value)
		if embedAlias != "" {
			warnings = append(warnings, collectOfficialEmbedImageWarning(ref, embedAlias, options)...)
			continue
		}
		if !strings.HasPrefix(ref.value, OfficialImageRegistryPrefix) {
			warnings = append(warnings, officialWarning(
				"store-image-registry-invalid",
				ref.path,
				fmt.Sprintf("LazyCat official submission requires %s to start with %s. Current value is %q.", ref.path, OfficialImageRegistryPrefix, ref.value),
			))
		}
	}
	return warnings
}

func collectOfficialEmbedImageWarning(ref officialImageRef, alias string, options options) []lpkgo.Warning {
	image, hasLockEntry := options.embeddedImage[alias]
	upstream := normalizeLintString(image.Upstream)
	if hasLockEntry && upstream != "" {
		if !strings.HasPrefix(upstream, OfficialImageRegistryPrefix) {
			return []lpkgo.Warning{officialWarning(
				"store-image-embed-upstream-invalid",
				ref.path,
				fmt.Sprintf("LazyCat official submission requires %s to use upstream images from %s. Current upstream for %q is %q.", ref.path, OfficialImageRegistryPrefix, alias, upstream),
			)}
		}
		return nil
	}
	if isFullyEmbeddedOfficialImage(hasLockEntry, image) {
		return nil
	}
	missingDigests := collectMissingOfficialBlobDigests(hasLockEntry, image)
	if len(missingDigests) > 0 {
		return []lpkgo.Warning{officialWarning(
			"store-image-embed-blob-missing",
			ref.path,
			fmt.Sprintf("LazyCat official submission requires embedded image blobs for %s. Embed alias %q is missing blobs: %s.", ref.path, alias, strings.Join(missingDigests, ", ")),
		)}
	}
	buildConfig, hasBuildConfig := options.imageBuilds[alias]
	if !hasBuildConfig {
		return []lpkgo.Warning{officialWarning(
			"store-image-embed-alias-missing",
			ref.path,
			fmt.Sprintf("LazyCat official submission requires %s to resolve to %s. Embed alias %q has no matching images.%s config.", ref.path, OfficialImageRegistryPrefix, alias, alias),
		)}
	}
	upstreamMatch := normalizeLintString(buildConfig.UpstreamMatch)
	if upstreamMatch == "" {
		upstreamMatch = OfficialImageRegistryPrefix
	}
	if !strings.HasPrefix(upstreamMatch, OfficialImageRegistryPrefix) {
		return []lpkgo.Warning{officialWarning(
			"store-image-embed-upstream-invalid",
			ref.path,
			fmt.Sprintf("LazyCat official submission requires %s to use upstream images from %s. Current images.%s.upstream-match is %q.", ref.path, OfficialImageRegistryPrefix, alias, upstreamMatch),
		)}
	}
	return nil
}

func isFullyEmbeddedOfficialImage(hasLockEntry bool, image EmbeddedImage) bool {
	if !hasLockEntry || normalizeLintString(image.Upstream) != "" || len(image.Layers) == 0 {
		return false
	}
	for _, layer := range image.Layers {
		if strings.ToLower(normalizeLintString(layer.Source)) != "embed" || !layer.BlobExists {
			return false
		}
	}
	return true
}

func collectMissingOfficialBlobDigests(hasLockEntry bool, image EmbeddedImage) []string {
	if !hasLockEntry || normalizeLintString(image.Upstream) != "" || len(image.Layers) == 0 {
		return nil
	}
	missing := make([]string, 0)
	for _, layer := range image.Layers {
		if strings.ToLower(normalizeLintString(layer.Source)) != "embed" {
			return nil
		}
	}
	for _, layer := range image.Layers {
		digest := normalizeLintString(layer.Digest)
		if digest != "" && !layer.BlobExists {
			missing = append(missing, digest)
		}
	}
	return missing
}

func collectOfficialImageRefs(root map[string]any) []officialImageRef {
	refs := make([]officialImageRef, 0)
	application, _ := stringMap(root["application"])
	if value := normalizeLintString(application["image"]); value != "" {
		refs = append(refs, officialImageRef{path: "application.image", value: value})
	}
	services, _ := stringMap(root["services"])
	serviceNames := sortedKeys(services)
	for _, serviceName := range serviceNames {
		service, _ := stringMap(services[serviceName])
		if value := normalizeLintString(service["image"]); value != "" {
			refs = append(refs, officialImageRef{path: "services." + serviceName + ".image", value: value})
		}
	}
	return refs
}

func parseOfficialEmbedAlias(imageRef string) string {
	value := normalizeLintString(imageRef)
	if !strings.HasPrefix(value, "embed:") {
		return ""
	}
	rest := strings.TrimSpace(strings.TrimPrefix(value, "embed:"))
	if index := strings.Index(rest, "@"); index >= 0 {
		rest = rest[:index]
	}
	return normalizeLintString(rest)
}

func collectOfficialIconSizeWarnings(icon iconOption) []lpkgo.Warning {
	size, name, ok := statOfficialIcon(icon)
	if !ok || size <= OfficialIconMaxBytes {
		return nil
	}
	return []lpkgo.Warning{officialWarning(
		"store-icon-too-large",
		name,
		fmt.Sprintf("LazyCat official submission requires icon.png smaller than 200 KiB. Current icon source is %d bytes.", size),
	)}
}

func statOfficialIcon(icon iconOption) (int64, string, bool) {
	switch {
	case strings.TrimSpace(icon.path) != "":
		info, err := os.Stat(icon.path)
		if err != nil || !info.Mode().IsRegular() {
			return 0, "", false
		}
		return info.Size(), officialIconWarningPath(filepath.Base(icon.path)), true
	case icon.root != nil && strings.TrimSpace(icon.name) != "":
		info, err := fs.Stat(icon.root, icon.name)
		if err != nil || !info.Mode().IsRegular() {
			return 0, "", false
		}
		return info.Size(), officialIconWarningPath(icon.name), true
	default:
		return 0, "", false
	}
}

func officialIconWarningPath(name string) string {
	name = strings.TrimSpace(filepath.ToSlash(name))
	if name == "" || name == "." {
		return "icon.png"
	}
	return name
}

func officialWarning(code string, path string, message string) lpkgo.Warning {
	return lpkgo.Warning{Code: code, Severity: lpkgo.SeverityWarning, Path: path, Message: message}
}

func normalizeLintString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
