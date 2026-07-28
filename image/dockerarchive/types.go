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
	MaxEntries int
	// MaxFileBytes limits both stored archive entries and the decompressed
	// content used to verify an already-gzipped Docker layer.
	MaxFileBytes int64
	// MaxExpandedBytes limits the cumulative decompressed size of distinct
	// Docker layer paths inspected during conversion.
	MaxExpandedBytes int64
	MaxArchiveBytes  int64
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
