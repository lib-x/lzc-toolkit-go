package oci

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

type layoutVersion struct {
	ImageLayoutVersion string `json:"imageLayoutVersion"`
}

func Validate(ctx context.Context, source fs.FS) (Report, error) {
	layout, err := Open(ctx, source)
	if err != nil {
		return Report{}, err
	}
	return layout.Report(), nil
}

func Open(ctx context.Context, source fs.FS) (*Layout, error) {
	if err := ociContextError(ctx, "oci.open"); err != nil {
		return nil, err
	}
	if source == nil {
		return nil, ociError(lpkgo.CodeInvalidArgument, "oci.open", errors.New("nil filesystem"))
	}
	var version layoutVersion
	if err := readJSONFile(ctx, source, "images/oci-layout", &version); err != nil {
		return nil, err
	}
	if version.ImageLayoutVersion != "1.0.0" {
		return nil, ociError(lpkgo.CodeInvalidConfig, "oci.open", fmt.Errorf("unsupported OCI layout version %q", version.ImageLayoutVersion))
	}
	var index Index
	if err := readJSONFile(ctx, source, "images/index.json", &index); err != nil {
		return nil, err
	}
	lockFile, err := source.Open("images.lock")
	if err != nil {
		return nil, ociError(lpkgo.CodeInvalidConfig, "oci.open_lock", err)
	}
	lock, readErr := ReadLock(ctx, lockFile)
	closeErr := lockFile.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, ociError(lpkgo.CodeInvalidConfig, "oci.open_lock", closeErr)
	}
	if index.SchemaVersion != 2 || lock.Version != 1 || len(index.Manifests) == 0 || len(lock.Images) == 0 {
		return nil, ociError(lpkgo.CodeInvalidConfig, "oci.open", errors.New("invalid or empty OCI index/images.lock"))
	}

	layout := &Layout{
		Index:     index,
		Lock:      lock,
		Manifests: make(map[string]Manifest, len(index.Manifests)),
		report: Report{
			ResolvedByAlias: make(map[string]string, len(index.Manifests)),
		},
	}
	seenAliases := make(map[string]struct{}, len(index.Manifests))
	seenEmbedded := make(map[Digest]struct{})
	for _, descriptor := range index.Manifests {
		if err := validateDescriptor(descriptor, MediaTypeImageManifest); err != nil {
			return nil, err
		}
		alias := strings.TrimSpace(descriptor.Annotations[AnnotationRefName])
		if alias == "" {
			return nil, ociError(lpkgo.CodeInvalidConfig, "oci.open_index", errors.New("manifest descriptor alias is required"))
		}
		if _, exists := seenAliases[alias]; exists {
			return nil, ociError(lpkgo.CodeConflict, "oci.open_index", fmt.Errorf("duplicated image alias %q", alias))
		}
		seenAliases[alias] = struct{}{}
		locked, exists := lock.Images[alias]
		if !exists {
			return nil, ociError(lpkgo.CodeInvalidConfig, "oci.open_lock", fmt.Errorf("image alias %q is missing", alias))
		}
		manifestData, err := readVerifiedMetadataBlob(ctx, source, descriptor)
		if err != nil {
			return nil, err
		}
		var imageManifest Manifest
		if err := json.Unmarshal(manifestData, &imageManifest); err != nil {
			return nil, ociError(lpkgo.CodeInvalidConfig, "oci.open_manifest", err)
		}
		if imageManifest.SchemaVersion != 2 || imageManifest.MediaType != MediaTypeImageManifest {
			return nil, ociError(lpkgo.CodeInvalidConfig, "oci.open_manifest", errors.New("invalid image manifest schema or media type"))
		}
		if err := validateDescriptor(imageManifest.Config, MediaTypeImageConfig); err != nil {
			return nil, err
		}
		if locked.ImageID != imageManifest.Config.Digest {
			return nil, ociError(lpkgo.CodeIntegrityMismatch, "oci.open_lock", fmt.Errorf("image id mismatch for alias %q", alias))
		}
		configData, err := readVerifiedMetadataBlob(ctx, source, imageManifest.Config)
		if err != nil {
			return nil, err
		}
		if !json.Valid(configData) {
			return nil, ociError(lpkgo.CodeInvalidConfig, "oci.open_config", fmt.Errorf("invalid image config JSON for alias %q", alias))
		}
		if len(locked.Layers) != len(imageManifest.Layers) {
			return nil, ociError(lpkgo.CodeIntegrityMismatch, "oci.open_lock", fmt.Errorf("layer count mismatch for alias %q", alias))
		}
		for index, layer := range imageManifest.Layers {
			if err := validateLayerDescriptor(layer); err != nil {
				return nil, err
			}
			lockLayer := locked.Layers[index]
			if lockLayer.Digest != layer.Digest {
				return nil, ociError(lpkgo.CodeIntegrityMismatch, "oci.open_lock", fmt.Errorf("layer digest mismatch for alias %q", alias))
			}
			switch lockLayer.Source {
			case LayerSourceEmbed:
				if _, exists := seenEmbedded[layer.Digest]; !exists {
					if err := verifyBlob(ctx, source, layer); err != nil {
						return nil, err
					}
					seenEmbedded[layer.Digest] = struct{}{}
					layout.report.EmbeddedLayerCount++
					layout.report.EmbeddedBytes += layer.Size
				}
			case LayerSourceUpstream:
				if strings.TrimSpace(locked.Upstream) == "" {
					return nil, ociError(lpkgo.CodeInvalidConfig, "oci.open_lock", fmt.Errorf("upstream is required for alias %q", alias))
				}
				layout.report.UpstreamLayerCount++
			default:
				return nil, ociError(lpkgo.CodeInvalidConfig, "oci.open_lock", fmt.Errorf("invalid layer source %q", lockLayer.Source))
			}
		}
		layout.Manifests[alias] = imageManifest
		layout.report.ResolvedByAlias[alias] = locked.ImageID.String()
	}
	for alias := range lock.Images {
		if _, exists := seenAliases[alias]; !exists {
			return nil, ociError(lpkgo.CodeInvalidConfig, "oci.open_lock", fmt.Errorf("alias %q has no index descriptor", alias))
		}
	}
	layout.report.ImageCount = len(seenAliases)
	return layout, nil
}

