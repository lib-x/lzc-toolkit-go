package oci

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

var sha256DigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Digest string

func ParseDigest(value string) (Digest, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if !strings.HasPrefix(normalized, "sha256:") || !sha256DigestPattern.MatchString(strings.TrimPrefix(normalized, "sha256:")) {
		return "", errors.New("invalid sha256 digest")
	}
	return Digest(normalized), nil
}

func (d Digest) String() string { return string(d) }

func (d Digest) Hex() string { return strings.TrimPrefix(string(d), "sha256:") }

func (d Digest) Valid() bool {
	_, err := ParseDigest(string(d))
	return err == nil
}

func (d Digest) MarshalJSON() ([]byte, error) { return json.Marshal(string(d)) }

func (d *Digest) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := ParseDigest(value)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

func (d Digest) MarshalYAML() (any, error) { return string(d), nil }

func (d *Digest) UnmarshalYAML(node *yaml.Node) error {
	var value string
	if err := node.Decode(&value); err != nil {
		return err
	}
	parsed, err := ParseDigest(value)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}
