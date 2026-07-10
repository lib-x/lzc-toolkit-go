package lpk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/archive"
	"github.com/lib-x/lzc-toolkit-go/manifest"
)

// OpenOption configures LPK reader opening. It is shared with archive.Open.
type OpenOption = archive.OpenOption

// WithLimits configures archive safety limits while opening an LPK.
func WithLimits(limits archive.Limits) OpenOption {
	return archive.WithLimits(limits)
}

// WithTempDir configures where stream inputs are spooled.
func WithTempDir(tempDir string) OpenOption {
	return archive.WithTempDir(tempDir)
}

// WithFilenameHint records the source filename for callers that track it.
func WithFilenameHint(filename string) OpenOption {
	return archive.WithFilenameHint(filename)
}

// Reader provides package-aware access to an opened LPK container.
type Reader struct {
	archiveReader *archive.Reader
	layout        Layout

	mu            sync.Mutex
	manifestCache documentCache
	packageCache  documentCache
}

type documentCache struct {
	loaded   bool
	document *manifest.Document
	err      error
}

// Open reads an LPK from a stream. It never closes src.
func Open(ctx context.Context, src io.Reader, options ...OpenOption) (*Reader, error) {
	reader, err := archive.Open(ctx, src, options...)
	if err != nil {
		return nil, err
	}
	return wrapArchiveReader(ctx, reader)
}

// OpenReaderAt opens an LPK from caller-owned random-access data.
func OpenReaderAt(ctx context.Context, src io.ReaderAt, size int64, options ...OpenOption) (*Reader, error) {
	reader, err := archive.OpenReaderAt(ctx, src, size, options...)
	if err != nil {
		return nil, err
	}
	return wrapArchiveReader(ctx, reader)
}

// OpenFile opens an LPK from a filesystem path.
func OpenFile(ctx context.Context, filename string, options ...OpenOption) (*Reader, error) {
	reader, err := archive.OpenFile(ctx, filename, options...)
	if err != nil {
		return nil, err
	}
	return wrapArchiveReader(ctx, reader)
}

func wrapArchiveReader(ctx context.Context, reader *archive.Reader) (*Reader, error) {
	layout, err := detectLayout(ctx, reader)
	if err != nil {
		return nil, errors.Join(err, reader.Close())
	}
	return &Reader{archiveReader: reader, layout: layout}, nil
}

func detectLayout(ctx context.Context, reader *archive.Reader) (Layout, error) {
	if reader == nil {
		return "", containerError(lpkgo.CodeInvalidArgument, "lpk.open", fmt.Errorf("nil archive reader"))
	}
	switch reader.Format() {
	case archive.FormatTAR:
		return LayoutV2, nil
	case archive.FormatZIP:
		entries, err := reader.Entries(ctx)
		if err != nil {
			return "", err
		}
		for _, entry := range entries {
			if entry.Name == "package.yml" || entry.Name == "images" || strings.HasPrefix(entry.Name, "images/") {
				return LayoutV2, nil
			}
		}
		return LayoutV1, nil
	default:
		return "", containerError(lpkgo.CodeUnsupportedFormat, "lpk.open", fmt.Errorf("unsupported archive format"))
	}
}

// Layout returns the detected LPK layout.
func (r *Reader) Layout() Layout {
	if r == nil {
		return ""
	}
	return r.layout
}

// Format returns the underlying archive format.
func (r *Reader) Format() archive.Format {
	if r == nil || r.archiveReader == nil {
		return ""
	}
	return r.archiveReader.Format()
}

// Size returns the container size in bytes.
func (r *Reader) Size() int64 {
	if r == nil || r.archiveReader == nil {
		return 0
	}
	return r.archiveReader.Size()
}

// Entries returns all archive entries in package order.
func (r *Reader) Entries(ctx context.Context) ([]archive.Entry, error) {
	if err := r.requireReader(ctx, "lpk.entries"); err != nil {
		return nil, err
	}
	return r.archiveReader.Entries(ctx)
}

// Entry returns one archive entry by normalized package path.
func (r *Reader) Entry(ctx context.Context, name string) (archive.Entry, error) {
	if err := r.requireReader(ctx, "lpk.entry"); err != nil {
		return archive.Entry{}, err
	}
	entries, err := r.archiveReader.Entries(ctx)
	if err != nil {
		return archive.Entry{}, err
	}
	wanted := normalizeEntryName(name)
	for _, entry := range entries {
		if entry.Name == wanted {
			return entry, nil
		}
	}
	return archive.Entry{}, containerError(lpkgo.CodeNotFound, "lpk.entry", fs.ErrNotExist)
}

// OpenEntry opens a regular archive entry by package path.
func (r *Reader) OpenEntry(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := r.requireReader(ctx, "lpk.open_entry"); err != nil {
		return nil, err
	}
	return r.archiveReader.OpenEntry(ctx, name)
}

// Extract safely extracts the full LPK contents into destination.
func (r *Reader) Extract(ctx context.Context, destination string) error {
	if err := r.requireReader(ctx, "lpk.extract"); err != nil {
		return err
	}
	return r.archiveReader.Extract(ctx, destination)
}

