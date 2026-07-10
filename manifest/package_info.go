package manifest

import (
	"fmt"
	"reflect"
	"strings"

	"go.yaml.in/yaml/v3"
)

var staticPackageFields = []string{
	"package",
	"version",
	"name",
	"description",
	"author",
	"license",
	"homepage",
	"min_os_version",
	"unsupported_platforms",
	"locales",
}

// StaticPackageFields returns the top-level fields stored in package.yml for
// LPK v2. The returned slice may be modified by the caller.
func StaticPackageFields() []string {
	return append([]string(nil), staticPackageFields...)
}

// Effective combines typed package metadata with the typed manifest while
// retaining an independent raw source document for preservation and linting.
type Effective struct {
	Manifest       Manifest
	Source         *Document
	PackageInfo    *PackageInfo
	HasPackageFile bool
}

// SplitEffective removes static package fields from a cloned manifest and
// returns them in a separate package document. Caller-owned inputs are never
// mutated.
func SplitEffective(source *Document, packageInfo *PackageInfo, removedFields []string) (*Document, *Document, error) {
	manifestDocument := source.Clone()
	manifestMapping, err := topLevelMapping(manifestDocument, "manifest.split_effective")
	if err != nil {
		return nil, nil, err
	}
	packageDocument := emptyMappingDocument()
	packageMapping, _ := topLevelMapping(packageDocument, "manifest.split_effective")

	removed := make(map[string]struct{}, len(removedFields))
	for _, field := range removedFields {
		removed[field] = struct{}{}
	}
	for _, field := range staticPackageFields {
		key, value, found := mappingPair(manifestMapping, field)
		if found {
			deleteMappingKey(manifestMapping, field)
		}
		if _, omit := removed[field]; omit {
			continue
		}
		if packageInfo != nil {
			state := presenceState(packageInfo.Presence, field)
			typedValue := packageFieldValue(packageInfo, field)
			if state == Absent && !isZeroPackageValue(typedValue) {
				state = Value
			}
			switch state {
			case Value, Null:
				if err := appendTypedPackageField(packageMapping, key, field, typedValue, state); err != nil {
					return nil, nil, err
				}
				continue
			case Absent:
			default:
				return nil, nil, manifestError("manifest.split_effective", fmt.Errorf("invalid presence state for %s", field))
			}
		}
		if !found {
			continue
		}
		packageMapping.Content = append(packageMapping.Content, key, value)
	}

	return manifestDocument, packageDocument, nil
}

func appendTypedPackageField(mapping *yaml.Node, sourceKey *yaml.Node, field string, value any, state FieldState) error {
	key := sourceKey
	if key == nil {
		key = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: field}
	}

	var encoded yaml.Node
	if state == Null {
		if err := encoded.Encode(nil); err != nil {
			return manifestError("manifest.split_effective", err)
		}
	} else {
		if isNilPackageValue(value) {
			return manifestError("manifest.split_effective", fmt.Errorf("presence state Value for %s requires a non-nil value", field))
		}
		if err := encoded.Encode(value); err != nil {
			return manifestError("manifest.split_effective", err)
		}
	}
	mapping.Content = append(mapping.Content, key, &encoded)
	return nil
}

func isNilPackageValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func packageFieldValue(info *PackageInfo, field string) any {
	switch field {
	case "package":
		return info.Package
	case "version":
		return info.Version
	case "name":
		return info.Name
	case "description":
		return info.Description
	case "author":
		return info.Author
	case "license":
		return info.License
	case "homepage":
		return info.Homepage
	case "min_os_version":
		return info.MinOSVersion
	case "unsupported_platforms":
		return info.UnsupportedPlatforms
	case "locales":
		return info.Locales
	default:
		return nil
	}
}

func isZeroPackageValue(value any) bool {
	if value == nil {
		return true
	}
	return reflect.ValueOf(value).IsZero()
}

func presenceState(presence PackagePresence, field string) FieldState {
	switch field {
	case "package":
		return presence.Package
	case "version":
		return presence.Version
	case "name":
		return presence.Name
	case "description":
		return presence.Description
	case "author":
		return presence.Author
	case "license":
		return presence.License
	case "homepage":
		return presence.Homepage
	case "min_os_version":
		return presence.MinOSVersion
	case "unsupported_platforms":
		return presence.UnsupportedPlatforms
	case "locales":
		return presence.Locales
	default:
		return Absent
	}
}

