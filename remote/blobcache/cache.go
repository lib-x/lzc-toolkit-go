// Package blobcache implements the per-project content-addressed blob cache
// used by lzc-cli remote image builds.
package blobcache

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/oci"
)

type Info struct {
	Digest string
	Size   int64
}

type Cache struct {
	root string
}

func New(projectDir string) Cache {
	absolute, err := filepath.Abs(projectDir)
	if err != nil {
		absolute = filepath.Clean(projectDir)
	}
	return Cache{root: filepath.Join(absolute, ".lzc-cli-cache", "blobs", "sha256")}
}

func (cache Cache) Has(ctx context.Context, digest string) (bool, error) {
	parsed, err := parseDigest(digest)
	if err != nil {
		return false, err
	}
	if err := cacheContext(ctx, "remote.blobcache.has"); err != nil {
		return false, err
	}
	info, err := os.Lstat(cache.blobPath(parsed))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.has", err)
	}
	return info.Mode().IsRegular(), nil
}

func (cache Cache) Open(ctx context.Context, digest string) (io.ReadCloser, error) {
	parsed, err := parseDigest(digest)
	if err != nil {
		return nil, err
	}
	if err := cacheContext(ctx, "remote.blobcache.open"); err != nil {
		return nil, err
	}
	filename := cache.blobPath(parsed)
	pathInfo, err := os.Lstat(filename)
	if err == nil && !pathInfo.Mode().IsRegular() {
		return nil, cacheError(lpkgo.CodeIntegrityMismatch, "remote.blobcache.open", errors.New("cached blob is not a regular file"))
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.open", err)
	}
	file, err := os.Open(filename)
	if err != nil {
		code := lpkgo.CodeCommandFailed
		if errors.Is(err, fs.ErrNotExist) {
			code = lpkgo.CodeNotFound
		}
		return nil, cacheError(code, "remote.blobcache.open", err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, cacheError(lpkgo.CodeIntegrityMismatch, "remote.blobcache.open", errors.New("cached blob is not a regular file"))
	}
	return file, nil
}

func (cache Cache) Put(ctx context.Context, digest string, source io.Reader) (Info, error) {
	parsed, err := parseDigest(digest)
	if err != nil {
		return Info{}, err
	}
	if source == nil {
		return Info{}, cacheError(lpkgo.CodeInvalidArgument, "remote.blobcache.put", errors.New("nil blob reader"))
	}
	return cache.put(ctx, parsed, source)
}

func (cache Cache) PutFile(ctx context.Context, filename string) (Info, error) {
	if err := cacheContext(ctx, "remote.blobcache.put_file"); err != nil {
		return Info{}, err
	}
	file, err := os.Open(filename)
	if err != nil {
		return Info{}, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.put_file", err)
	}
	info, putErr := cache.put(ctx, "", file)
	closeErr := file.Close()
	if putErr != nil {
		return Info{}, putErr
	}
	if closeErr != nil {
		return Info{}, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.put_file", closeErr)
	}
	return info, nil
}

func (cache Cache) CopyTo(ctx context.Context, digest string, destination io.Writer) error {
	if destination == nil {
		return cacheError(lpkgo.CodeInvalidArgument, "remote.blobcache.copy", errors.New("nil destination writer"))
	}
	file, err := cache.Open(ctx, digest)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(destination, &contextReader{ctx: ctx, reader: file})
	closeErr := file.Close()
	if contextErr := cacheContext(ctx, "remote.blobcache.copy"); contextErr != nil {
		return contextErr
	}
	if copyErr != nil || closeErr != nil {
		return cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.copy", errors.Join(copyErr, closeErr))
	}
	return nil
}

func (cache Cache) ImportOCI(ctx context.Context, layout fs.FS) ([]Info, error) {
	if layout == nil {
		return nil, cacheError(lpkgo.CodeInvalidArgument, "remote.blobcache.import_oci", errors.New("nil OCI filesystem"))
	}
	entries, err := fs.ReadDir(layout, "blobs/sha256")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.import_oci", err)
	}
	result := make([]Info, 0, len(entries))
	for _, entry := range entries {
		if err := cacheContext(ctx, "remote.blobcache.import_oci"); err != nil {
			return nil, err
		}
		if entry.IsDir() || len(entry.Name()) != 64 {
			continue
		}
		digest, err := oci.ParseDigest("sha256:" + entry.Name())
		if err != nil {
			continue
		}
		file, err := layout.Open(path.Join("blobs/sha256", entry.Name()))
		if err != nil {
			return nil, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.import_oci", err)
		}
		info, putErr := cache.put(ctx, digest, file)
		closeErr := file.Close()
		if putErr != nil {
			return nil, putErr
		}
		if closeErr != nil {
			return nil, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.import_oci", closeErr)
		}
		result = append(result, info)
	}
	return result, nil
}

