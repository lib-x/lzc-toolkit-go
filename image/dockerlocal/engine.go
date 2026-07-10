package dockerlocal

import (
	"context"
	"io"

	imagebuild "github.com/lib-x/lzc-toolkit-go/image"
	"github.com/lib-x/lzc-toolkit-go/oci"
)

const DefaultPlatform = "linux/amd64"

type BuildRequest struct {
	Entry    imagebuild.Entry
	Platform string
}

type BuildResult struct {
	Ref             string
	ImageID         oci.Digest
	DiffIDs         []oci.Digest
	Upstream        string
	UpstreamDiffIDs []oci.Digest
}

type Engine interface {
	Build(context.Context, BuildRequest) (BuildResult, error)
	Save(context.Context, []string, io.Writer) error
	Remove(context.Context, string) error
}

type Option func(*Builder)

func WithPlatform(platform string) Option {
	return func(builder *Builder) { builder.platform = platform }
}
