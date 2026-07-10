package dockerarchive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/oci"
)

const (
	defaultMaxEntries      = 100_000
	defaultMaxFileBytes    = int64(32 << 30)
	defaultMaxArchiveBytes = int64(128 << 30)
	maxJSONBytes           = int64(16 << 20)
)

type dockerManifest struct {
	Config   string   `json:"Config"`
	RepoTags []string `json:"RepoTags"`
	Layers   []string `json:"Layers"`
}

type dockerConfig struct {
	RootFS struct {
		DiffIDs []oci.Digest `json:"diff_ids"`
	} `json:"rootfs"`
}

type extractedArchive struct {
	root  string
	sizes map[string]int64
}

// Convert consumes source without closing it and atomically creates an OCI
// artifact directory at destination.
func Convert(ctx context.Context, source io.Reader, destination string, request Request) (Result, error) {
	if err := contextError(ctx, "dockerarchive.convert"); err != nil {
		return Result{}, err
	}
	if source == nil || strings.TrimSpace(destination) == "" {
		return Result{}, archiveError(lpkgo.CodeInvalidArgument, "dockerarchive.convert", errors.New("nil reader or empty destination"))
	}
	if _, err := os.Stat(destination); err == nil {
		return Result{}, archiveError(lpkgo.CodeConflict, "dockerarchive.convert", errors.New("destination already exists"))
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, archiveError(lpkgo.CodeCommandFailed, "dockerarchive.convert", err)
	}
	limits := normalizeLimits(request.Limits)
	extracted, cleanup, err := extract(ctx, source, limits)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Result{}, archiveError(lpkgo.CodeCommandFailed, "dockerarchive.convert", err)
	}
	stage, err := os.MkdirTemp(parent, ".lpk-oci-*")
	if err != nil {
		return Result{}, archiveError(lpkgo.CodeCommandFailed, "dockerarchive.convert", err)
	}
	stageLive := true
	defer func() {
		if stageLive {
			_ = os.RemoveAll(stage)
		}
	}()

	result, err := convertExtracted(ctx, extracted, stage, request.Specs)
	if err != nil {
		return Result{}, err
	}
	if err := os.Rename(stage, destination); err != nil {
		return Result{}, archiveError(lpkgo.CodeCommandFailed, "dockerarchive.convert", err)
	}
	stageLive = false
	return result, nil
}

func extract(ctx context.Context, source io.Reader, limits Limits) (extractedArchive, func(), error) {
	root, err := os.MkdirTemp("", "lpk-docker-archive-*")
	if err != nil {
		return extractedArchive{}, nil, archiveError(lpkgo.CodeCommandFailed, "dockerarchive.extract", err)
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	reader := tar.NewReader(&countingReader{reader: source, limit: limits.MaxArchiveBytes})
	sizes := make(map[string]int64)
	entries := 0
	for {
		if err := contextError(ctx, "dockerarchive.extract"); err != nil {
			cleanup()
			return extractedArchive{}, nil, err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			cleanup()
			return extractedArchive{}, nil, archiveError(lpkgo.CodeInvalidConfig, "dockerarchive.extract", err)
		}
		name, ok := normalizeArchivePath(header.Name)
		if !ok {
			cleanup()
			return extractedArchive{}, nil, archiveError(lpkgo.CodeInvalidConfig, "dockerarchive.extract", fmt.Errorf("invalid archive path %q", header.Name))
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			cleanup()
			return extractedArchive{}, nil, archiveError(lpkgo.CodeInvalidConfig, "dockerarchive.extract", fmt.Errorf("unsupported archive entry %q", name))
		}
		entries++
		if entries > limits.MaxEntries || header.Size < 0 || header.Size > limits.MaxFileBytes {
			cleanup()
			return extractedArchive{}, nil, archiveError(lpkgo.CodeInvalidConfig, "dockerarchive.extract", errors.New("archive limit exceeded"))
		}
		if _, duplicate := sizes[name]; duplicate {
			cleanup()
			return extractedArchive{}, nil, archiveError(lpkgo.CodeConflict, "dockerarchive.extract", fmt.Errorf("duplicate archive path %q", name))
		}
		target := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			cleanup()
			return extractedArchive{}, nil, archiveError(lpkgo.CodeCommandFailed, "dockerarchive.extract", err)
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			cleanup()
			return extractedArchive{}, nil, archiveError(lpkgo.CodeCommandFailed, "dockerarchive.extract", err)
		}
		written, copyErr := io.CopyN(output, &contextReader{ctx: ctx, reader: reader}, header.Size)
		closeErr := output.Close()
		if copyErr != nil || written != header.Size || closeErr != nil {
			cleanup()
			return extractedArchive{}, nil, archiveError(lpkgo.CodeInvalidConfig, "dockerarchive.extract", errors.Join(copyErr, closeErr))
		}
		sizes[name] = header.Size
	}
	return extractedArchive{root: root, sizes: sizes}, cleanup, nil
}

