// Package dockerarchive converts Docker image-save TAR streams into the OCI
// image layout used by lzc-cli LPK v2 packages.
package dockerarchive

import "github.com/lib-x/lzc-toolkit-go/oci"

type Spec struct {
	Ref             string
	Alias           string
	ImageID         oci.Digest
	Upstream        string
	EmbeddedDiffIDs []oci.Digest
}

type Limits struct {
	MaxEntries      int
	MaxFileBytes    int64
	MaxArchiveBytes int64
}

type Request struct {
	Specs  []Spec
	Limits Limits
}

type Result struct {
	ImageCount         int
	EmbeddedLayerCount int
	UpstreamLayerCount int
	EmbeddedBytes      int64
}
