// Package lint reports non-fatal compatibility issues in LPK inputs.
package lint

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
	manifestpkg "github.com/lib-x/lpk-go/manifest"
)

type schemaKind uint8

const (
	schemaAny schemaKind = iota
	schemaObject
	schemaArray
	schemaMap
)

type schemaField struct {
	name string
	node *schemaNode
}

type schemaNode struct {
	kind    schemaKind
	fields  []schemaField
	element *schemaNode
}

var anyNode = &schemaNode{kind: schemaAny}

func object(fields ...schemaField) *schemaNode {
	return &schemaNode{kind: schemaObject, fields: fields}
}

func field(name string, node *schemaNode) schemaField {
	return schemaField{name: name, node: node}
}

func arrayOf(node *schemaNode) *schemaNode {
	return &schemaNode{kind: schemaArray, element: node}
}

func mapOf(node *schemaNode) *schemaNode {
	return &schemaNode{kind: schemaMap, element: node}
}

var appHealthCheckSchema = object(
	field("test_url", anyNode),
	field("disable", anyNode),
	field("start_period", anyNode),
	field("timeout", anyNode),
)

var serviceHealthCheckSchema = object(
	field("test", anyNode),
	field("timeout", anyNode),
	field("interval", anyNode),
	field("retries", anyNode),
	field("start_period", anyNode),
	field("start_interval", anyNode),
	field("disable", anyNode),
	field("test_url", anyNode),
)

var manifestSchema = object(
	field("package", anyNode),
	field("version", anyNode),
	field("name", anyNode),
	field("description", anyNode),
	field("usage", anyNode),
	field("license", anyNode),
	field("homepage", anyNode),
	field("author", anyNode),
	field("min_os_version", anyNode),
	field("unsupported_platforms", anyNode),
	field("locales", anyNode),
	field("ext_config", object(
		field("permissions", anyNode),
		field("enable_document_access", anyNode),
		field("enable_media_access", anyNode),
		field("enable_clientfs_access", anyNode),
		field("disable_grpc_web_on_root", anyNode),
		field("default_prefix_domain", anyNode),
		field("enable_bind_mime_globs", anyNode),
		field("disable_url_raw_path", anyNode),
		field("remove_this_request_headers", anyNode),
		field("fix_websocket_header", anyNode),
	)),
	field("application", object(
		field("image", anyNode),
		field("background_task", anyNode),
		field("subdomain", anyNode),
		field("secondary_domains", anyNode),
		field("multi_instance", anyNode),
		field("usb_accel", anyNode),
		field("gpu_accel", anyNode),
		field("kvm_accel", anyNode),
		field("file_handler", object(
			field("mime", anyNode),
			field("actions", anyNode),
		)),
		field("entries", arrayOf(object(
			field("id", anyNode),
			field("title", anyNode),
			field("path", anyNode),
			field("prefix_domain", anyNode),
		))),
		field("routes", anyNode),
		field("upstreams", arrayOf(object(
			field("location", anyNode),
			field("disable_trim_location", anyNode),
			field("domain_prefix", anyNode),
			field("backend", anyNode),
			field("use_backend_host", anyNode),
			field("backend_launch_command", anyNode),
			field("trim_url_suffix", anyNode),
			field("disable_backend_ssl_verify", anyNode),
			field("disable_auto_health_checking", anyNode),
			field("disable_url_raw_path", anyNode),
			field("remove_this_request_headers", anyNode),
			field("fix_websocket_header", anyNode),
			field("dump_http_headers_when_5xx", anyNode),
			field("dump_http_headers_when_paths", anyNode),
		))),
		field("injects", arrayOf(object(
			field("id", anyNode),
			field("on", anyNode),
			field("auth_required", anyNode),
			field("prefix_domain", anyNode),
			field("when", anyNode),
			field("unless", anyNode),
			field("do", arrayOf(object(
				field("src", anyNode),
				field("params", anyNode),
			))),
		))),
		field("public_path", anyNode),
		field("workdir", anyNode),
		field("ingress", arrayOf(object(
			field("protocol", anyNode),
			field("port", anyNode),
			field("service", anyNode),
			field("description", anyNode),
			field("publish_port", anyNode),
			field("send_port_info", anyNode),
			field("yes_i_want_80_443", anyNode),
		))),
		field("environment", anyNode),
		field("health_check", appHealthCheckSchema),
		field("oidc_redirect_path", anyNode),
		field("handlers", object(
			field("acl_handler", anyNode),
			field("error_page_templates", anyNode),
		)),
		field("user_app", anyNode),
		field("depends_on", anyNode),
	)),
	field("services", mapOf(object(
		field("init", anyNode),
		field("image", anyNode),
		field("environment", anyNode),
		field("entrypoint", anyNode),
		field("command", anyNode),
		field("tmpfs", anyNode),
		field("depends_on", anyNode),
		field("healthcheck", serviceHealthCheckSchema),
		field("health_check", serviceHealthCheckSchema),
		field("user", anyNode),
		field("cpu_shares", anyNode),
		field("cpus", anyNode),
		field("mem_limit", anyNode),
		field("shm_size", anyNode),
		field("network_mode", anyNode),
		field("netadmin", anyNode),
		field("setup_script", anyNode),
		field("binds", anyNode),
		field("runtime", anyNode),
	))),
)