func convertExtracted(ctx context.Context, archive extractedArchive, stage string, specs []Spec) (Result, error) {
	if len(specs) == 0 {
		return Result{}, archiveError(lpkgo.CodeInvalidArgument, "dockerarchive.convert", errors.New("specs cannot be empty"))
	}
	manifestData, err := readSmallFile(filepath.Join(archive.root, "manifest.json"))
	if err != nil {
		return Result{}, err
	}
	var manifests []dockerManifest
	if err := json.Unmarshal(manifestData, &manifests); err != nil || len(manifests) == 0 {
		return Result{}, archiveError(lpkgo.CodeInvalidConfig, "dockerarchive.manifest", errors.New("invalid or empty manifest.json"))
	}
	manifestByRef := make(map[string]dockerManifest)
	for _, item := range manifests {
		for _, ref := range item.RepoTags {
			if _, exists := manifestByRef[ref]; exists {
				return Result{}, archiveError(lpkgo.CodeConflict, "dockerarchive.manifest", fmt.Errorf("duplicate image ref %q", ref))
			}
			manifestByRef[ref] = item
		}
	}
	if err := validateSpecs(specs); err != nil {
		return Result{}, err
	}
	blobsDir := filepath.Join(stage, "images", "blobs", "sha256")
	if err := os.MkdirAll(blobsDir, 0o755); err != nil {
		return Result{}, archiveError(lpkgo.CodeCommandFailed, "dockerarchive.convert", err)
	}
	index := oci.Index{SchemaVersion: 2}
	lock := oci.Lock{Version: 1, Images: make(map[string]oci.LockImage, len(specs))}
	result := Result{ImageCount: len(specs)}
	sortedSpecs := append([]Spec(nil), specs...)
	sort.Slice(sortedSpecs, func(i, j int) bool { return sortedSpecs[i].Alias < sortedSpecs[j].Alias })
	seenEmbedded := make(map[oci.Digest]struct{})
	for _, spec := range sortedSpecs {
		if err := contextError(ctx, "dockerarchive.convert"); err != nil {
			return Result{}, err
		}
		item, exists := manifestByRef[spec.Ref]
		if !exists {
			return Result{}, archiveError(lpkgo.CodeNotFound, "dockerarchive.manifest", fmt.Errorf("image ref %q not found", spec.Ref))
		}
		configName, ok := normalizeArchivePath(item.Config)
		if !ok {
			return Result{}, archiveError(lpkgo.CodeInvalidConfig, "dockerarchive.config", errors.New("invalid config path"))
		}
		configData, err := readSmallFile(filepath.Join(archive.root, filepath.FromSlash(configName)))
		if err != nil {
			return Result{}, err
		}
		if digestBytes(configData) != spec.ImageID {
			return Result{}, archiveError(lpkgo.CodeIntegrityMismatch, "dockerarchive.config", fmt.Errorf("image id mismatch for alias %q", spec.Alias))
		}
		var config dockerConfig
		if err := json.Unmarshal(configData, &config); err != nil || len(config.RootFS.DiffIDs) != len(item.Layers) {
			return Result{}, archiveError(lpkgo.CodeInvalidConfig, "dockerarchive.config", fmt.Errorf("layer count mismatch for alias %q", spec.Alias))
		}
		if err := writeBytesBlob(blobsDir, spec.ImageID, configData); err != nil {
			return Result{}, err
		}
		embedded := make(map[oci.Digest]struct{}, len(spec.EmbeddedDiffIDs))
		for _, digest := range spec.EmbeddedDiffIDs {
			embedded[digest] = struct{}{}
		}
		imageManifest := oci.Manifest{
			SchemaVersion: 2,
			MediaType:     oci.MediaTypeImageManifest,
			Config:        oci.Descriptor{MediaType: oci.MediaTypeImageConfig, Digest: spec.ImageID, Size: int64(len(configData))},
		}
		lockImage := oci.LockImage{ImageID: spec.ImageID, Upstream: strings.TrimSpace(spec.Upstream)}
		for index, layerNameRaw := range item.Layers {
			layerName, ok := normalizeArchivePath(layerNameRaw)
			if !ok {
				return Result{}, archiveError(lpkgo.CodeInvalidConfig, "dockerarchive.layer", errors.New("invalid layer path"))
			}
			layerPath := filepath.Join(archive.root, filepath.FromSlash(layerName))
			rawDigest := config.RootFS.DiffIDs[index]
			size, exists := archive.sizes[layerName]
			if !exists {
				return Result{}, archiveError(lpkgo.CodeNotFound, "dockerarchive.layer", fmt.Errorf("layer %q not found", layerName))
			}
			verified, err := digestFile(ctx, layerPath)
			if err != nil || verified != rawDigest {
				return Result{}, archiveError(lpkgo.CodeIntegrityMismatch, "dockerarchive.layer", fmt.Errorf("diff id mismatch for %q", layerName))
			}
			if _, shouldEmbed := embedded[rawDigest]; shouldEmbed {
				compressed, compressedSize, err := gzipBlob(ctx, layerPath, blobsDir)
				if err != nil {
					return Result{}, err
				}
				imageManifest.Layers = append(imageManifest.Layers, oci.Descriptor{MediaType: oci.MediaTypeImageLayerGzip, Digest: compressed, Size: compressedSize})
				lockImage.Layers = append(lockImage.Layers, oci.LockLayer{Digest: compressed, Source: oci.LayerSourceEmbed})
				if _, seen := seenEmbedded[compressed]; !seen {
					seenEmbedded[compressed] = struct{}{}
					result.EmbeddedLayerCount++
					result.EmbeddedBytes += compressedSize
				}
			} else {
				if lockImage.Upstream == "" {
					return Result{}, archiveError(lpkgo.CodeInvalidConfig, "dockerarchive.layer", fmt.Errorf("upstream is required for alias %q", spec.Alias))
				}
				imageManifest.Layers = append(imageManifest.Layers, oci.Descriptor{MediaType: oci.MediaTypeImageLayer, Digest: rawDigest, Size: size})
				lockImage.Layers = append(lockImage.Layers, oci.LockLayer{Digest: rawDigest, Source: oci.LayerSourceUpstream})
				result.UpstreamLayerCount++
			}
		}
		var manifestBuffer bytes.Buffer
		if err := oci.WriteManifest(ctx, &manifestBuffer, imageManifest); err != nil {
			return Result{}, err
		}
		manifestDigest := digestBytes(manifestBuffer.Bytes())
		if err := writeBytesBlob(blobsDir, manifestDigest, manifestBuffer.Bytes()); err != nil {
			return Result{}, err
		}
		index.Manifests = append(index.Manifests, oci.Descriptor{
			MediaType: oci.MediaTypeImageManifest,
			Digest:    manifestDigest,
			Size:      int64(manifestBuffer.Len()),
			Annotations: map[string]string{
				oci.AnnotationRefName: spec.Alias,
			},
		})
		lock.Images[spec.Alias] = lockImage
	}
	if err := os.WriteFile(filepath.Join(stage, "images", "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o644); err != nil {
		return Result{}, archiveError(lpkgo.CodeCommandFailed, "dockerarchive.write", err)
	}
	indexFile, err := os.Create(filepath.Join(stage, "images", "index.json"))
	if err != nil {
		return Result{}, archiveError(lpkgo.CodeCommandFailed, "dockerarchive.write", err)
	}
	writeErr := oci.WriteIndex(ctx, indexFile, index)
	closeErr := indexFile.Close()
	if writeErr != nil || closeErr != nil {
		return Result{}, errors.Join(writeErr, closeErr)
	}
	lockFile, err := os.Create(filepath.Join(stage, "images.lock"))
	if err != nil {
		return Result{}, archiveError(lpkgo.CodeCommandFailed, "dockerarchive.write", err)
	}
	writeErr = oci.WriteLock(ctx, lockFile, lock)
	closeErr = lockFile.Close()
	if writeErr != nil || closeErr != nil {
		return Result{}, errors.Join(writeErr, closeErr)
	}
	if _, err := oci.Validate(ctx, os.DirFS(stage)); err != nil {
		return Result{}, err
	}
	return result, nil
}

func validateSpecs(specs []Spec) error {
	aliases := make(map[string]struct{}, len(specs))
	refs := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if strings.TrimSpace(spec.Alias) == "" || strings.TrimSpace(spec.Ref) == "" || !spec.ImageID.Valid() {
			return archiveError(lpkgo.CodeInvalidArgument, "dockerarchive.spec", errors.New("invalid image spec"))
		}
		if _, exists := aliases[spec.Alias]; exists {
			return archiveError(lpkgo.CodeConflict, "dockerarchive.spec", fmt.Errorf("duplicate alias %q", spec.Alias))
		}
		aliases[spec.Alias] = struct{}{}
		if _, exists := refs[spec.Ref]; exists {
			return archiveError(lpkgo.CodeConflict, "dockerarchive.spec", fmt.Errorf("duplicate ref %q", spec.Ref))
		}
		refs[spec.Ref] = struct{}{}
		for _, digest := range spec.EmbeddedDiffIDs {
			if !digest.Valid() {
				return archiveError(lpkgo.CodeInvalidArgument, "dockerarchive.spec", errors.New("invalid embedded diff id"))
			}
		}
	}
	return nil
}

