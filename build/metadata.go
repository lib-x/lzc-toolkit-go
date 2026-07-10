package build

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/lib-x/lzc-toolkit-go/manifest"
	"github.com/lib-x/lzc-toolkit-go/oci"
	"go.yaml.in/yaml/v3"
)

var packageNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*(\.[a-z][a-z0-9]*(-[a-z0-9]+)*)*$`)

func applyMetadataOverrides(document *manifest.Document, config Config, versionOverride string) error {
	overrides := cloneMap(config.PackageOverride)
	if config.PackageID != nil {
		if _, exists := overrides["package"]; !exists {
			overrides["package"] = strings.TrimSpace(*config.PackageID)
		}
	}
	if config.PackageName != nil {
		if _, exists := overrides["name"]; !exists {
			overrides["name"] = strings.TrimSpace(*config.PackageName)
		}
	}
	for field, value := range overrides {
		if value == nil {
			document.Delete(field)
			continue
		}
		if err := document.Set(value, field); err != nil {
			return err
		}
	}
	if strings.TrimSpace(versionOverride) != "" {
		return document.Set(strings.TrimSpace(versionOverride), "version")
	}
	return nil
}

func metadataPath(resourceOnly bool, manifestPath, packagePath string) string {
	if resourceOnly {
		return packagePath
	}
	return manifestPath
}

func loadManifestTemplateValues(root string, config Config) (map[string]string, error) {
	manifestName := strings.TrimSpace(config.Manifest)
	if manifestName == "" {
		manifestName = "lzc-manifest.yml"
	}
	values := make(map[string]any)
	manifestPath := resolveProjectPath(root, manifestName)
	if data, err := os.ReadFile(manifestPath); err == nil {
		if err := yaml.Unmarshal(data, &values); err != nil {
			if !isGoTemplate(data) {
				return nil, configError("build.template_manifest", manifestPath, err)
			}
			values = fakeManifestValues(data)
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
		mergeTopLevel(values, packageValues)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, buildPathError("build.template_package", packagePath, err)
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case nil:
			result[key] = ""
		case string:
			result[key] = typed
		case bool, int, int64, uint64, float64:
			result[key] = fmt.Sprint(typed)
		}
	}
	return result, nil
}

func parseManifestForBuild(data []byte) (*manifest.Document, bool, error) {
	document, err := manifest.Parse(data)
	if err == nil {
		return document, false, nil
	}
	if !isGoTemplate(data) {
		return nil, false, err
	}
	values := fakeManifestValues(data)
	compatible := map[string]any{"application": map[string]any{"subdomain": values["subdomain"]}}
	for _, field := range []string{"package", "version", "name"} {
		if value, exists := values[field]; exists && value != "" {
			compatible[field] = value
		}
	}
	encoded, err := yaml.Marshal(compatible)
	if err != nil {
		return nil, false, configError("build.template_manifest", "manifest.yml", err)
	}
	document, err = manifest.Parse(encoded)
	return document, true, err
}

func isGoTemplate(data []byte) bool {
	return bytes.Contains(data, []byte("{{")) && bytes.Contains(data, []byte("}}"))
}

func fakeManifestValues(data []byte) map[string]any {
	values := make(map[string]any)
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Count(trimmed, ":") != 1 {
			continue
		}
		key, value, _ := strings.Cut(trimmed, ":")
		if key != "package" && key != "version" && key != "name" && key != "subdomain" {
			continue
		}
		value = strings.TrimSpace(strings.SplitN(value, " #", 2)[0])
		if _, exists := values[key]; !exists && value != "" {
			values[key] = value
		}
	}
	return values
}

func removeTopLevelFields(data []byte, fields []string) []byte {
	wanted := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		wanted[field] = struct{}{}
	}
	lines := strings.Split(string(data), "\n")
	output := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent == 0 && trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "{{") {
			key, _, found := strings.Cut(trimmed, ":")
			_, remove := wanted[key]
			if found && remove {
				skipping = true
				continue
			}
			skipping = false
		}
		if !skipping {
			output = append(output, line)
		}
	}
	return []byte(strings.Join(output, "\n"))
}

func rewriteTopLevelScalar(data []byte, field, value string) []byte {
	lines := strings.Split(string(data), "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, field+":") {
			lines[index] = field + ": " + value
			break
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

func rewriteEmbeddedImages(data []byte, resolved map[string]string) ([]byte, error) {
	aliases := make([]string, 0, len(resolved))
	for alias := range resolved {
		aliases = append(aliases, alias)
	}
	sort.Slice(aliases, func(i, j int) bool {
		if len(aliases[i]) == len(aliases[j]) {
			return aliases[i] < aliases[j]
		}
		return len(aliases[i]) > len(aliases[j])
	})
	output := string(data)
	for _, alias := range aliases {
		digest, err := oci.ParseDigest(resolved[alias])
		if err != nil {
			return nil, configError("build.rewrite_images", "manifest.yml", fmt.Errorf("invalid resolved image for alias %q", alias))
		}
		pattern := regexp.MustCompile(`embed:` + regexp.QuoteMeta(alias) + `(?:@sha256:[0-9a-fA-F]{64})?([^A-Za-z0-9._-]|$)`)
		output = pattern.ReplaceAllString(output, "embed:"+alias+"@"+digest.String()+"$1")
	}
	return []byte(output), nil
}