// LoadEffective loads a legacy manifest or combines manifest.yml with an LPK
// v2 package.yml document.
func LoadEffective(manifestDocument *Document, packageDocument *Document, strictStaticFields bool) (Effective, error) {
	if manifestDocument == nil {
		return Effective{}, manifestError("manifest.load_effective", fmt.Errorf("nil manifest document"))
	}

	source := manifestDocument.Clone()
	if packageDocument == nil {
		var typed Manifest
		if err := source.Decode(&typed); err != nil {
			return Effective{}, err
		}
		presence, err := packagePresence(source)
		if err != nil {
			return Effective{}, err
		}
		typed.Presence = presence
		return Effective{Manifest: typed, Source: source}, nil
	}

	packageSource := packageDocument.Clone()
	if strictStaticFields {
		fields, err := presentStaticPackageFields(source)
		if err != nil {
			return Effective{}, err
		}
		if len(fields) != 0 {
			return Effective{}, manifestError("manifest.load_effective", fmt.Errorf("static package fields must be moved to package.yml: %s", strings.Join(fields, ", ")))
		}
	}
	var packageInfo PackageInfo
	if err := packageSource.Decode(&packageInfo); err != nil {
		return Effective{}, err
	}
	presence, err := packagePresence(packageSource)
	if err != nil {
		return Effective{}, err
	}
	packageInfo.Presence = presence

	merged := source.Clone()
	if err := replaceStaticPackageFields(merged, packageSource); err != nil {
		return Effective{}, err
	}
	var typed Manifest
	if err := merged.Decode(&typed); err != nil {
		return Effective{}, err
	}
	typed.Presence = presence

	return Effective{
		Manifest:       typed,
		Source:         source,
		PackageInfo:    &packageInfo,
		HasPackageFile: true,
	}, nil
}

func presentStaticPackageFields(document *Document) ([]string, error) {
	mapping, err := topLevelMapping(document, "manifest.load_effective")
	if err != nil {
		return nil, err
	}
	fields := make([]string, 0, len(staticPackageFields))
	for _, field := range staticPackageFields {
		if _, found := mappingValue(mapping, field); found {
			fields = append(fields, field)
		}
	}
	return fields, nil
}

func replaceStaticPackageFields(target *Document, packageSource *Document) error {
	targetMapping, err := topLevelMapping(target, "manifest.load_effective")
	if err != nil {
		return err
	}
	packageMapping, err := topLevelMapping(packageSource, "manifest.load_effective")
	if err != nil {
		return err
	}

	for _, field := range staticPackageFields {
		deleteMappingKey(targetMapping, field)
	}
	clones := make(map[*yaml.Node]*yaml.Node)
	for _, field := range staticPackageFields {
		key, value, found := mappingPair(packageMapping, field)
		if !found {
			continue
		}
		targetMapping.Content = append(targetMapping.Content, cloneNode(key, clones), cloneNode(value, clones))
	}
	return nil
}

func emptyMappingDocument() *Document {
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	return &Document{root: &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{mapping}}}
}

func packagePresence(document *Document) (PackagePresence, error) {
	mapping, err := topLevelMapping(document, "manifest.package_presence")
	if err != nil {
		return PackagePresence{}, err
	}

	var presence PackagePresence
	for _, field := range staticPackageFields {
		node, found := mappingValue(mapping, field)
		state := Absent
		if found {
			state = Value
			if isNullNode(node) {
				state = Null
			}
		}
		setPresenceState(&presence, field, state)
	}
	return presence, nil
}

func topLevelMapping(document *Document, op string) (*yaml.Node, error) {
	if document == nil || document.root == nil {
		return nil, manifestError(op, fmt.Errorf("nil document"))
	}
	mapping := documentContent(document.root)
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil, manifestError(op, fmt.Errorf("top-level YAML value is not a mapping"))
	}
	return mapping, nil
}

func mappingValue(mapping *yaml.Node, key string) (*yaml.Node, bool) {
	_, value, found := mappingPair(mapping, key)
	return value, found
}

func mappingPair(mapping *yaml.Node, key string) (*yaml.Node, *yaml.Node, bool) {
	for index := 0; index < len(mapping.Content); index += 2 {
		keyNode := mapping.Content[index]
		if keyNode.Kind == yaml.ScalarNode && keyNode.Value == key {
			return keyNode, mapping.Content[index+1], true
		}
	}
	return nil, nil, false
}

func deleteMappingKey(mapping *yaml.Node, key string) bool {
	for index := 0; index < len(mapping.Content); index += 2 {
		keyNode := mapping.Content[index]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Value != key {
			continue
		}
		mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
		return true
	}
	return false
}

func setPresenceState(presence *PackagePresence, field string, state FieldState) {
	switch field {
	case "package":
		presence.Package = state
	case "version":
		presence.Version = state
	case "name":
		presence.Name = state
	case "description":
		presence.Description = state
	case "author":
		presence.Author = state
	case "license":
		presence.License = state
	case "homepage":
		presence.Homepage = state
	case "min_os_version":
		presence.MinOSVersion = state
	case "unsupported_platforms":
		presence.UnsupportedPlatforms = state
	case "locales":
		presence.Locales = state
	}
}