// Manifest reports compatibility warnings for source. The raw document is
// authoritative for field presence; typed is accepted alongside it so callers
// can use the same decoded manifest passed to later lifecycle stages.
//
// Warning.Path values from this function are opaque presentation metadata.
// A warning that aggregates multiple source locations uses a comma-delimited
// path string for readability; callers must not split or otherwise parse it.
// Warning.Code is the compatibility identifier callers should consume.
func Manifest(source *manifestpkg.Document, typed manifestpkg.Manifest, optionValues ...Option) ([]lpkgo.Warning, error) {
	if source == nil {
		return nil, invalidManifestLintError()
	}
	options := applyOptions(optionValues)
	var raw any
	if err := source.Decode(&raw); err != nil {
		return nil, err
	}
	root, ok := stringMap(raw)
	if !ok {
		return nil, invalidManifestLintError()
	}

	warnings := make([]lpkgo.Warning, 0, 7)
	if paths := collectUnknownPaths(root, manifestSchema, ""); len(paths) != 0 {
		warnings = append(warnings, manifestWarning(
			"unknown-manifest-fields",
			strings.Join(paths, ","),
			fmt.Sprintf("Unknown manifest fields detected: %s.", quotePaths(paths)),
		))
	}

	staticPaths := presentPaths(root, manifestpkg.StaticPackageFields(), "")
	if len(staticPaths) != 0 {
		warnings = append(warnings, manifestWarning(
			"legacy-static-package-fields",
			strings.Join(staticPaths, ","),
			fmt.Sprintf("Top-level static package fields are deprecated in LPK v2: %s.", quotePaths(staticPaths)),
		))
	}

	application, _ := stringMap(root["application"])
	for _, deprecated := range []struct {
		field   string
		code    string
		message string
	}{
		{field: "handlers", code: "application-handlers-deprecated", message: "application.handlers is deprecated and kept for compatibility only."},
		{field: "user_app", code: "application-user-app-deprecated", message: "application.user_app is deprecated; use application.multi_instance."},
		{field: "depends_on", code: "application-depends-on-deprecated", message: "application.depends_on is deprecated; use service dependencies or routing health checks."},
	} {
		if _, found := application[deprecated.field]; found {
			path := "application." + deprecated.field
			warnings = append(warnings, manifestWarning(deprecated.code, path, deprecated.message))
		}
	}

	services, _ := stringMap(root["services"])
	serviceNames := sortedKeys(services)
	legacyServicePaths := make([]string, 0)
	for _, serviceName := range serviceNames {
		service, _ := stringMap(services[serviceName])
		if _, found := service["health_check"]; found {
			legacyServicePaths = append(legacyServicePaths, "services."+serviceName+".health_check")
		}
	}
	if len(legacyServicePaths) != 0 {
		warnings = append(warnings, manifestWarning(
			"service-health-check-deprecated",
			strings.Join(legacyServicePaths, ","),
			fmt.Sprintf("Legacy service health checks detected: %s; rename them to healthcheck.", quotePaths(legacyServicePaths)),
		))
	}

	extConfig, _ := stringMap(root["ext_config"])
	legacyExtPaths := presentPaths(extConfig, []string{
		"disable_url_raw_path",
		"remove_this_request_headers",
		"fix_websocket_header",
	}, "ext_config")
	if len(legacyExtPaths) != 0 {
		warnings = append(warnings, manifestWarning(
			"ext-config-http-routing-deprecated",
			strings.Join(legacyExtPaths, ","),
			fmt.Sprintf("Legacy ext_config HTTP routing fields are deprecated: %s.", quotePaths(legacyExtPaths)),
		))
	}
	if options.official {
		warnings = append(warnings, collectOfficialWarnings(root, typed, options)...)
	}
	return warnings, nil
}

