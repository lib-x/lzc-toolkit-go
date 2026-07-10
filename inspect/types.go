package inspect

import (
	"github.com/lib-x/lzc-toolkit-go/archive"
	"github.com/lib-x/lzc-toolkit-go/lpk"
)

// Info is the materialized inspection result for one LPK.
type Info struct {
	Size           int64
	Format         archive.Format
	Layout         lpk.Layout
	Signed         bool
	ResourceOnly   bool
	PackageID      string
	AppVersion     string
	HasManifest    bool
	HasPackageInfo bool
	HasImagesDir   bool
	HasImagesLock  bool
	Images         ImageInfo
}

// ImageDetail summarizes one image alias from images.lock.
type ImageDetail struct {
	Alias                     string
	ImageID                   string
	Upstream                  string
	EmbeddedLayerCount        int
	UpstreamLayerCount        int
	UniqueEmbeddedLayerCount  int
	EmbeddedBytes             int64
	MissingEmbeddedLayerCount int
}

// ImageInfo summarizes all image aliases and embedded layer blobs.
type ImageInfo struct {
	Aliases                        []string
	Details                        []ImageDetail
	TotalEmbeddedLayerCount        int
	TotalEmbeddedBytes             int64
	TotalMissingEmbeddedLayerCount int
}
