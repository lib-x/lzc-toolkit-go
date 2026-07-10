package remote

import (
	"context"
	"io"

	"github.com/lib-x/lzc-toolkit-go/oci"
)

type BuildPackRequest struct {
	Tag           string
	Context       io.Reader
	ContextDigest oci.Digest
}

type BuildPackResult struct {
	Tag            string       `json:"tag"`
	ArchiveKey     string       `json:"archiveKey"`
	ImageID        oci.Digest   `json:"imageID"`
	DiffIDs        []oci.Digest `json:"diffIDs"`
	BaseRepoDigest string       `json:"baseRepoDigest,omitempty"`
	BaseDiffIDs    []oci.Digest `json:"baseDiffIDs,omitempty"`
}

type PackImageSpec struct {
	Ref             string       `json:"ref"`
	Alias           string       `json:"alias"`
	ImageID         oci.Digest   `json:"imageID"`
	Upstream        string       `json:"upstream"`
	EmbeddedDiffIDs []oci.Digest `json:"embeddedDiffIDs"`
	ArchiveKey      string       `json:"archiveKey,omitempty"`
}

type PackBlob struct {
	Digest oci.Digest `json:"digest"`
	Size   int64      `json:"size,omitempty"`
}

type PackManifest struct {
	Blobs              []PackBlob               `json:"blobs"`
	Index              oci.Index                `json:"index"`
	LockImages         map[string]oci.LockImage `json:"lockImages"`
	EmbeddedLayerBytes int64                    `json:"embeddedLayerBytes"`
	EmbeddedLayerCount int                      `json:"embeddedLayerCount"`
}

type ImageBackend interface {
	Info(context.Context) (BackendInfo, error)
	BuildPack(context.Context, BuildPackRequest) (BuildPackResult, error)
	PackImagesManifest(context.Context, []PackImageSpec) (PackManifest, error)
	BlobCheck(context.Context, []oci.Digest) ([]oci.Digest, error)
	BlobGet(context.Context, oci.Digest, io.Writer) error
}