func gzipBlob(ctx context.Context, sourcePath, blobsDir string) (oci.Digest, int64, error) {
	input, err := os.Open(sourcePath)
	if err != nil {
		return "", 0, archiveError(lpkgo.CodeCommandFailed, "dockerarchive.gzip", err)
	}
	temporary, err := os.CreateTemp(blobsDir, ".layer-*")
	if err != nil {
		_ = input.Close()
		return "", 0, archiveError(lpkgo.CodeCommandFailed, "dockerarchive.gzip", err)
	}
	temporaryName := temporary.Name()
	hasher := sha256.New()
	counter := &countingWriter{}
	compressed := gzip.NewWriter(io.MultiWriter(temporary, hasher, counter))
	_, copyErr := io.Copy(compressed, &contextReader{ctx: ctx, reader: input})
	gzipCloseErr := compressed.Close()
	inputCloseErr := input.Close()
	fileCloseErr := temporary.Close()
	if copyErr != nil || gzipCloseErr != nil || inputCloseErr != nil || fileCloseErr != nil {
		_ = os.Remove(temporaryName)
		return "", 0, archiveError(lpkgo.CodeCommandFailed, "dockerarchive.gzip", errors.Join(copyErr, gzipCloseErr, inputCloseErr, fileCloseErr))
	}
	digest, err := oci.ParseDigest(fmt.Sprintf("sha256:%x", hasher.Sum(nil)))
	if err != nil {
		_ = os.Remove(temporaryName)
		return "", 0, archiveError(lpkgo.CodeIntegrityMismatch, "dockerarchive.gzip", err)
	}
	target := filepath.Join(blobsDir, digest.Hex())
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(temporaryName, target); err != nil {
			_ = os.Remove(temporaryName)
			return "", 0, archiveError(lpkgo.CodeCommandFailed, "dockerarchive.gzip", err)
		}
	} else {
		_ = os.Remove(temporaryName)
		if err != nil {
			return "", 0, archiveError(lpkgo.CodeCommandFailed, "dockerarchive.gzip", err)
		}
	}
	return digest, counter.written, nil
}

