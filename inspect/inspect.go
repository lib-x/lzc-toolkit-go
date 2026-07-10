package inspect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/archive"
	"github.com/lib-x/lzc-toolkit-go/lpk"
	"github.com/lib-x/lzc-toolkit-go/manifest"
)

// Reader inspects an already-open LPK. It does not close r.
func Reader(ctx context.Context, r *lpk.Reader) (Info, error) {
	if err := contextError(ctx, "inspect.reader"); err != nil {
		return Info{}, err
	}
	if r == nil {
		return Info{}, inspectError(lpkgo.CodeInvalidArgument, "inspect.reader", fmt.Errorf("nil LPK reader"))
	}

	entries, err := r.Entries(ctx)
	if err != nil {
		return Info{}, err
	}
	state := scanEntries(entries)
	info := Info{
		Size:           r.Size(),
		Format:         r.Format(),
		Layout:         r.Layout(),
		Signed:         state.signed,
		ResourceOnly:   r.Layout() == lpk.LayoutV2 && !state.hasManifest && state.hasExports,
		HasManifest:    state.hasManifest,
		HasPackageInfo: state.hasPackageInfo,
		HasImagesDir:   state.hasImagesDir,
		HasImagesLock:  state.hasImagesLock,
	}

	effective, err := r.EffectiveManifest(ctx)
	if err != nil {
		return Info{}, err
	}
	info.PackageID, info.AppVersion = packageIdentity(effective)

	if info.HasImagesLock {
		images, err := summarizeImages(ctx, r, state.entriesByName)
		if err != nil {
			return Info{}, err
		}
		info.Images = images
	}
	return info, nil
}

// Stream opens and inspects an LPK from src. It never closes src.
func Stream(ctx context.Context, src io.Reader, options ...lpk.OpenOption) (Info, error) {
	reader, err := lpk.Open(ctx, src, options...)
	if err != nil {
		return Info{}, err
	}
	defer reader.Close()
	return Reader(ctx, reader)
}

// ReaderAt opens and inspects an LPK from random-access data.
func ReaderAt(ctx context.Context, src io.ReaderAt, size int64, options ...lpk.OpenOption) (Info, error) {
	reader, err := lpk.OpenReaderAt(ctx, src, size, options...)
	if err != nil {
		return Info{}, err
	}
	defer reader.Close()
	return Reader(ctx, reader)
}

// File opens and inspects an LPK from a filesystem path.
func File(ctx context.Context, filename string, options ...lpk.OpenOption) (Info, error) {
	reader, err := lpk.OpenFile(ctx, filename, options...)
	if err != nil {
		return Info{}, err
	}
	defer reader.Close()
	return Reader(ctx, reader)
}

type entryState struct {
	entriesByName  map[string]archive.Entry
	hasManifest    bool
	hasPackageInfo bool
	hasImagesDir   bool
	hasImagesLock  bool
	hasExports     bool
	signed         bool
}

func scanEntries(entries []archive.Entry) entryState {
	state := entryState{entriesByName: make(map[string]archive.Entry, len(entries))}
	for _, entry := range entries {
		state.entriesByName[entry.Name] = entry
		switch entry.Name {
		case "manifest.yml":
			state.hasManifest = entry.Type == archive.EntryRegular
		case "package.yml":
			state.hasPackageInfo = entry.Type == archive.EntryRegular
		case "images":
			state.hasImagesDir = entry.Type == archive.EntryDirectory
		case "images.lock":
			state.hasImagesLock = entry.Type == archive.EntryRegular
		case "exports":
			state.hasExports = entry.Type == archive.EntryDirectory
		case "META/release.lock":
			state.signed = entry.Type == archive.EntryRegular
		}
		if strings.HasPrefix(entry.Name, "images/") {
			state.hasImagesDir = true
		}
		if strings.HasPrefix(entry.Name, "exports/") {
			state.hasExports = true
		}
		if strings.HasPrefix(entry.Name, "META/signatures/") && strings.HasSuffix(entry.Name, ".sig") && entry.Type == archive.EntryRegular {
			state.signed = true
		}
	}
	return state
}

func packageIdentity(effective manifest.Effective) (string, string) {
	packageID := effective.Manifest.Package
	version := effective.Manifest.Version
	if packageID == "" && effective.PackageInfo != nil {
		packageID = effective.PackageInfo.Package
	}
	if version == "" && effective.PackageInfo != nil {
		version = effective.PackageInfo.Version
	}
	return packageID, version
}

func readEntryBytes(ctx context.Context, r *lpk.Reader, name string, op string) ([]byte, error) {
	if err := contextError(ctx, op); err != nil {
		return nil, err
	}
	contents, err := r.OpenEntry(ctx, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, inspectError(lpkgo.CodeNotFound, op, err)
		}
		return nil, err
	}
	data, readErr := io.ReadAll(contents)
	closeErr := contents.Close()
	if err := contextError(ctx, op); err != nil {
		return nil, err
	}
	if readErr != nil {
		return nil, inspectError(lpkgo.CodeIntegrityMismatch, op, readErr)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return data, nil
}

func contextError(ctx context.Context, op string) error {
	if ctx == nil {
		return inspectError(lpkgo.CodeInvalidArgument, op, fmt.Errorf("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return inspectError(lpkgo.CodeCancelled, op, err)
	}
	return nil
}

func inspectError(code lpkgo.Code, op string, cause error) error {
	if cause == nil {
		return nil
	}
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}
