package archive

import (
	"archive/tar"
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	lpkgo "github.com/lib-x/lpk-go"
)

type WriteOptions struct {
	Format       Format
	Reproducible bool
}

type WriteResult struct {
	Format Format
	Size   int64
	SHA256 [32]byte
}

func Write(ctx context.Context, dst io.Writer, source fs.FS, options WriteOptions) (result WriteResult, resultErr error) {
	result.Format = options.Format
	if dst == nil || source == nil {
		return result, archiveError(lpkgo.CodeInvalidArgument, "archive.write", fmt.Errorf("nil writer or filesystem"))
	}
	if err := contextError(ctx, "archive.write"); err != nil {
		return result, err
	}
	if options.Format != FormatZIP && options.Format != FormatTAR {
		return result, archiveError(lpkgo.CodeUnsupportedFormat, "archive.write", fmt.Errorf("unsupported format"))
	}

	entries, err := collectWriteEntries(ctx, source)
	if err != nil {
		return result, err
	}
	hasher := sha256.New()
	counter := &countingWriter{}
	output := io.MultiWriter(dst, hasher, counter)
	defer func() {
		result.Size = counter.written
		copy(result.SHA256[:], hasher.Sum(nil))
	}()

	switch options.Format {
	case FormatZIP:
		resultErr = writeZIP(ctx, output, source, entries, options.Reproducible)
	case FormatTAR:
		resultErr = writeTAR(ctx, output, source, entries, options.Reproducible)
	}
	return result, resultErr
}

type writeEntry struct {
	name string
	info fs.FileInfo
}

func collectWriteEntries(ctx context.Context, source fs.FS) ([]writeEntry, error) {
	var entries []writeEntry
	err := fs.WalkDir(source, ".", func(name string, directory fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return archiveError(lpkgo.CodeCommandFailed, "archive.walk", walkErr)
		}
		if err := contextError(ctx, "archive.walk"); err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if !localArchivePath(name) {
			return archiveError(lpkgo.CodeInvalidArgument, "archive.walk", fmt.Errorf("non-local path"))
		}
		info, err := directory.Info()
		if err != nil {
			return archiveError(lpkgo.CodeCommandFailed, "archive.walk", err)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return archiveError(lpkgo.CodeInvalidArgument, "archive.walk", fmt.Errorf("unsupported file type"))
		}
		entries = append(entries, writeEntry{name: name, info: info})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	return entries, nil
}

func localArchivePath(name string) bool {
	return fs.ValidPath(name) && name != "." && path.Clean(name) == name && !strings.ContainsRune(name, '\\')
}

func writeZIP(ctx context.Context, dst io.Writer, source fs.FS, entries []writeEntry, reproducible bool) error {
	zw := zip.NewWriter(&contextWriter{ctx: ctx, writer: dst, op: "archive.write_zip"})
	for _, entry := range entries {
		if err := contextError(ctx, "archive.write_zip"); err != nil {
			return err
		}
		name := entry.name
		header := &zip.FileHeader{Name: name}
		header.SetMode(entry.info.Mode())
		if reproducible {
			header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		} else if !entry.info.ModTime().IsZero() {
			header.SetModTime(entry.info.ModTime())
		}
		if entry.info.IsDir() {
			header.Name += "/"
			header.Method = zip.Store
		} else {
			header.Method = zip.Deflate
		}
		writer, err := zw.CreateHeader(header)
		if err != nil {
			if contextErr := contextError(ctx, "archive.write_zip"); contextErr != nil {
				return contextErr
			}
			return archiveError(lpkgo.CodeCommandFailed, "archive.write_zip", err)
		}
		if entry.info.IsDir() {
			continue
		}
		if err := copySourceFile(ctx, writer, source, entry.name, "archive.write_zip"); err != nil {
			return err
		}
	}
	if err := contextError(ctx, "archive.write_zip"); err != nil {
		return err
	}
	closeErr := zw.Close()
	if err := contextError(ctx, "archive.write_zip"); err != nil {
		return err
	}
	if closeErr != nil {
		return archiveError(lpkgo.CodeCommandFailed, "archive.write_zip", closeErr)
	}
	return nil
}

func writeTAR(ctx context.Context, dst io.Writer, source fs.FS, entries []writeEntry, reproducible bool) error {
	tw := tar.NewWriter(&contextWriter{ctx: ctx, writer: dst, op: "archive.write_tar"})
	for _, entry := range entries {
		if err := contextError(ctx, "archive.write_tar"); err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(entry.info, "")
		if err != nil {
			return archiveError(lpkgo.CodeCommandFailed, "archive.write_tar", err)
		}
		header.Name = entry.name
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		if reproducible {
			header.ModTime = time.Unix(0, 0).UTC()
		} else {
			header.ModTime = entry.info.ModTime()
		}
		if entry.info.IsDir() {
			header.Name += "/"
		}
		if err := tw.WriteHeader(header); err != nil {
			if contextErr := contextError(ctx, "archive.write_tar"); contextErr != nil {
				return contextErr
			}
			return archiveError(lpkgo.CodeCommandFailed, "archive.write_tar", err)
		}
		if entry.info.IsDir() {
			continue
		}
		if err := copySourceFile(ctx, tw, source, entry.name, "archive.write_tar"); err != nil {
			return err
		}
	}
	if err := contextError(ctx, "archive.write_tar"); err != nil {
		return err
	}
	closeErr := tw.Close()
	if err := contextError(ctx, "archive.write_tar"); err != nil {
		return err
	}
	if closeErr != nil {
		return archiveError(lpkgo.CodeCommandFailed, "archive.write_tar", closeErr)
	}
	return nil
}

func copySourceFile(ctx context.Context, dst io.Writer, source fs.FS, name, op string) error {
	file, err := source.Open(name)
	if err != nil {
		return archiveError(lpkgo.CodeCommandFailed, op, err)
	}
	_, copyErr := io.Copy(dst, &contextReader{ctx: ctx, reader: file})
	closeErr := file.Close()
	if contextErr := contextError(ctx, op); contextErr != nil {
		return contextErr
	}
	if copyErr != nil {
		return archiveError(lpkgo.CodeCommandFailed, op, copyErr)
	}
	if closeErr != nil {
		return archiveError(lpkgo.CodeCommandFailed, op, closeErr)
	}
	return nil
}

type countingWriter struct {
	written int64
}

type contextWriter struct {
	ctx    context.Context
	writer io.Writer
	op     string
}

func (w *contextWriter) Write(buffer []byte) (int, error) {
	if err := contextError(w.ctx, w.op); err != nil {
		return 0, err
	}
	n, err := w.writer.Write(buffer)
	if contextErr := contextError(w.ctx, w.op); contextErr != nil {
		return n, contextErr
	}
	return n, err
}

func (w *countingWriter) Write(buffer []byte) (int, error) {
	w.written += int64(len(buffer))
	return len(buffer), nil
}
