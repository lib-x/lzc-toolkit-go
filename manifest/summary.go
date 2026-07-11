package manifest

import (
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
)

type Diagnostic struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Path     string   `json:"path"`
	Message  string   `json:"message"`
}

type PackageSummary struct {
	Package              string   `json:"id"`
	Version              string   `json:"version"`
	Name                 string   `json:"name"`
	Description          string   `json:"description"`
	Author               string   `json:"author"`
	License              string   `json:"license"`
	Homepage             string   `json:"homepage"`
	MinOSVersion         string   `json:"minOsVersion"`
	UnsupportedPlatforms []string `json:"unsupportedPlatforms"`
	LocaleCodes          []string `json:"localeCodes"`
}

type ApplicationSummary struct {
	Subdomain      string   `json:"subdomain"`
	HasImage       bool     `json:"hasImage"`
	HasServices    bool     `json:"hasServices"`
	HasExecLaunch  bool     `json:"hasExecLaunch"`
	BackgroundTask bool     `json:"backgroundTask"`
	MultiInstance  bool     `json:"multiInstance"`
	HasRoutes      bool     `json:"hasRoutes"`
	HasUpstreams   bool     `json:"hasUpstreams"`
	HasIngress     bool     `json:"hasIngress"`
	HasEntries     bool     `json:"hasEntries"`
	HasInjects     bool     `json:"hasInjects"`
	HasPublicPath  bool     `json:"hasPublicPath"`
	HasPermissions bool     `json:"hasPermissions"`
	Permissions    []string `json:"permissions"`
}

type ServiceSummary struct {
	Name           string   `json:"name"`
	Conditional    bool     `json:"conditional"`
	HasImage       bool     `json:"hasImage"`
	HasHealthcheck bool     `json:"hasHealthcheck"`
	HasBinds       bool     `json:"hasBinds"`
	DependsOn      []string `json:"dependsOn"`
}

type ImageSummary struct {
	Target        string `json:"target"`
	Service       string `json:"service"`
	RuntimeRef    string `json:"runtimeRef"`
	UpstreamRef   string `json:"upstreamRef"`
	EmbeddedAlias string `json:"embeddedAlias"`
	Templated     bool   `json:"templated"`
	Conditional   bool   `json:"conditional"`
	Editable      bool   `json:"editable"`
	Reason        string `json:"reason"`
}

type Summary struct {
	Package     PackageSummary     `json:"package"`
	Application ApplicationSummary `json:"application"`
	Services    []ServiceSummary   `json:"services"`
	Images      []ImageSummary     `json:"images"`
	Template    TemplateInfo       `json:"template"`
	Diagnostics []Diagnostic       `json:"diagnostics"`
}

type summaryBuilder struct {
	analysis    *Analysis
	summary     Summary
	serviceSeen map[string]int
	imageSeen   map[string]int
}

// Summary extracts static, normalized facts from the projected manifest.
// It never decodes the whole source into Manifest, so duplicate and templated
// YAML structures remain inspectable without executing template actions.
func (analysis *Analysis) Summary() Summary {
	if analysis == nil || analysis.document == nil || analysis.document.root == nil {
		return Summary{}
	}
	builder := summaryBuilder{
		analysis:    analysis,
		serviceSeen: make(map[string]int),
		imageSeen:   make(map[string]int),
	}
	builder.summary.Template = analysis.Template()
	builder.extract(documentContent(analysis.document.root))
	builder.finish()
	return cloneSummary(builder.summary)
}

func (builder *summaryBuilder) extract(root *yaml.Node) {
	if root == nil || root.Kind != yaml.MappingNode {
		return
	}
	for _, pair := range allMappingPairs(root) {
		switch pair.key.Value {
		case "package":
			builder.setPackageString(&builder.summary.Package.Package, pair.value, "package")
		case "version":
			builder.setPackageString(&builder.summary.Package.Version, pair.value, "version")
		case "name":
			builder.setPackageString(&builder.summary.Package.Name, pair.value, "name")
		case "description":
			builder.setPackageString(&builder.summary.Package.Description, pair.value, "description")
		case "author":
			builder.setPackageString(&builder.summary.Package.Author, pair.value, "author")
		case "license":
			builder.setPackageString(&builder.summary.Package.License, pair.value, "license")
		case "homepage":
			builder.setPackageString(&builder.summary.Package.Homepage, pair.value, "homepage")
		case "min_os_version":
			builder.setPackageString(&builder.summary.Package.MinOSVersion, pair.value, "min_os_version")
		case "unsupported_platforms":
			if values, ok := staticStringList(pair.value); ok {
				builder.summary.Package.UnsupportedPlatforms = append(builder.summary.Package.UnsupportedPlatforms, values...)
			} else if nodeHasExpressionMarker(pair.value) {
				builder.templatedField("unsupported_platforms")
			}
		case "locales":
			builder.extractLocales(pair.value)
		case "application":
			builder.extractApplication(pair.value)
		case "ext_config":
			builder.extractExtConfig(pair.value)
		case "services":
			if pair.value.Kind == yaml.MappingNode {
				builder.summary.Application.HasServices = true
				builder.extractServices(pair.value)
			}
		}
	}
}