func writeBytesBlob(blobsDir string, digest oci.Digest, data []byte) error {
	if digestBytes(data) != digest {
		return archiveError(lpkgo.CodeIntegrityMismatch, "dockerarchive.write_blob", errors.New("blob digest mismatch"))
	}
	filename := filepath.Join(blobsDir, digest.Hex())
	if _, err := os.Stat(filename); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return archiveError(lpkgo.CodeCommandFailed, "dockerarchive.write_blob", err)
	}
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		return archiveError(lpkgo.CodeCommandFailed, "dockerarchive.write_blob", err)
	}
	return nil
}

func digestFile(ctx context.Context, filename string) (oci.Digest, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(hasher, &contextReader{ctx: ctx, reader: file})
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return "", errors.Join(copyErr, closeErr)
	}
	return oci.ParseDigest(fmt.Sprintf("sha256:%x", hasher.Sum(nil)))
}

func digestBytes(data []byte) oci.Digest {
	sum := sha256.Sum256(data)
	digest, _ := oci.ParseDigest(fmt.Sprintf("sha256:%x", sum[:]))
	return digest
}

func readSmallFile(filename string) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, archiveError(lpkgo.CodeNotFound, "dockerarchive.read", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxJSONBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, archiveError(lpkgo.CodeInvalidConfig, "dockerarchive.read", errors.Join(readErr, closeErr))
	}
	if int64(len(data)) > maxJSONBytes {
		return nil, archiveError(lpkgo.CodeInvalidConfig, "dockerarchive.read", errors.New("JSON metadata exceeds limit"))
	}
	return data, nil
}