func readJSONFile(ctx context.Context, source fs.FS, name string, target any) error {
	file, err := source.Open(name)
	if err != nil {
		return ociError(lpkgo.CodeInvalidConfig, "oci.read_metadata", err)
	}
	decodeErr := decodeJSON(ctx, file, target, "oci.read_metadata")
	closeErr := file.Close()
	if decodeErr != nil {
		return decodeErr
	}
	if closeErr != nil {
		return ociError(lpkgo.CodeInvalidConfig, "oci.read_metadata", closeErr)
	}
	return nil
}

func readVerifiedMetadataBlob(ctx context.Context, source fs.FS, descriptor Descriptor) ([]byte, error) {
	if descriptor.Size > maxMetadataBytes {
		return nil, ociError(lpkgo.CodeInvalidConfig, "oci.read_blob", errors.New("OCI metadata blob exceeds limit"))
	}
	name := path.Join("images/blobs/sha256", descriptor.Digest.Hex())
	file, err := source.Open(name)
	if err != nil {
		return nil, ociError(lpkgo.CodeIntegrityMismatch, "oci.read_blob", err)
	}
	hasher := sha256.New()
	limited := io.LimitReader(file, descriptor.Size+1)
	data, readErr := io.ReadAll(io.TeeReader(limited, hasher))
	closeErr := file.Close()
	if readErr != nil {
		return nil, ociError(lpkgo.CodeIntegrityMismatch, "oci.read_blob", readErr)
	}
	if closeErr != nil {
		return nil, ociError(lpkgo.CodeIntegrityMismatch, "oci.read_blob", closeErr)
	}
	if int64(len(data)) != descriptor.Size || fmt.Sprintf("sha256:%x", hasher.Sum(nil)) != descriptor.Digest.String() {
		return nil, ociError(lpkgo.CodeIntegrityMismatch, "oci.read_blob", fmt.Errorf("blob verification failed for %s", descriptor.Digest))
	}
	return data, nil
}

func verifyBlob(ctx context.Context, source fs.FS, descriptor Descriptor) error {
	if err := ociContextError(ctx, "oci.verify_blob"); err != nil {
		return err
	}
	name := path.Join("images/blobs/sha256", descriptor.Digest.Hex())
	file, err := source.Open(name)
	if err != nil {
		return ociError(lpkgo.CodeIntegrityMismatch, "oci.verify_blob", err)
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(hasher, &contextReader{ctx: ctx, reader: file})
	closeErr := file.Close()
	if copyErr != nil {
		return ociError(lpkgo.CodeIntegrityMismatch, "oci.verify_blob", copyErr)
	}
	if closeErr != nil {
		return ociError(lpkgo.CodeIntegrityMismatch, "oci.verify_blob", closeErr)
	}
	if written != descriptor.Size || fmt.Sprintf("sha256:%x", hasher.Sum(nil)) != descriptor.Digest.String() {
		return ociError(lpkgo.CodeIntegrityMismatch, "oci.verify_blob", fmt.Errorf("blob verification failed for %s", descriptor.Digest))
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

func validateDescriptor(descriptor Descriptor, mediaType string) error {
	if descriptor.MediaType != mediaType || !descriptor.Digest.Valid() || descriptor.Size < 0 {
		return ociError(lpkgo.CodeInvalidConfig, "oci.validate_descriptor", errors.New("invalid OCI descriptor"))
	}
	return nil
}

func validateLayerDescriptor(descriptor Descriptor) error {
	if descriptor.MediaType != MediaTypeImageLayer && descriptor.MediaType != MediaTypeImageLayerGzip {
		return ociError(lpkgo.CodeInvalidConfig, "oci.validate_layer", fmt.Errorf("invalid layer media type %q", descriptor.MediaType))
	}
	if !descriptor.Digest.Valid() || descriptor.Size < 0 {
		return ociError(lpkgo.CodeInvalidConfig, "oci.validate_layer", errors.New("invalid layer descriptor"))
	}
	return nil
}

func ociContextError(ctx context.Context, op string) error {
	if ctx == nil {
		return ociError(lpkgo.CodeInvalidArgument, op, errors.New("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return ociError(lpkgo.CodeCancelled, op, err)
	}
	return nil
}

func ociError(code lpkgo.Code, op string, cause error) error {
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}
