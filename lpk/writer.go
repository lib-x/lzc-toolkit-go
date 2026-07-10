package lpk

import (
	"context"
	"io"
	"io/fs"

	"github.com/lib-x/lpk-go/archive"
)

// WriteRequest describes a completed package root to encode as an LPK.
type WriteRequest struct {
	Layout Layout
	Files  fs.FS
	Strict bool
	// AllowManifestTemplate permits the unrendered Go-template manifests
	// accepted and preserved by lzc-cli. It does not render the template.
	AllowManifestTemplate bool
}

// WriteResult reports the encoded LPK container metadata.
type WriteResult struct {
	Layout Layout
	Format archive.Format
	Size   int64
	SHA256 [32]byte
}

// Write validates and encodes an LPK to dst. It does not close dst.
func Write(ctx context.Context, dst io.Writer, request WriteRequest) (WriteResult, error) {
	result := WriteResult{Layout: request.Layout, Format: formatForLayout(request.Layout)}
	if err := contextError(ctx, "lpk.write"); err != nil {
		return result, err
	}
	if dst == nil {
		return result, nilWriterError("lpk.write")
	}
	if err := validateWriteRequest(ctx, request); err != nil {
		return result, err
	}
	archiveResult, err := archive.Write(ctx, dst, request.Files, archive.WriteOptions{
		Format:       result.Format,
		Reproducible: true,
	})
	result.Size = archiveResult.Size
	result.SHA256 = archiveResult.SHA256
	return result, err
}

// WriteFile writes an LPK to filename using atomic replacement.
func WriteFile(ctx context.Context, filename string, request WriteRequest) (WriteResult, error) {
	result := WriteResult{Layout: request.Layout, Format: formatForLayout(request.Layout)}
	if err := contextError(ctx, "lpk.write_file"); err != nil {
		return result, err
	}
	if err := validateWriteRequest(ctx, request); err != nil {
		return result, err
	}
	archiveResult, err := archive.WriteFileAtomic(ctx, filename, request.Files, archive.WriteOptions{
		Format:       result.Format,
		Reproducible: true,
	})
	result.Format = archiveResult.Format
	result.Size = archiveResult.Size
	result.SHA256 = archiveResult.SHA256
	return result, err
}

func formatForLayout(layout Layout) archive.Format {
	switch layout {
	case LayoutV1:
		return archive.FormatZIP
	case LayoutV2:
		return archive.FormatTAR
	default:
		return ""
	}
}
