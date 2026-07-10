package archive

import (
	"archive/tar"
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"

	lpkgo "github.com/lib-x/lpk-go"
)

type openConfig struct {
	limits       Limits
	tempDir      string
	filenameHint string
}

type OpenOption func(*openConfig)

func WithLimits(limits Limits) OpenOption {
	return func(config *openConfig) {
		config.limits = limits
	}
}

func WithTempDir(tempDir string) OpenOption {
	return func(config *openConfig) {
		config.tempDir = tempDir
	}
}

func WithFilenameHint(filename string) OpenOption {
	return func(config *openConfig) {
		config.filenameHint = filename
	}
}

type Reader struct {
	format Format
	data   io.ReaderAt
	size   int64
	limits Limits

	cleanup   func() error
	closeOnce sync.Once
	closeErr  error
}

func Open(ctx context.Context, src io.Reader, options ...OpenOption) (*Reader, error) {
	config, err := newOpenConfig(options)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, archiveError(lpkgo.CodeInvalidArgument, "archive.open", fmt.Errorf("nil reader"))
	}
	if err := contextError(ctx, "archive.open"); err != nil {
		return nil, err
	}

	file, err := os.CreateTemp(config.tempDir, "lpk-go-archive-*")
	if err != nil {
		return nil, archiveError(lpkgo.CodeCommandFailed, "archive.open", err)
	}
	name := file.Name()
	cleanup := func() error {
		return errors.Join(file.Close(), os.Remove(name))
	}
	fail := func(err error) (*Reader, error) {
		_ = cleanup()
		return nil, err
	}

	limit := config.limits.MaxInputBytes
	copyLimit := limit + 1
	if copyLimit <= 0 {
		copyLimit = limit
	}
	written, err := io.Copy(file, io.LimitReader(&contextReader{ctx: ctx, reader: src}, copyLimit))
	if err != nil {
		if contextErr := contextError(ctx, "archive.open"); contextErr != nil {
			return fail(contextErr)
		}
		return fail(archiveError(lpkgo.CodeCommandFailed, "archive.open", err))
	}
	if written > limit {
		return fail(archiveError(lpkgo.CodeInvalidArgument, "archive.open", fmt.Errorf("input exceeds limit")))
	}
	format, err := detectFormat(file, written)
	if err != nil {
		return fail(err)
	}
	return &Reader{
		format:  format,
		data:    file,
		size:    written,
		limits:  config.limits,
		cleanup: cleanup,
	}, nil
}

func OpenReaderAt(ctx context.Context, src io.ReaderAt, size int64, options ...OpenOption) (*Reader, error) {
	config, err := newOpenConfig(options)
	if err != nil {
		return nil, err
	}
	if src == nil || size < 0 || size > config.limits.MaxInputBytes {
		return nil, archiveError(lpkgo.CodeInvalidArgument, "archive.open_reader_at", fmt.Errorf("invalid reader or size"))
	}
	if err := contextError(ctx, "archive.open_reader_at"); err != nil {
		return nil, err
	}
	format, err := detectFormat(src, size)
	if err != nil {
		return nil, err
	}
	return &Reader{format: format, data: src, size: size, limits: config.limits}, nil
}

func OpenFile(ctx context.Context, filename string, options ...OpenOption) (*Reader, error) {
	config, err := newOpenConfig(options)
	if err != nil {
		return nil, err
	}
	if err := contextError(ctx, "archive.open_file"); err != nil {
		return nil, err
	}
	file, err := os.Open(filename)
	if err != nil {
		code := lpkgo.CodeCommandFailed
		if errors.Is(err, fs.ErrNotExist) {
			code = lpkgo.CodeNotFound
		}
		return nil, archiveError(code, "archive.open_file", err)
	}
	fail := func(err error) (*Reader, error) {
		_ = file.Close()
		return nil, err
	}
	if err := contextError(ctx, "archive.open_file"); err != nil {
		return fail(err)
	}
	if err := contextError(ctx, "archive.open_file"); err != nil {
		return fail(err)
	}
	info, err := file.Stat()
	if err != nil {
		return fail(archiveError(lpkgo.CodeCommandFailed, "archive.open_file", err))
	}
	if err := contextError(ctx, "archive.open_file"); err != nil {
		return fail(err)
	}
	if info.Size() > config.limits.MaxInputBytes {
		return fail(archiveError(lpkgo.CodeInvalidArgument, "archive.open_file", fmt.Errorf("input exceeds limit")))
	}
	format, err := detectFormat(file, info.Size())
	if err != nil {
		return fail(err)
	}
	return &Reader{
		format:  format,
		data:    file,
		size:    info.Size(),
		limits:  config.limits,
		cleanup: file.Close,
	}, nil
}

func (r *Reader) Format() Format {
	return r.format
}

func (r *Reader) Size() int64 {
	return r.size
}

func (r *Reader) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		if r.cleanup != nil {
			r.closeErr = r.cleanup()
			if r.closeErr != nil {
				r.closeErr = archiveError(lpkgo.CodeCommandFailed, "archive.close", r.closeErr)
			}
		}
	})
	return r.closeErr
}