func (builder *summaryBuilder) setPackageString(target *string, node *yaml.Node, path string) {
	if *target != "" {
		return
	}
	if value, ok := staticString(node); ok {
		*target = value
	} else if nodeHasExpressionMarker(node) {
		builder.templatedField(path)
	}
}

func (builder *summaryBuilder) extractLocales(node *yaml.Node) {
	if node.Kind != yaml.MappingNode {
		return
	}
	for _, pair := range allMappingPairs(node) {
		if value, ok := staticString(pair.key); ok && value != "" {
			builder.summary.Package.LocaleCodes = append(builder.summary.Package.LocaleCodes, value)
		}
	}
}

func (builder *summaryBuilder) extractApplication(node *yaml.Node) {
	if node.Kind != yaml.MappingNode {
		return
	}
	for _, pair := range allMappingPairs(node) {
		switch pair.key.Value {
		case "subdomain":
			if builder.summary.Application.Subdomain == "" {
				if value, ok := staticString(pair.value); ok {
					builder.summary.Application.Subdomain = value
				} else if nodeHasExpressionMarker(pair.value) {
					builder.templatedField("application.subdomain")
				}
			}
		case "image":
			builder.summary.Application.HasImage = true
			builder.addImage("application.image", "", pair.key, pair.value, builder.conditional(pair.key, pair.value))
		case "background_task":
			if value, ok := staticBool(pair.value); ok && value {
				builder.summary.Application.BackgroundTask = true
			}
		case "multi_instance":
			if value, ok := staticBool(pair.value); ok && value {
				builder.summary.Application.MultiInstance = true
			}
		case "routes":
			builder.summary.Application.HasRoutes = true
			if containsStaticPrefix(pair.value, "exec://") {
				builder.summary.Application.HasExecLaunch = true
			}
		case "upstreams":
			builder.summary.Application.HasUpstreams = true
			if hasNonEmptyMappingScalarRecursive(pair.value, "backend_launch_command") {
				builder.summary.Application.HasExecLaunch = true
			}
		case "ingress":
			builder.summary.Application.HasIngress = true
		case "entries":
			builder.summary.Application.HasEntries = true
		case "injects":
			builder.summary.Application.HasInjects = true
		case "public_path":
			builder.summary.Application.HasPublicPath = true
		}
	}
}

func (builder *summaryBuilder) extractExtConfig(node *yaml.Node) {
	if node.Kind != yaml.MappingNode {
		return
	}
	for _, pair := range allMappingPairs(node) {
		if pair.key.Value != "permissions" {
			continue
		}
		builder.summary.Application.HasPermissions = true
		permissions, ok := staticStringList(pair.value)
		if ok {
			builder.summary.Application.Permissions = append(builder.summary.Application.Permissions, permissions...)
			continue
		}
		builder.addDiagnostic(SeverityWarning, "UNSUPPORTED_PERMISSIONS_SHAPE", "ext_config.permissions", "permissions are present in a non-scalar-list form")
	}
}

func (builder *summaryBuilder) extractServices(node *yaml.Node) {
	for _, pair := range allMappingPairs(node) {
		name, ok := staticString(pair.key)
		if !ok || name == "" || pair.value.Kind != yaml.MappingNode {
			continue
		}
		service := ServiceSummary{Name: name, Conditional: builder.conditional(pair.key, pair.value)}
		builder.serviceSeen[name]++
		for _, field := range allMappingPairs(pair.value) {
			switch field.key.Value {
			case "image":
				service.HasImage = true
				builder.addImage("services."+name+".image", name, field.key, field.value, service.Conditional || builder.conditional(field.key, field.value))
			case "healthcheck":
				service.HasHealthcheck = true
			case "health_check":
				service.HasHealthcheck = true
				builder.addDiagnostic(SeverityWarning, "LEGACY_HEALTH_CHECK", "services."+name+".health_check", "legacy health_check spelling is present")
			case "binds":
				service.HasBinds = true
			case "depends_on":
				service.DependsOn = append(service.DependsOn, dependencyNames(field.value)...)
			}
		}
		service.DependsOn = sortedUnique(service.DependsOn)
		builder.summary.Services = append(builder.summary.Services, service)
	}
}

