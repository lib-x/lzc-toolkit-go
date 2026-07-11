package build

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/lib-x/lzc-toolkit-go/manifest"
	"github.com/lib-x/lzc-toolkit-go/oci"
	"go.yaml.in/yaml/v3"
)

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

func parseManifestForBuild(data []byte) (*manifest.Document, manifest.Summary, bool, error) {
	analysis, err := manifest.Analyze(data)
	if err != nil {
		return nil, manifest.Summary{}, false, err
	}
	summary := analysis.Summary()
	if !summary.Template.Present {
		return analysis.Document(), summary, false, nil
	}
	compatible := compatibleTemplatedManifest(summary)
	encoded, err := yaml.Marshal(compatible)
	if err != nil {
		return nil, manifest.Summary{}, false, configError("build.template_manifest", "manifest.yml", err)
	}
	document, err := manifest.Parse(encoded)
	return document, summary, true, err
}

func compatibleTemplatedManifest(summary manifest.Summary) map[string]any {
	compatible := make(map[string]any)
	for field, value := range map[string]string{
		"package":     summary.Package.Package,
		"version":     summary.Package.Version,
		"name":        summary.Package.Name,
		"description": summary.Package.Description,
		"author":      summary.Package.Author,
		"license":     summary.Package.License,
		"homepage":    summary.Package.Homepage,
	} {
		if value != "" {
			compatible[field] = value
		}
	}
	application := map[string]any{"subdomain": summary.Application.Subdomain}
	services := make(map[string]any)
	for _, service := range summary.Services {
		if _, exists := services[service.Name]; !exists {
			services[service.Name] = map[string]any{}
		}
	}
	for _, image := range summary.Images {
		if !image.Editable || image.RuntimeRef == "" {
			continue
		}
		if image.Target == "application" {
			application["image"] = image.RuntimeRef
			continue
		}
		service, exists := services[image.Service].(map[string]any)
		if exists {
			service["image"] = image.RuntimeRef
		}
	}
	compatible["application"] = application
	if len(services) > 0 {
		compatible["services"] = services
	}
	return compatible
}

func validateTemplateImageBuild(summary manifest.Summary) error {
	if !summary.Template.Present {
		return nil
	}
	for _, image := range summary.Images {
		if image.Templated || image.EmbeddedAlias != "" && !image.Editable {
			return configError("build.prepare_images", "manifest.yml", fmt.Errorf("image target %q is templated or conditional", imageTargetName(image)))
		}
	}
	return nil
}

func imageTargetName(image manifest.ImageSummary) string {
	if image.Target == "application" {
		return "application.image"
	}
	return "services." + image.Service + ".image"
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