func normalizeArchivePath(name string) (string, bool) {
	normalized := strings.TrimPrefix(strings.TrimSpace(name), "./")
	return normalized, normalized != "" && normalized != "." && fs.ValidPath(normalized) && path.Clean(normalized) == normalized && !strings.ContainsRune(normalized, '\\')
}

func normalizeLimits(limits Limits) Limits {
	if limits.MaxEntries <= 0 {
		limits.MaxEntries = defaultMaxEntries
	}
	if limits.MaxFileBytes <= 0 {
		limits.MaxFileBytes = defaultMaxFileBytes
	}
	if limits.MaxArchiveBytes <= 0 {
		limits.MaxArchiveBytes = defaultMaxArchiveBytes
	}
	return limits
}

type countingReader struct {
	reader io.Reader
	read   int64
	limit  int64
}

func (r *countingReader) Read(data []byte) (int, error) {
	if r.read >= r.limit {
		return 0, errors.New("archive size limit exceeded")
	}
	if int64(len(data)) > r.limit-r.read {
		data = data[:r.limit-r.read]
	}
	n, err := r.reader.Read(data)
	r.read += int64(n)
	return n, err
}

type countingWriter struct{ written int64 }

func (w *countingWriter) Write(data []byte) (int, error) {
	w.written += int64(len(data))
	return len(data), nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func contextError(ctx context.Context, op string) error {
	if ctx == nil {
		return archiveError(lpkgo.CodeInvalidArgument, op, errors.New("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return archiveError(lpkgo.CodeCancelled, op, err)
	}
	return nil
}

func archiveError(code lpkgo.Code, op string, cause error) error {
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}