func (builder *summaryBuilder) addImage(target string, service string, key *yaml.Node, value *yaml.Node, conditional bool) {
	templated := nodeHasExpressionMarker(value)
	runtimeRef, _ := staticString(value)
	image := ImageSummary{
		Target:        target,
		Service:       service,
		RuntimeRef:    runtimeRef,
		UpstreamRef:   upstreamComment(key, value),
		EmbeddedAlias: embeddedAlias(runtimeRef),
		Templated:     templated,
		Conditional:   conditional,
		Editable:      !templated && !conditional,
	}
	if templated {
		image.Reason = "image reference is templated"
		builder.addDiagnostic(SeverityWarning, "TEMPLATED_IMAGE", target, "image reference contains a template expression")
	}
	if conditional {
		image.Reason = appendReason(image.Reason, "image field is conditional")
		builder.addDiagnostic(SeverityWarning, "CONDITIONAL_IMAGE", target, "image field is inside a conditional template block")
	}
	builder.imageSeen[target]++
	builder.summary.Images = append(builder.summary.Images, image)
}

func (builder *summaryBuilder) conditional(nodes ...*yaml.Node) bool {
	for _, node := range nodes {
		if node != nil && builder.analysis.lineDepth[node.Line] > 0 {
			return true
		}
	}
	return false
}

func (builder *summaryBuilder) templatedField(path string) {
	builder.addDiagnostic(SeverityWarning, "TEMPLATED_FIELD", path, "field contains a template expression")
}

func (builder *summaryBuilder) addDiagnostic(severity Severity, code string, path string, message string) {
	builder.summary.Diagnostics = append(builder.summary.Diagnostics, Diagnostic{Severity: severity, Code: code, Path: path, Message: message})
}

func (builder *summaryBuilder) finish() {
	for name, count := range builder.serviceSeen {
		if count > 1 {
			builder.addDiagnostic(SeverityWarning, "DUPLICATE_SERVICE", "services."+name, "service is defined more than once")
		}
	}
	for target, count := range builder.imageSeen {
		if count <= 1 {
			continue
		}
		builder.addDiagnostic(SeverityWarning, "DUPLICATE_IMAGE_TARGET", target, "image target is defined more than once")
		for index := range builder.summary.Images {
			if builder.summary.Images[index].Target == target {
				builder.summary.Images[index].Editable = false
				builder.summary.Images[index].Reason = appendReason(builder.summary.Images[index].Reason, "image target is duplicated")
			}
		}
	}
	for index := range builder.summary.Services {
		if builder.serviceSeen[builder.summary.Services[index].Name] > 1 {
			// Duplicate service definitions are ambiguous even if their images
			// happen to differ in source shape.
			for imageIndex := range builder.summary.Images {
				if builder.summary.Images[imageIndex].Service == builder.summary.Services[index].Name {
					builder.summary.Images[imageIndex].Editable = false
					builder.summary.Images[imageIndex].Reason = appendReason(builder.summary.Images[imageIndex].Reason, "service is duplicated")
				}
			}
		}
	}
	builder.summary.Package.LocaleCodes = sortedUnique(builder.summary.Package.LocaleCodes)
	builder.summary.Package.UnsupportedPlatforms = sortedUnique(builder.summary.Package.UnsupportedPlatforms)
	builder.summary.Application.Permissions = sortedUnique(builder.summary.Application.Permissions)
	sort.SliceStable(builder.summary.Services, func(i, j int) bool {
		return builder.summary.Services[i].Name < builder.summary.Services[j].Name
	})
	sort.SliceStable(builder.summary.Images, func(i, j int) bool {
		if builder.summary.Images[i].Target != builder.summary.Images[j].Target {
			return builder.summary.Images[i].Target < builder.summary.Images[j].Target
		}
		if builder.summary.Images[i].RuntimeRef != builder.summary.Images[j].RuntimeRef {
			return builder.summary.Images[i].RuntimeRef < builder.summary.Images[j].RuntimeRef
		}
		return builder.summary.Images[i].UpstreamRef < builder.summary.Images[j].UpstreamRef
	})
	sort.SliceStable(builder.summary.Diagnostics, func(i, j int) bool {
		left, right := builder.summary.Diagnostics[i], builder.summary.Diagnostics[j]
		if left.Severity != right.Severity {
			return left.Severity < right.Severity
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.Message < right.Message
	})
}

type yamlPair struct {
	key   *yaml.Node
	value *yaml.Node
}

func allMappingPairs(node *yaml.Node) []yamlPair {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	pairs := make([]yamlPair, 0, len(node.Content)/2)
	for index := 0; index+1 < len(node.Content); index += 2 {
		pairs = append(pairs, yamlPair{key: node.Content[index], value: node.Content[index+1]})
	}
	return pairs
}

func staticString(node *yaml.Node) (string, bool) {
	if node == nil || node.Kind != yaml.ScalarNode || node.Tag != "!!str" || isNullNode(node) || nodeHasExpressionMarker(node) {
		return "", false
	}
	return node.Value, true
}

func staticBool(node *yaml.Node) (bool, bool) {
	if node == nil || node.Kind != yaml.ScalarNode || node.Tag != "!!bool" || nodeHasExpressionMarker(node) {
		return false, false
	}
	value, err := strconv.ParseBool(node.Value)
	return value, err == nil
}

func staticStringList(node *yaml.Node) ([]string, bool) {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil, false
	}
	values := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		value, ok := staticString(item)
		if !ok {
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}

func dependencyNames(node *yaml.Node) []string {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		if value, ok := staticString(node); ok && value != "" {
			return []string{value}
		}
	case yaml.SequenceNode:
		values, ok := staticStringList(node)
		if ok {
			return values
		}
	case yaml.MappingNode:
		values := make([]string, 0, len(node.Content)/2)
		for _, pair := range allMappingPairs(node) {
			if value, ok := staticString(pair.key); ok && value != "" {
				values = append(values, value)
			}
		}
		return values
	}
	return nil
}

