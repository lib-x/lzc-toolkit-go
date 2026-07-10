package dockerlocal

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/build"
	imagebuild "github.com/lib-x/lzc-toolkit-go/image"
	"github.com/lib-x/lzc-toolkit-go/image/dockerarchive"
	"github.com/lib-x/lzc-toolkit-go/oci"
)

type Builder struct {
	engine   Engine
	platform string
}

func New(engine Engine, options ...Option) *Builder {
	if engine == nil {
		engine = CLIEngine{}
	}
	builder := &Builder{engine: engine, platform: DefaultPlatform}
	for _, option := range options {
		if option != nil {
			option(builder)
		}
	}
	return builder
}

func (b *Builder) Build(ctx context.Context, request build.ImageBuildRequest) (build.ImageArtifact, error) {
	if ctx == nil {
		return nil, localError(lpkgo.CodeInvalidArgument, "dockerlocal.build", errors.New("nil context"))
	}
	if b == nil || b.engine == nil {
		return nil, localError(lpkgo.CodeInvalidArgument, "dockerlocal.build", errors.New("nil builder or engine"))
	}
	platform := strings.ToLower(strings.TrimSpace(b.platform))
	if !validPlatform(platform) {
		return nil, localError(lpkgo.CodeInvalidArgument, "dockerlocal.build", errors.New("invalid target platform"))
	}
	entries, err := imagebuild.Normalize(ctx, request.Root, request.Manifest, request.Config)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, localError(lpkgo.CodeInvalidConfig, "dockerlocal.build", errors.New("images config is empty"))
	}
	results := make([]BuildResult, 0, len(entries))
	refs := make([]string, 0, len(entries))
	defer func() {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		for _, ref := range refs {
			_ = b.engine.Remove(cleanupContext, ref)
		}
	}()
	for _, entry := range entries {
		if entry.Builder != imagebuild.BuilderLocal {
			return nil, localError(lpkgo.CodeIncompatibleBackend, "dockerlocal.build", errors.New("remote image entry requires a remote builder"))
		}
		built, err := b.engine.Build(ctx, BuildRequest{Entry: entry, Platform: platform})
		if err != nil {
			return nil, localError(lpkgo.CodeCommandFailed, "dockerlocal.build_image", err)
		}
		if err := validateBuildResult(built); err != nil {
			return nil, err
		}
		results = append(results, built)
		refs = append(refs, built.Ref)
	}

	work, err := os.MkdirTemp("", "lpk-dockerlocal-*")
	if err != nil {
		return nil, localError(lpkgo.CodeCommandFailed, "dockerlocal.build", err)
	}
	keepWork := false
	defer func() {
		if !keepWork {
			_ = os.RemoveAll(work)
		}
	}()
	archiveFile, err := os.Create(filepath.Join(work, "images.tar"))
	if err != nil {
		return nil, localError(lpkgo.CodeCommandFailed, "dockerlocal.save", err)
	}
	if err := b.engine.Save(ctx, uniqueStrings(refs), archiveFile); err != nil {
		_ = archiveFile.Close()
		return nil, localError(lpkgo.CodeCommandFailed, "dockerlocal.save", err)
	}
	if _, err := archiveFile.Seek(0, 0); err != nil {
		_ = archiveFile.Close()
		return nil, localError(lpkgo.CodeCommandFailed, "dockerlocal.save", err)
	}
	specs := make([]dockerarchive.Spec, 0, len(results))
	for index, built := range results {
		embedded := built.DiffIDs
		if len(built.UpstreamDiffIDs) > 0 {
			embedded = built.DiffIDs[len(built.UpstreamDiffIDs):]
		}
		specs = append(specs, dockerarchive.Spec{
			Ref:             built.Ref,
			Alias:           entries[index].Alias,
			ImageID:         built.ImageID,
			Upstream:        built.Upstream,
			EmbeddedDiffIDs: append([]oci.Digest(nil), embedded...),
		})
	}
	artifactPath := filepath.Join(work, "artifact")
	_, convertErr := dockerarchive.Convert(ctx, archiveFile, artifactPath, dockerarchive.Request{Specs: specs})
	closeErr := archiveFile.Close()
	if convertErr != nil {
		return nil, convertErr
	}
	if closeErr != nil {
		return nil, localError(lpkgo.CodeCommandFailed, "dockerlocal.save", closeErr)
	}
	keepWork = true
	return &directoryArtifact{root: work, artifact: artifactPath}, nil
}

func validateBuildResult(result BuildResult) error {
	if strings.TrimSpace(result.Ref) == "" || !result.ImageID.Valid() || len(result.DiffIDs) == 0 {
		return localError(lpkgo.CodeInvalidConfig, "dockerlocal.build_result", errors.New("invalid local image build result"))
	}
	for _, digest := range result.DiffIDs {
		if !digest.Valid() {
			return localError(lpkgo.CodeInvalidConfig, "dockerlocal.build_result", errors.New("invalid rootfs diff id"))
		}
	}
	if len(result.UpstreamDiffIDs) > len(result.DiffIDs) {
		return localError(lpkgo.CodeInvalidConfig, "dockerlocal.build_result", errors.New("upstream layer prefix is longer than image layers"))
	}
	for index, digest := range result.UpstreamDiffIDs {
		if digest != result.DiffIDs[index] {
			return localError(lpkgo.CodeInvalidConfig, "dockerlocal.build_result", errors.New("upstream layers are not an image layer prefix"))
		}
	}
	if len(result.UpstreamDiffIDs) > 0 && strings.TrimSpace(result.Upstream) == "" {
		return localError(lpkgo.CodeInvalidConfig, "dockerlocal.build_result", errors.New("upstream reference is required"))
	}
	return nil
}

func validPlatform(platform string) bool {
	parts := strings.Split(platform, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != "" && !strings.ContainsAny(platform, " \\")
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

type directoryArtifact struct {
	root     string
	artifact string
	closed   bool
}

func (a *directoryArtifact) FS() fs.FS {
	if a == nil || a.closed {
		return nil
	}
	return os.DirFS(a.artifact)
}

func (a *directoryArtifact) Close() error {
	if a == nil || a.closed {
		return nil
	}
	a.closed = true
	return os.RemoveAll(a.root)
}

func localError(code lpkgo.Code, op string, cause error) error {
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}
