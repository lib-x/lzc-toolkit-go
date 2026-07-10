package buildpack

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/oci"
	"github.com/lib-x/lpk-go/remote"
)

func (builder *Builder) materialize(ctx context.Context, manifest remote.PackManifest, destination string) error {
	if manifest.Index.SchemaVersion != 2 || len(manifest.Index.Manifests) == 0 || len(manifest.LockImages) == 0 || len(manifest.Blobs) == 0 {
		return packError(lpkgo.CodeRemoteUnavailable, "buildpack.pack_manifest", errors.New("invalid or empty pack manifest"))
	}
	missingLocal := make([]oci.Digest, 0)
	seen := make(map[oci.Digest]remote.PackBlob, len(manifest.Blobs))
	for _, blob := range manifest.Blobs {
		if !blob.Digest.Valid() || blob.Size < 0 {
			return packError(lpkgo.CodeRemoteUnavailable, "buildpack.pack_manifest", errors.New("invalid blob descriptor"))
		}
		if _, exists := seen[blob.Digest]; exists {
			return packError(lpkgo.CodeRemoteUnavailable, "buildpack.pack_manifest", errors.New("duplicate blob descriptor"))
		}
		seen[blob.Digest] = blob
		has, err := builder.cache.Has(ctx, blob.Digest.String())
		if err != nil {
			return err
		}
		if !has {
			missingLocal = append(missingLocal, blob.Digest)
		}
	}
	if len(missingLocal) > 0 {
		missingRemote, err := builder.backend.BlobCheck(ctx, missingLocal)
		if err != nil {
			return err
		}
		if len(missingRemote) > 0 {
			return packError(lpkgo.CodeNotFound, "buildpack.blob_check", errors.New("required pack blob is missing on backend"))
		}
		for _, digest := range missingLocal {
			temporary, err := os.CreateTemp("", "lpk-buildpack-blob-*")
			if err != nil {
				return packError(lpkgo.CodeCommandFailed, "buildpack.blob_get", err)
			}
			name := temporary.Name()
			getErr := builder.backend.BlobGet(ctx, digest, temporary)
			closeErr := temporary.Close()
			if getErr != nil || closeErr != nil {
				_ = os.Remove(name)
				return errors.Join(getErr, closeErr)
			}
			file, err := os.Open(name)
			if err != nil {
				_ = os.Remove(name)
				return packError(lpkgo.CodeCommandFailed, "buildpack.blob_get", err)
			}
			info, putErr := builder.cache.Put(ctx, digest.String(), file)
			fileCloseErr := file.Close()
			_ = os.Remove(name)
			if putErr != nil || fileCloseErr != nil {
				return errors.Join(putErr, fileCloseErr)
			}
			if expected := seen[digest].Size; expected > 0 && info.Size != expected {
				return packError(lpkgo.CodeIntegrityMismatch, "buildpack.blob_get", errors.New("downloaded blob size mismatch"))
			}
		}
	}

	blobsDir := filepath.Join(destination, "images", "blobs", "sha256")
	if err := os.MkdirAll(blobsDir, 0o755); err != nil {
		return packError(lpkgo.CodeCommandFailed, "buildpack.materialize", err)
	}
	for digest, blob := range seen {
		file, err := os.Create(filepath.Join(blobsDir, digest.Hex()))
		if err != nil {
			return packError(lpkgo.CodeCommandFailed, "buildpack.materialize", err)
		}
		copyErr := builder.cache.CopyTo(ctx, digest.String(), file)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
		if blob.Size > 0 {
			info, err := os.Stat(filepath.Join(blobsDir, digest.Hex()))
			if err != nil || info.Size() != blob.Size {
				return packError(lpkgo.CodeIntegrityMismatch, "buildpack.materialize", errors.New("materialized blob size mismatch"))
			}
		}
	}
	if err := os.WriteFile(filepath.Join(destination, "images", "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o644); err != nil {
		return packError(lpkgo.CodeCommandFailed, "buildpack.materialize", err)
	}
	indexFile, err := os.Create(filepath.Join(destination, "images", "index.json"))
	if err != nil {
		return packError(lpkgo.CodeCommandFailed, "buildpack.materialize", err)
	}
	writeErr := oci.WriteIndex(ctx, indexFile, manifest.Index)
	closeErr := indexFile.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	lockFile, err := os.Create(filepath.Join(destination, "images.lock"))
	if err != nil {
		return packError(lpkgo.CodeCommandFailed, "buildpack.materialize", err)
	}
	writeErr = oci.WriteLock(ctx, lockFile, oci.Lock{Version: 1, Images: manifest.LockImages})
	closeErr = lockFile.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	_, err = oci.Validate(ctx, os.DirFS(destination))
	return err
}