func (r *Reader) Entries(ctx context.Context) ([]Entry, error) {
	if err := contextError(ctx, "archive.entries"); err != nil {
		return nil, err
	}
	if r == nil || r.data == nil {
		return nil, archiveError(lpkgo.CodeInvalidArgument, "archive.entries", fmt.Errorf("nil archive reader"))
	}
	switch r.format {
	case FormatZIP:
		return r.zipEntries(ctx)
	case FormatTAR:
		return r.tarEntries(ctx)
	default:
		return nil, archiveError(lpkgo.CodeUnsupportedFormat, "archive.entries", fmt.Errorf("unsupported format"))
	}
}

func (r *Reader) OpenEntry(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := contextError(ctx, "archive.open_entry"); err != nil {
		return nil, err
	}
	entries, err := r.Entries(ctx)
	if err != nil {
		return nil, err
	}
	wanted := normalizeName(name)
	var entry *Entry
	for i := range entries {
		if entries[i].Name == wanted {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		return nil, archiveError(lpkgo.CodeNotFound, "archive.open_entry", fs.ErrNotExist)
	}
	if entry.Type != EntryRegular {
		return nil, archiveError(lpkgo.CodeInvalidArgument, "archive.open_entry", fmt.Errorf("entry is not regular"))
	}

	switch r.format {
	case FormatZIP:
		zr, err := zip.NewReader(r.data, r.size)
		if err != nil {
			return nil, archiveError(lpkgo.CodeUnsupportedFormat, "archive.open_entry", err)
		}
		for _, file := range zr.File {
			if err := contextError(ctx, "archive.open_entry"); err != nil {
				return nil, err
			}
			if normalizeName(file.Name) != wanted {
				continue
			}
			contents, err := file.Open()
			if err != nil {
				return nil, archiveError(lpkgo.CodeIntegrityMismatch, "archive.open_entry", err)
			}
			return &contextReadCloser{ctx: ctx, reader: contents}, nil
		}
	case FormatTAR:
		tr := tar.NewReader(io.NewSectionReader(r.data, 0, r.size))
		for {
			if err := contextError(ctx, "archive.open_entry"); err != nil {
				return nil, err
			}
			header, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, archiveError(lpkgo.CodeIntegrityMismatch, "archive.open_entry", err)
			}
			if normalizeName(header.Name) == wanted {
				return &contextReadCloser{ctx: ctx, reader: io.NopCloser(tr)}, nil
			}
		}
	}
	return nil, archiveError(lpkgo.CodeNotFound, "archive.open_entry", fs.ErrNotExist)
}

func (r *Reader) zipEntries(ctx context.Context) ([]Entry, error) {
	zr, err := zip.NewReader(r.data, r.size)
	if err != nil {
		return nil, archiveError(lpkgo.CodeUnsupportedFormat, "archive.entries", err)
	}
	entries := make([]Entry, 0, len(zr.File))
	seen := make(map[string]struct{}, len(zr.File))
	var total int64
	for _, file := range zr.File {
		if err := contextError(ctx, "archive.entries"); err != nil {
			return nil, err
		}
		name := normalizeName(file.Name)
		if _, ok := seen[name]; ok {
			return nil, archiveError(lpkgo.CodeConflict, "archive.entries", fmt.Errorf("duplicate entry"))
		}
		seen[name] = struct{}{}
		mode := file.Mode()
		entryType := EntryRegular
		size := int64(file.UncompressedSize64)
		linkname := ""
		if file.FileInfo().IsDir() || strings.HasSuffix(file.Name, "/") {
			entryType = EntryDirectory
			size = 0
			mode |= fs.ModeDir
		} else if mode&fs.ModeSymlink != 0 {
			entryType = EntrySymlink
		}
		if err := r.validateEntry(name, size, len(entries)+1, &total); err != nil {
			return nil, err
		}
		if entryType == EntrySymlink {
			linkname, err = r.readZIPLink(ctx, file)
			if err != nil {
				return nil, err
			}
		}
		entries = append(entries, Entry{
			Name:     name,
			Type:     entryType,
			Mode:     mode,
			Size:     size,
			Linkname: linkname,
			ModTime:  file.Modified,
		})
	}
	return entries, nil
}

func (r *Reader) readZIPLink(ctx context.Context, file *zip.File) (string, error) {
	contents, err := file.Open()
	if err != nil {
		return "", archiveError(lpkgo.CodeIntegrityMismatch, "archive.entries", err)
	}
	limit := int64(r.limits.MaxPathBytes) + 1
	if limit <= 0 {
		limit = int64(r.limits.MaxPathBytes)
	}
	linkname, readErr := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: contents}, limit))
	closeErr := contents.Close()
	if contextErr := contextError(ctx, "archive.entries"); contextErr != nil {
		return "", contextErr
	}
	if readErr != nil {
		return "", archiveError(lpkgo.CodeIntegrityMismatch, "archive.entries", readErr)
	}
	if closeErr != nil {
		return "", archiveError(lpkgo.CodeIntegrityMismatch, "archive.entries", closeErr)
	}
	if len(linkname) > r.limits.MaxPathBytes {
		return "", archiveError(lpkgo.CodeInvalidArgument, "archive.entries", fmt.Errorf("link target exceeds limits"))
	}
	return string(linkname), nil
}