func containsStaticPrefix(node *yaml.Node, prefix string) bool {
	if node == nil {
		return false
	}
	if value, ok := staticString(node); ok && strings.HasPrefix(strings.TrimSpace(value), prefix) {
		return true
	}
	for _, child := range node.Content {
		if containsStaticPrefix(child, prefix) {
			return true
		}
	}
	return false
}

func hasNonEmptyMappingScalarRecursive(node *yaml.Node, key string) bool {
	if node == nil {
		return false
	}
	if node.Kind == yaml.MappingNode {
		for _, pair := range allMappingPairs(node) {
			if pair.key.Value == key {
				if value, ok := staticString(pair.value); ok && strings.TrimSpace(value) != "" {
					return true
				}
			}
		}
	}
	for _, child := range node.Content {
		if hasNonEmptyMappingScalarRecursive(child, key) {
			return true
		}
	}
	return false
}

func embeddedAlias(reference string) string {
	if !strings.HasPrefix(reference, "embed:") {
		return ""
	}
	rest := strings.TrimPrefix(reference, "embed:")
	if at := strings.IndexByte(rest, '@'); at >= 0 {
		digest := rest[at+1:]
		if !validSHA256Digest(digest) {
			return ""
		}
		rest = rest[:at]
	}
	if rest == "" {
		return ""
	}
	for _, character := range rest {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '.' || character == '_' || character == '-' {
			continue
		}
		return ""
	}
	return rest
}

func validSHA256Digest(value string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+64 {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') && (character < 'A' || character > 'F') {
			return false
		}
	}
	return true
}

func upstreamComment(nodes ...*yaml.Node) string {
	for _, node := range nodes {
		if node == nil {
			continue
		}
		for _, comment := range []string{node.HeadComment, node.LineComment} {
			for _, line := range strings.Split(comment, "\n") {
				line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
				if !strings.HasPrefix(line, "upstream:") {
					continue
				}
				if reference := strings.TrimSpace(strings.TrimPrefix(line, "upstream:")); reference != "" {
					return reference
				}
			}
		}
	}
	return ""
}

func appendReason(existing string, reason string) string {
	if existing == "" {
		return reason
	}
	return existing + "; " + reason
}

func sortedUnique(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	copyValues := append([]string(nil), values...)
	sort.Strings(copyValues)
	result := copyValues[:0]
	for _, value := range copyValues {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func cloneSummary(source Summary) Summary {
	result := source
	result.Package.UnsupportedPlatforms = append([]string(nil), source.Package.UnsupportedPlatforms...)
	result.Package.LocaleCodes = append([]string(nil), source.Package.LocaleCodes...)
	result.Application.Permissions = append([]string(nil), source.Application.Permissions...)
	result.Services = append([]ServiceSummary(nil), source.Services...)
	for index := range result.Services {
		result.Services[index].DependsOn = append([]string(nil), source.Services[index].DependsOn...)
	}
	result.Images = append([]ImageSummary(nil), source.Images...)
	result.Template.ActionKinds = append([]string(nil), source.Template.ActionKinds...)
	result.Diagnostics = append([]Diagnostic(nil), source.Diagnostics...)
	return result
}
