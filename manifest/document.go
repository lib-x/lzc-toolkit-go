package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	lpkgo "github.com/lib-x/lpk-go"
	"go.yaml.in/yaml/v3"
)

// Document is a mutable YAML document that preserves syntax details such as
// comments and fields unknown to the typed manifest model.
type Document struct {
	root *yaml.Node
}

// Parse parses exactly one non-empty YAML document. Empty trailing documents
// are ignored, while trailing documents containing values are rejected.
func Parse(data []byte) (*Document, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))

	var root yaml.Node
	if err := decoder.Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			err = fmt.Errorf("empty YAML document")
		}
		return nil, manifestError("manifest.parse", err)
	}
	if documentContent(&root) == nil || isNullNode(documentContent(&root)) {
		return nil, manifestError("manifest.parse", fmt.Errorf("empty YAML document"))
	}

	for {
		var trailing yaml.Node
		err := decoder.Decode(&trailing)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, manifestError("manifest.parse", err)
		}
		content := documentContent(&trailing)
		if content != nil && !isNullNode(content) {
			return nil, manifestError("manifest.parse", fmt.Errorf("multiple YAML documents"))
		}
	}

	return &Document{root: &root}, nil
}

// Decode decodes the complete document into target.
func (d *Document) Decode(target any) error {
	if d == nil || d.root == nil {
		return manifestError("manifest.decode", fmt.Errorf("nil document"))
	}
	if err := d.root.Decode(target); err != nil {
		return manifestError("manifest.decode", err)
	}
	return nil
}

// Bytes encodes the document as YAML using two-space indentation.
func (d *Document) Bytes() ([]byte, error) {
	if d == nil || d.root == nil {
		return nil, manifestError("manifest.bytes", fmt.Errorf("nil document"))
	}

	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(d.root); err != nil {
		return nil, manifestError("manifest.bytes", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, manifestError("manifest.bytes", err)
	}
	return output.Bytes(), nil
}

// Lookup returns the decoded value at path. A missing key returns found=false.
func (d *Document) Lookup(path ...string) (value any, found bool, err error) {
	node, found, err := d.lookupNode(path)
	if err != nil || !found {
		return nil, found, err
	}
	if err := node.Decode(&value); err != nil {
		return nil, false, manifestError("manifest.lookup", err)
	}
	return value, true, nil
}

// Set replaces or appends the value at path. All parent mappings must exist.
func (d *Document) Set(value any, path ...string) error {
	if len(path) == 0 {
		return manifestError("manifest.set", fmt.Errorf("empty path"))
	}
	if path[len(path)-1] == "" {
		return manifestError("manifest.set", fmt.Errorf("empty path component"))
	}

	parent, found, err := d.lookupNode(path[:len(path)-1])
	if err != nil {
		return err
	}
	if !found {
		return manifestError("manifest.set", fmt.Errorf("parent path %q does not exist", path[:len(path)-1]))
	}
	if parent.Kind != yaml.MappingNode {
		return manifestError("manifest.set", fmt.Errorf("parent path %q is not a mapping", path[:len(path)-1]))
	}

	var encoded yaml.Node
	if err := encoded.Encode(value); err != nil {
		return manifestError("manifest.set", err)
	}
	key := path[len(path)-1]
	for index := 0; index < len(parent.Content); index += 2 {
		keyNode := parent.Content[index]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Value != key {
			continue
		}
		old := parent.Content[index+1]
		encoded.HeadComment = old.HeadComment
		encoded.LineComment = old.LineComment
		encoded.FootComment = old.FootComment
		parent.Content[index+1] = &encoded
		return nil
	}

	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&encoded,
	)
	return nil
}

// Delete removes the key/value pair at path.
func (d *Document) Delete(path ...string) bool {
	if len(path) == 0 {
		return false
	}
	parent, found, err := d.lookupNode(path[:len(path)-1])
	if err != nil || !found || parent.Kind != yaml.MappingNode {
		return false
	}

	key := path[len(path)-1]
	for index := 0; index < len(parent.Content); index += 2 {
		keyNode := parent.Content[index]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Value != key {
			continue
		}
		parent.Content = append(parent.Content[:index], parent.Content[index+2:]...)
		return true
	}
	return false
}

// Clone returns an independent deep copy of the document.
func (d *Document) Clone() *Document {
	if d == nil {
		return nil
	}
	return &Document{root: cloneNode(d.root, make(map[*yaml.Node]*yaml.Node))}
}

func (d *Document) lookupNode(path []string) (*yaml.Node, bool, error) {
	if d == nil || d.root == nil {
		return nil, false, manifestError("manifest.lookup", fmt.Errorf("nil document"))
	}

	current := documentContent(d.root)
	if current == nil {
		return nil, false, manifestError("manifest.lookup", fmt.Errorf("empty document"))
	}
	for _, component := range path {
		if component == "" {
			return nil, false, manifestError("manifest.lookup", fmt.Errorf("empty path component"))
		}
		if current.Kind != yaml.MappingNode {
			return nil, false, manifestError("manifest.lookup", fmt.Errorf("path component %q traverses a non-mapping value", component))
		}
		var next *yaml.Node
		for index := 0; index < len(current.Content); index += 2 {
			key := current.Content[index]
			if key.Kind == yaml.ScalarNode && key.Value == component {
				next = current.Content[index+1]
				break
			}
		}
		if next == nil {
			return nil, false, nil
		}
		current = next
	}
	return current, true, nil
}

func documentContent(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		return node.Content[0]
	}
	return node
}

func isNullNode(node *yaml.Node) bool {
	return node.Kind == yaml.ScalarNode && node.Tag == "!!null"
}

func cloneNode(node *yaml.Node, clones map[*yaml.Node]*yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if clone, ok := clones[node]; ok {
		return clone
	}

	clone := *node
	clones[node] = &clone
	clone.Alias = cloneNode(node.Alias, clones)
	clone.Content = make([]*yaml.Node, len(node.Content))
	for index, child := range node.Content {
		clone.Content[index] = cloneNode(child, clones)
	}
	return &clone
}

func manifestError(op string, cause error) error {
	if cause == nil {
		return nil
	}
	return &lpkgo.Error{Code: lpkgo.CodeInvalidManifest, Op: op, Cause: cause}
}
