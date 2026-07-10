// Package buildpack implements remote image builds through LazyCat Developer
// Tools without adding SSH or gRPC dependencies to package build.
package buildpack

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/build"
	imagebuild "github.com/lib-x/lpk-go/image"
	"github.com/lib-x/lpk-go/oci"
	"github.com/lib-x/lpk-go/remote"
	"github.com/lib-x/lpk-go/remote/blobcache"
)

const defaultMaxContextBytes = int64(32 << 30)

type Option func(*Builder)

func WithMaxContextBytes(value int64) Option {
	return func(builder *Builder) {
		if value > 0 {
			builder.maxContextBytes = value
		}
	}
}

type Builder struct {
	backend         remote.ImageBackend
	cache           blobcache.Cache
	maxContextBytes int64
}

func New(backend remote.ImageBackend, cache blobcache.Cache, options ...Option) *Builder {
	builder := &Builder{backend: backend, cache: cache, maxContextBytes: defaultMaxContextBytes}
	for _, option := range options {
		if option != nil {
			option(builder)
		}
	}
	return builder
}

func (builder *Builder) Build(ctx context.Context, request build.ImageBuildRequest) (build.ImageArtifact, error) {
	if ctx == nil || builder == nil || builder.backend == nil {
		return nil, packError(lpkgo.CodeInvalidArgument, "buildpack.build", errors.New("nil context, builder, or backend"))
	}
	entries, err := imagebuild.Normalize(ctx, request.Root, request.Manifest, request.Config)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, packError(lpkgo.CodeInvalidConfig, "buildpack.build", errors.New("images config is empty"))
	}
	for _, entry := range entries {
		if entry.Builder != imagebuild.BuilderRemote {
			return nil, packError(lpkgo.CodeIncompatibleBackend, "buildpack.build", errors.New("local image entry requires the local builder"))
		}
	}
	info, err := builder.backend.Info(ctx)
	if err != nil {
		return nil, err
	}
	for _, capability := range []remote.Capability{
		remote.CapabilityLPKV2,
		remote.CapabilityBuildPackContextCache,
		remote.CapabilityBlobManifestTransport,
	} {
		if err := remote.Require(capability, info.Version); err != nil {
			return nil, err
		}
	}

	work, err := os.MkdirTemp("", "lpk-buildpack-*")
	if err != nil {
		return nil, packError(lpkgo.CodeCommandFailed, "buildpack.build", err)
	}
	keepWork := false
	defer func() {
		if !keepWork {
			_ = os.RemoveAll(work)
		}
	}()

	specs := make([]remote.PackImageSpec, 0, len(entries))
	for _, entry := range entries {
		contextPath := filepath.Join(work, entry.Alias+"-context.tar")
		contextDigest, err := writeContextTar(ctx, contextPath, entry, builder.maxContextBytes)
		if err != nil {
			return nil, err
		}
		contextFile, err := os.Open(contextPath)
		if err != nil {
			return nil, packError(lpkgo.CodeCommandFailed, "buildpack.context", err)
		}
		built, buildErr := builder.backend.BuildPack(ctx, remote.BuildPackRequest{
			Tag:           "debug.bridge/" + entry.ImageLabel,
			Context:       contextFile,
			ContextDigest: contextDigest,
		})
		closeErr := contextFile.Close()
		if buildErr != nil {
			return nil, buildErr
		}
		if closeErr != nil {
			return nil, packError(lpkgo.CodeCommandFailed, "buildpack.context", closeErr)
		}
		if err := validateBuildResult(built); err != nil {
			return nil, err
		}
		upstream, upstreamDiffIDs := selectUpstream(entry.UpstreamMatch, built)
		embedded := append([]oci.Digest(nil), built.DiffIDs[len(upstreamDiffIDs):]...)
		if len(embedded) == 0 && upstream == "" {
			return nil, packError(lpkgo.CodeInvalidConfig, "buildpack.build_result", errors.New("image has no embedded layers or upstream"))
		}
		specs = append(specs, remote.PackImageSpec{
			Ref:             "archive-key:" + built.ArchiveKey,
			Alias:           entry.Alias,
			ImageID:         built.ImageID,
			Upstream:        upstream,
			EmbeddedDiffIDs: embedded,
			ArchiveKey:      built.ArchiveKey,
		})
	}

	manifest, err := builder.backend.PackImagesManifest(ctx, specs)
	if err != nil {
		return nil, err
	}
	artifactPath := filepath.Join(work, "artifact")
	if err := builder.materialize(ctx, manifest, artifactPath); err != nil {
		return nil, err
	}
	keepWork = true
	return &directoryArtifact{root: work, artifact: artifactPath}, nil
}

func validateBuildResult(result remote.BuildPackResult) error {
	if strings.TrimSpace(result.Tag) == "" || strings.TrimSpace(result.ArchiveKey) == "" || !result.ImageID.Valid() || len(result.DiffIDs) == 0 {
		return packError(lpkgo.CodeRemoteUnavailable, "buildpack.build_result", errors.New("invalid build-pack result"))
	}
	for _, digest := range append(append([]oci.Digest(nil), result.DiffIDs...), result.BaseDiffIDs...) {
		if !digest.Valid() {
			return packError(lpkgo.CodeRemoteUnavailable, "buildpack.build_result", errors.New("invalid build-pack digest"))
		}
	}
	return nil
}

func selectUpstream(match string, result remote.BuildPackResult) (string, []oci.Digest) {
	upstream := strings.TrimSpace(result.BaseRepoDigest)
	separator := strings.LastIndex(upstream, "@sha256:")
	if separator <= 0 || (strings.TrimSpace(match) != "" && !strings.HasPrefix(upstream[:separator], strings.TrimSpace(match))) {
		return "", nil
	}
	if len(result.BaseDiffIDs) > len(result.DiffIDs) {
		return "", nil
	}
	for index, digest := range result.BaseDiffIDs {
		if digest != result.DiffIDs[index] {
			return "", nil
		}
	}
	return upstream, append([]oci.Digest(nil), result.BaseDiffIDs...)
}

type directoryArtifact struct {
	root     string
	artifact string
	closed   bool
}

func (artifact *directoryArtifact) FS() fs.FS {
	if artifact == nil || artifact.closed {
		return nil
	}
	return os.DirFS(artifact.artifact)
}

func (artifact *directoryArtifact) Close() error {
	if artifact == nil || artifact.closed {
		return nil
	}
	artifact.closed = true
	return os.RemoveAll(artifact.root)
}

func packError(code lpkgo.Code, op string, cause error) error {
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}