func (r *Reader) tarEntries(ctx context.Context) ([]Entry, error) {
	tr := tar.NewReader(io.NewSectionReader(r.data, 0, r.size))
	var entries []Entry
	seen := make(map[string]struct{})
	var total int64
	for {
		if err := contextError(ctx, "archive.entries"); err != nil {
			return nil, err
		}
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return entries, nil
		}
		if err != nil {
			return nil, archiveError(lpkgo.CodeUnsupportedFormat, "archive.entries", err)
		}
		name := normalizeName(header.Name)
		if _, ok := seen[name]; ok {
			return nil, archiveError(lpkgo.CodeConflict, "archive.entries", fmt.Errorf("duplicate entry"))
		}
		seen[name] = struct{}{}
		entryType, mode, size, err := tarEntryMetadata(header)
		if err != nil {
			return nil, err
		}
		if len(header.Linkname) > r.limits.MaxPathBytes {
			return nil, archiveError(lpkgo.CodeInvalidArgument, "archive.entries", fmt.Errorf("link target exceeds limits"))
		}
		if err := r.validateEntry(name, size, len(entries)+1, &total); err != nil {
			return nil, err
		}
		entries = append(entries, Entry{
			Name:     name,
			Type:     entryType,
			Mode:     mode,
			Size:     size,
			Linkname: header.Linkname,
			ModTime:  header.ModTime,
		})
	}
}

func tarEntryMetadata(header *tar.Header) (EntryType, fs.FileMode, int64, error) {
	mode := header.FileInfo().Mode()
	switch header.Typeflag {
	case tar.TypeReg, tar.TypeRegA:
		return EntryRegular, mode, header.Size, nil
	case tar.TypeDir:
		return EntryDirectory, mode | fs.ModeDir, 0, nil
	case tar.TypeSymlink:
		return EntrySymlink, mode | fs.ModeSymlink, 0, nil
	case tar.TypeLink:
		return EntryHardlink, mode, 0, nil
	default:
		return "", 0, 0, archiveError(lpkgo.CodeUnsupportedFormat, "archive.entries", fmt.Errorf("unsupported tar entry type"))
	}
}

func (r *Reader) validateEntry(name string, size int64, count int, total *int64) error {
	if name == "" || len(name) > r.limits.MaxPathBytes || count > r.limits.MaxEntries || size < 0 || size > r.limits.MaxFileBytes {
		return archiveError(lpkgo.CodeInvalidArgument, "archive.entries", fmt.Errorf("entry exceeds limits"))
	}
	if size > r.limits.MaxTotalBytes-*total {
		return archiveError(lpkgo.CodeInvalidArgument, "archive.entries", fmt.Errorf("archive exceeds total size"))
	}
	*total += size
	return nil
}

func newOpenConfig(options []OpenOption) (openConfig, error) {
	config := openConfig{limits: DefaultLimits()}
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	limits, err := normalizeLimits(config.limits)
	if err != nil {
		return openConfig{}, err
	}
	config.limits = limits
	return config, nil
}

func detectFormat(data io.ReaderAt, size int64) (Format, error) {
	if size < 2 {
		return "", archiveError(lpkgo.CodeUnsupportedFormat, "archive.detect", fmt.Errorf("input too short"))
	}
	var signature [2]byte
	if _, err := data.ReadAt(signature[:], 0); err != nil {
		return "", archiveError(lpkgo.CodeUnsupportedFormat, "archive.detect", err)
	}
	if string(signature[:]) == "PK" {
		return FormatZIP, nil
	}
	tr := tar.NewReader(io.NewSectionReader(data, 0, size))
	if _, err := tr.Next(); err == nil {
		return FormatTAR, nil
	}
	return "", archiveError(lpkgo.CodeUnsupportedFormat, "archive.detect", fmt.Errorf("unsupported format"))
}

func normalizeName(name string) string {
	return path.Clean(strings.ReplaceAll(name, "\\", "/"))
}

func contextError(ctx context.Context, op string) error {
	if ctx == nil {
		return archiveError(lpkgo.CodeInvalidArgument, op, fmt.Errorf("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return archiveError(lpkgo.CodeCancelled, op, err)
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
	n, err := r.reader.Read(buffer)
	if contextErr := r.ctx.Err(); contextErr != nil {
		return n, contextErr
	}
	return n, err
}

type contextReadCloser struct {
	ctx    context.Context
	reader io.ReadCloser
}

func (r *contextReadCloser) Read(buffer []byte) (int, error) {
	if err := contextError(r.ctx, "archive.read_entry"); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(buffer)
	if contextErr := contextError(r.ctx, "archive.read_entry"); contextErr != nil {
		return n, contextErr
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return n, archiveError(lpkgo.CodeIntegrityMismatch, "archive.read_entry", err)
	}
	return n, err
}

func (r *contextReadCloser) Close() error {
	if err := r.reader.Close(); err != nil {
		return archiveError(lpkgo.CodeCommandFailed, "archive.close_entry", err)
	}
	return nil
}