func collectUnknownPaths(value any, schema *schemaNode, currentPath string) []string {
	if schema == nil || schema.kind == schemaAny || value == nil {
		return nil
	}
	switch schema.kind {
	case schemaArray:
		values, ok := value.([]any)
		if !ok {
			return nil
		}
		var paths []string
		for index, item := range values {
			paths = append(paths, collectUnknownPaths(item, schema.element, fmt.Sprintf("%s[%d]", currentPath, index))...)
		}
		return paths
	case schemaMap:
		mapping, ok := stringMap(value)
		if !ok {
			return nil
		}
		var paths []string
		for _, key := range sortedKeys(mapping) {
			paths = append(paths, collectUnknownPaths(mapping[key], schema.element, joinPath(currentPath, key))...)
		}
		return paths
	case schemaObject:
		mapping, ok := stringMap(value)
		if !ok {
			return nil
		}
		known := make(map[string]struct{}, len(schema.fields))
		var paths []string
		for _, schemaField := range schema.fields {
			known[schemaField.name] = struct{}{}
			if item, found := mapping[schemaField.name]; found {
				paths = append(paths, collectUnknownPaths(item, schemaField.node, joinPath(currentPath, schemaField.name))...)
			}
		}
		unknown := make([]string, 0)
		for key := range mapping {
			if _, found := known[key]; !found {
				unknown = append(unknown, key)
			}
		}
		sort.Strings(unknown)
		for _, key := range unknown {
			paths = append(paths, joinPath(currentPath, key))
		}
		return paths
	default:
		return nil
	}
}

func stringMap(value any) (map[string]any, bool) {
	mapping, ok := value.(map[string]any)
	return mapping, ok
}

func sortedKeys(mapping map[string]any) []string {
	keys := make([]string, 0, len(mapping))
	for key := range mapping {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func presentPaths(mapping map[string]any, fields []string, prefix string) []string {
	paths := make([]string, 0, len(fields))
	for _, name := range fields {
		if _, found := mapping[name]; found {
			paths = append(paths, joinPath(prefix, name))
		}
	}
	return paths
}

func joinPath(prefix string, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func quotePaths(paths []string) string {
	quoted := make([]string, len(paths))
	for index, path := range paths {
		quoted[index] = "`" + path + "`"
	}
	return strings.Join(quoted, ", ")
}

func manifestWarning(code string, path string, message string) lpkgo.Warning {
	return lpkgo.Warning{Code: code, Severity: lpkgo.SeverityWarning, Path: path, Message: message}
}

func invalidManifestLintError() error {
	return &lpkgo.Error{Code: lpkgo.CodeInvalidManifest, Op: "lint.manifest", Cause: errors.New("manifest lint input is invalid")}
}
