package signature

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/manifest"
)

func collectObjects(ctx context.Context, root fs.FS) ([]releaseObject, error) {
	if err := contextError(ctx, "signature.collect_objects"); err != nil {
		return nil, err
	}
	var names []string
	err := fs.WalkDir(root, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return signatureError(lpkgo.CodeCommandFailed, "signature.collect_objects", walkErr)
		}
		if err := contextError(ctx, "signature.collect_objects"); err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
		if clean == "META" || strings.HasPrefix(clean, "META/") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return signatureError(lpkgo.CodeCommandFailed, "signature.collect_objects", err)
		}
		if info.Mode().IsRegular() {
			names = append(names, clean)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	objects := make([]releaseObject, 0, len(names))
	for _, name := range names {
		digest, size, err := hashFSFile(ctx, root, name, "signature.collect_objects")
		if err != nil {
			return nil, err
		}
		objects = append(objects, releaseObject{
			Path:   name,
			Digest: "sha256:" + digest,
			Size:   size,
		})
	}
	return objects, nil
}

func hashFSFile(ctx context.Context, root fs.FS, name string, op string) (string, int64, error) {
	if err := contextError(ctx, op); err != nil {
		return "", 0, err
	}
	file, err := root.Open(name)
	if err != nil {
		return "", 0, signatureError(lpkgo.CodeCommandFailed, op, err)
	}
	defer file.Close()

	hasher := sha256.New()
	counter := &countingWriter{}
	if _, err := io.Copy(io.MultiWriter(hasher, counter), file); err != nil {
		return "", 0, signatureError(lpkgo.CodeCommandFailed, op, err)
	}
	if err := contextError(ctx, op); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), counter.written, nil
}

func packageIdentityFromDir(directory string) (string, string, error) {
	manifestPath := pathFromSlash(directory, "manifest.yml")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		if errorsIsNotExist(err) {
			return "", "", nil
		}
		return "", "", signatureError(lpkgo.CodeInvalidManifest, "signature.package_identity", err)
	}
	manifestDocument, err := manifest.Parse(manifestData)
	if err != nil {
		return "", "", err
	}
	var packageDocument *manifest.Document
	packageData, err := os.ReadFile(pathFromSlash(directory, "package.yml"))
	if err == nil {
		packageDocument, err = manifest.Parse(packageData)
		if err != nil {
			return "", "", err
		}
	} else if !errorsIsNotExist(err) {
		return "", "", signatureError(lpkgo.CodeInvalidManifest, "signature.package_identity", err)
	}
	effective, err := manifest.LoadEffective(manifestDocument, packageDocument, false)
	if err != nil {
		return "", "", err
	}
	return effective.Manifest.Package, effective.Manifest.Version, nil
}

func hasSignatureData(root string) bool {
	if _, err := os.Stat(pathFromSlash(root, releaseLockPath)); err == nil {
		return true
	}
	entries, err := os.ReadDir(pathFromSlash(root, "META/signatures"))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sig") {
			return true
		}
	}
	return false
}

func pathFromSlash(root string, name string) string {
	return root + string(os.PathSeparator) + strings.ReplaceAll(name, "/", string(os.PathSeparator))
}

func errorsIsNotExist(err error) bool {
	return os.IsNotExist(err)
}

type countingWriter struct {
	written int64
}

func (w *countingWriter) Write(buffer []byte) (int, error) {
	w.written += int64(len(buffer))
	return len(buffer), nil
}

func invalidMetadata(format string, args ...any) error {
	return integrityError("signature.metadata", format, args...)
}