// Manifest returns a defensive copy of manifest.yml.
func (r *Reader) Manifest(ctx context.Context) (*manifest.Document, error) {
	document, err := r.manifestDocument(ctx)
	if err != nil {
		return nil, err
	}
	return document.Clone(), nil
}

// PackageInfo returns a defensive copy of package.yml or split legacy metadata.
func (r *Reader) PackageInfo(ctx context.Context) (*manifest.Document, error) {
	document, err := r.packageDocument(ctx)
	if err != nil {
		return nil, err
	}
	return document.Clone(), nil
}

// EffectiveManifest returns the typed manifest with package metadata applied.
func (r *Reader) EffectiveManifest(ctx context.Context) (manifest.Effective, error) {
	if err := r.requireReader(ctx, "lpk.effective_manifest"); err != nil {
		return manifest.Effective{}, err
	}
	packageDocument, err := r.packageDocument(ctx)
	if r.layout == LayoutV1 {
		if err != nil {
			return manifest.Effective{}, err
		}
		manifestDocument, err := r.manifestDocument(ctx)
		if err != nil {
			return manifest.Effective{}, err
		}
		return manifest.LoadEffective(manifestDocument, nil, false)
	}
	if err != nil {
		return manifest.Effective{}, err
	}

	manifestDocument, err := r.manifestDocument(ctx)
	if errors.Is(err, fs.ErrNotExist) {
		emptyManifest, parseErr := manifest.Parse([]byte("{}\n"))
		if parseErr != nil {
			return manifest.Effective{}, parseErr
		}
		effective, loadErr := manifest.LoadEffective(emptyManifest, packageDocument, true)
		if loadErr != nil {
			return manifest.Effective{}, loadErr
		}
		return manifest.Effective{
			PackageInfo:    effective.PackageInfo,
			HasPackageFile: true,
		}, nil
	}
	if err != nil {
		return manifest.Effective{}, err
	}
	return manifest.LoadEffective(manifestDocument, packageDocument, true)
}

// Close releases resources owned by the reader. It is safe to call repeatedly.
func (r *Reader) Close() error {
	if r == nil || r.archiveReader == nil {
		return nil
	}
	return r.archiveReader.Close()
}

func (r *Reader) manifestDocument(ctx context.Context) (*manifest.Document, error) {
	if err := r.requireReader(ctx, "lpk.manifest"); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.manifestCache.loaded {
		return r.manifestCache.document, r.manifestCache.err
	}
	document, err := r.readDocument(ctx, "manifest.yml", "lpk.manifest")
	if errors.Is(err, fs.ErrNotExist) {
		err = containerError(lpkgo.CodeNotFound, "lpk.manifest", err)
	}
	r.manifestCache = documentCache{loaded: true, document: document, err: err}
	return document, err
}

func (r *Reader) packageDocument(ctx context.Context) (*manifest.Document, error) {
	if err := r.requireReader(ctx, "lpk.package_info"); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.packageCache.loaded {
		return r.packageCache.document, r.packageCache.err
	}

	var document *manifest.Document
	var err error
	if r.layout == LayoutV2 {
		document, err = r.readDocument(ctx, "package.yml", "lpk.package_info")
		if errors.Is(err, fs.ErrNotExist) {
			err = containerError(lpkgo.CodeNotFound, "lpk.package_info", err)
		}
		r.packageCache = documentCache{loaded: true, document: document, err: err}
		return document, err
	}

	manifestDocument := r.manifestCache.document
	if !r.manifestCache.loaded {
		manifestDocument, err = r.readDocument(ctx, "manifest.yml", "lpk.manifest")
		if errors.Is(err, fs.ErrNotExist) {
			err = containerError(lpkgo.CodeNotFound, "lpk.manifest", err)
		}
		r.manifestCache = documentCache{loaded: true, document: manifestDocument, err: err}
	}
	if err = r.manifestCache.err; err == nil {
		_, document, err = manifest.SplitEffective(manifestDocument, nil, nil)
	}
	r.packageCache = documentCache{loaded: true, document: document, err: err}
	return document, err
}

func (r *Reader) readDocument(ctx context.Context, name string, op string) (*manifest.Document, error) {
	data, err := r.readEntryBytes(ctx, name, op)
	if err != nil {
		return nil, err
	}
	return manifest.Parse(data)
}

func (r *Reader) readEntryBytes(ctx context.Context, name string, op string) ([]byte, error) {
	if err := contextError(ctx, op); err != nil {
		return nil, err
	}
	contents, err := r.archiveReader.OpenEntry(ctx, name)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(contents)
	closeErr := contents.Close()
	if err := contextError(ctx, op); err != nil {
		return nil, err
	}
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return data, nil
}

func (r *Reader) requireReader(ctx context.Context, op string) error {
	if err := contextError(ctx, op); err != nil {
		return err
	}
	if r == nil || r.archiveReader == nil {
		return containerError(lpkgo.CodeInvalidArgument, op, fmt.Errorf("nil LPK reader"))
	}
	return nil
}

func normalizeEntryName(name string) string {
	return path.Clean(strings.ReplaceAll(name, "\\", "/"))
}