func (cache Cache) put(ctx context.Context, expected oci.Digest, source io.Reader) (Info, error) {
	if err := cacheContext(ctx, "remote.blobcache.put"); err != nil {
		return Info{}, err
	}
	if expected != "" {
		if existing, err := cache.verifyExisting(ctx, expected); err != nil {
			return Info{}, err
		} else if existing != nil {
			return *existing, nil
		}
	}
	if err := os.MkdirAll(cache.root, 0o755); err != nil {
		return Info{}, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.put", err)
	}
	temporary, err := os.CreateTemp(cache.root, ".tmp-*")
	if err != nil {
		return Info{}, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.put", err)
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		if committed {
			return
		}
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}()

	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(temporary, hash), &contextReader{ctx: ctx, reader: source})
	if contextErr := cacheContext(ctx, "remote.blobcache.put"); contextErr != nil {
		return Info{}, contextErr
	}
	if copyErr != nil {
		return Info{}, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.put", copyErr)
	}
	actual, _ := oci.ParseDigest("sha256:" + fmt.Sprintf("%x", hash.Sum(nil)))
	if expected != "" && actual != expected {
		return Info{}, cacheError(lpkgo.CodeIntegrityMismatch, "remote.blobcache.put", errors.New("blob digest mismatch"))
	}
	if expected == "" {
		expected = actual
	}
	if err := temporary.Sync(); err != nil {
		return Info{}, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.put", err)
	}
	if err := temporary.Close(); err != nil {
		return Info{}, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.put", err)
	}
	if err := os.Chmod(temporaryName, 0o644); err != nil {
		return Info{}, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.put", err)
	}
	if err := os.Rename(temporaryName, cache.blobPath(expected)); err != nil {
		return Info{}, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.put", err)
	}
	committed = true
	return Info{Digest: expected.String(), Size: size}, nil
}

func (cache Cache) verifyExisting(ctx context.Context, digest oci.Digest) (*Info, error) {
	filename := cache.blobPath(digest)
	pathInfo, err := os.Lstat(filename)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.verify", err)
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, cacheError(lpkgo.CodeIntegrityMismatch, "remote.blobcache.verify", errors.New("cached blob is not a regular file"))
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.verify", err)
	}
	hash := sha256.New()
	size, readErr := io.Copy(hash, &contextReader{ctx: ctx, reader: file})
	closeErr := file.Close()
	if contextErr := cacheContext(ctx, "remote.blobcache.verify"); contextErr != nil {
		return nil, contextErr
	}
	if readErr != nil || closeErr != nil {
		return nil, cacheError(lpkgo.CodeCommandFailed, "remote.blobcache.verify", errors.Join(readErr, closeErr))
	}
	actual, _ := oci.ParseDigest("sha256:" + fmt.Sprintf("%x", hash.Sum(nil)))
	if actual != digest {
		return nil, cacheError(lpkgo.CodeIntegrityMismatch, "remote.blobcache.verify", errors.New("cached blob digest mismatch"))
	}
	return &Info{Digest: digest.String(), Size: size}, nil
}

func (cache Cache) blobPath(digest oci.Digest) string {
	return filepath.Join(cache.root, digest.Hex())
}

func parseDigest(value string) (oci.Digest, error) {
	digest, err := oci.ParseDigest(strings.TrimSpace(value))
	if err != nil {
		return "", cacheError(lpkgo.CodeInvalidArgument, "remote.blobcache.digest", errors.New("invalid sha256 digest"))
	}
	return digest, nil
}

func cacheContext(ctx context.Context, op string) error {
	if ctx == nil {
		return cacheError(lpkgo.CodeInvalidArgument, op, errors.New("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return cacheError(lpkgo.CodeCancelled, op, err)
	}
	return nil
}

func cacheError(code lpkgo.Code, op string, cause error) error {
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
}
