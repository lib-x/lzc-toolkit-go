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
	"sort"
	"strings"
	"unicode"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

func (r *Reader) Extract(ctx context.Context, destination string) error {
	return r.extract(ctx, destination, nil)
}

func (r *Reader) ExtractSelected(ctx context.Context, destination string, names []string) error {
	if err := contextError(ctx, "archive.extract"); err != nil {
		return err
	}
	if r == nil {
		return archiveError(lpkgo.CodeInvalidArgument, "archive.extract", fmt.Errorf("nil archive reader"))
	}
	selected := make(map[string]struct{}, len(names))
	for _, name := range names {
		normalized, err := validateExtractionPath(name, r.limits.MaxPathBytes, false)
		if err != nil {
			return err
		}
		selected[normalized] = struct{}{}
	}
	return r.extract(ctx, destination, selected)
}

func (r *Reader) extract(ctx context.Context, destination string, selected map[string]struct{}) error {
	if err := contextError(ctx, "archive.extract"); err != nil {
		return err
	}
	if r == nil {
		return archiveError(lpkgo.CodeInvalidArgument, "archive.extract", fmt.Errorf("nil archive reader"))
	}
	if destination == "" {
		return archiveError(lpkgo.CodeInvalidArgument, "archive.extract", fmt.Errorf("empty destination"))
	}
	entries, err := r.Entries(ctx)
	if err != nil {
		return err
	}
	chosen := make([]Entry, 0, len(entries))
	found := make(map[string]struct{}, len(selected))
	entryTypes := make(map[string]EntryType, len(entries))
	for _, entry := range entries {
		normalized, err := validateExtractionPath(entry.Name, r.limits.MaxPathBytes, entry.Type == EntryDirectory)
		if err != nil {
			return err
		}
		entry.Name = normalized
		if err := validateExtractionType(entry); err != nil {
			return err
		}
		switch entry.Type {
		case EntrySymlink:
			entry.Linkname, err = validateSymlinkTarget(entry.Name, entry.Linkname, r.limits.MaxPathBytes)
			if err != nil {
				return err
			}
		case EntryHardlink:
			entry.Linkname, err = validateExtractionPath(entry.Linkname, r.limits.MaxPathBytes, false)
			if err != nil {
				return err
			}
		}
		entryTypes[entry.Name] = entry.Type
		if selected != nil {
			if _, ok := selected[entry.Name]; !ok {
				continue
			}
			found[entry.Name] = struct{}{}
		}
		chosen = append(chosen, entry)
	}
	for _, entry := range entries {
		if entry.Type != EntryHardlink {
			continue
		}
		target, err := validateExtractionPath(entry.Linkname, r.limits.MaxPathBytes, false)
		if err != nil {
			return err
		}
		if entryTypes[target] != EntryRegular {
			return archiveError(lpkgo.CodeInvalidArgument, "archive.extract", fmt.Errorf("hardlink target is not a regular archive entry"))
		}
	}
	if selected != nil {
		for _, entry := range chosen {
			if entry.Type != EntryHardlink {
				continue
			}
			if _, ok := selected[entry.Linkname]; !ok {
				return archiveError(lpkgo.CodeInvalidArgument, "archive.extract", fmt.Errorf("selected hardlink target is not selected"))
			}
		}
	}
	for name := range selected {
		if _, ok := found[name]; !ok {
			return archiveError(lpkgo.CodeNotFound, "archive.extract", fs.ErrNotExist)
		}
	}
	if err := contextError(ctx, "archive.extract"); err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return archiveError(lpkgo.CodeCommandFailed, "archive.extract", err)
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return archiveError(lpkgo.CodeCommandFailed, "archive.extract", err)
	}
	extractErr := r.extractEntries(ctx, root, chosen)
	closeErr := root.Close()
	if extractErr != nil {
		return extractErr
	}
	if closeErr != nil {
		return archiveError(lpkgo.CodeCommandFailed, "archive.extract", closeErr)
	}
	return nil
}

func validateExtractionType(entry Entry) error {
	valid := false
	switch entry.Type {
	case EntryDirectory:
		valid = entry.Mode.IsDir()
	case EntryRegular:
		valid = entry.Mode.IsRegular()
	case EntrySymlink:
		valid = entry.Mode&fs.ModeSymlink != 0
	case EntryHardlink:
		valid = entry.Mode.IsRegular()
	}
	if !valid {
		return archiveError(lpkgo.CodeUnsupportedFormat, "archive.extract", fmt.Errorf("unsupported entry type"))
	}
	return nil
}

func (r *Reader) extractEntries(ctx context.Context, root *os.Root, entries []Entry) error {
	var directories []Entry
	for _, entry := range entries {
		if err := contextError(ctx, "archive.extract"); err != nil {
			return err
		}
		switch entry.Type {
		case EntryDirectory:
			if entry.Name == "." {
				directories = append(directories, entry)
				continue
			}
			if err := ensureNoSymlinkComponents(root, entry.Name); err != nil {
				return err
			}
			if err := root.MkdirAll(entry.Name, 0o755); err != nil {
				return archiveError(lpkgo.CodeCommandFailed, "archive.extract", err)
			}
			directories = append(directories, entry)
		}
	}
	if err := r.extractRegularEntries(ctx, root, entries); err != nil {
		return err
	}
	for _, linkType := range []EntryType{EntryHardlink, EntrySymlink} {
		for _, entry := range entries {
			if entry.Type != linkType {
				continue
			}
			if err := contextError(ctx, "archive.extract"); err != nil {
				return err
			}
			if err := createSafeParents(root, entry.Name); err != nil {
				return err
			}
			if err := ensureNoSymlinkComponents(root, entry.Name); err != nil {
				return err
			}
			var err error
			if entry.Type == EntryHardlink {
				if err := requireRegularHardlinkTarget(root, entry.Linkname); err != nil {
					return err
				}
				err = root.Link(entry.Linkname, entry.Name)
			} else {
				err = root.Symlink(entry.Linkname, entry.Name)
			}
			if err != nil {
				return archiveError(lpkgo.CodeCommandFailed, "archive.extract", err)
			}
		}
	}

	sort.Slice(directories, func(i, j int) bool {
		return strings.Count(directories[i].Name, "/") > strings.Count(directories[j].Name, "/")
	})
	for _, directory := range directories {
		if err := contextError(ctx, "archive.extract"); err != nil {
			return err
		}
		if err := root.Chmod(directory.Name, directory.Mode.Perm()); err != nil {
			return archiveError(lpkgo.CodeCommandFailed, "archive.extract", err)
		}
	}
	return nil
}

func requireRegularHardlinkTarget(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if err != nil {
		return archiveError(lpkgo.CodeConflict, "archive.extract", err)
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return archiveError(lpkgo.CodeConflict, "archive.extract", fmt.Errorf("hardlink target is not a materialized regular file"))
	}
	return nil
}

func (r *Reader) extractRegularEntries(ctx context.Context, root *os.Root, entries []Entry) error {
	regulars := make(map[string]Entry)
	for _, entry := range entries {
		if entry.Type == EntryRegular {
			regulars[entry.Name] = entry
		}
	}
	if len(regulars) == 0 {
		return nil
	}

	var err error
	switch r.format {
	case FormatZIP:
		err = r.extractZIPRegularEntries(ctx, root, regulars)
	case FormatTAR:
		err = r.extractTARRegularEntries(ctx, root, regulars)
	default:
		err = archiveError(lpkgo.CodeUnsupportedFormat, "archive.extract", fmt.Errorf("unsupported format"))
	}
	if err != nil {
		return err
	}
	if len(regulars) != 0 {
		return archiveError(lpkgo.CodeIntegrityMismatch, "archive.extract", fmt.Errorf("selected regular entry contents are missing"))
	}
	return nil
}

func (r *Reader) extractZIPRegularEntries(ctx context.Context, root *os.Root, regulars map[string]Entry) error {
	zr, err := zip.NewReader(r.data, r.size)
	if err != nil {
		return archiveError(lpkgo.CodeIntegrityMismatch, "archive.extract", err)
	}
	for _, file := range zr.File {
		if err := contextError(ctx, "archive.extract"); err != nil {
			return err
		}
		entry, ok := regulars[normalizeName(file.Name)]
		if !ok {
			continue
		}
		if file.Mode().Type() != 0 || int64(file.UncompressedSize64) != entry.Size {
			return archiveError(lpkgo.CodeIntegrityMismatch, "archive.extract", fmt.Errorf("regular ZIP metadata changed during extraction"))
		}
		contents, err := file.Open()
		if err != nil {
			return archiveError(lpkgo.CodeIntegrityMismatch, "archive.extract", err)
		}
		extractErr := extractRegularContent(ctx, root, entry, contents)
		closeErr := contents.Close()
		if extractErr != nil {
			return extractErr
		}
		if closeErr != nil {
			return archiveError(lpkgo.CodeIntegrityMismatch, "archive.extract", closeErr)
		}
		delete(regulars, entry.Name)
	}
	return nil
}

func (r *Reader) extractTARRegularEntries(ctx context.Context, root *os.Root, regulars map[string]Entry) error {
	tr := tar.NewReader(io.NewSectionReader(r.data, 0, r.size))
	for {
		if err := contextError(ctx, "archive.extract"); err != nil {
			return err
		}
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return archiveError(lpkgo.CodeIntegrityMismatch, "archive.extract", err)
		}
		entry, ok := regulars[normalizeName(header.Name)]
		if !ok {
			continue
		}
		if (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || header.Size != entry.Size {
			return archiveError(lpkgo.CodeIntegrityMismatch, "archive.extract", fmt.Errorf("regular TAR metadata changed during extraction"))
		}
		if err := extractRegularContent(ctx, root, entry, tr); err != nil {
			return err
		}
		delete(regulars, entry.Name)
	}
}

func extractRegularContent(ctx context.Context, root *os.Root, entry Entry, source io.Reader) error {
	if err := createSafeParents(root, entry.Name); err != nil {
		return err
	}
	if err := ensureNoSymlinkComponents(root, entry.Name); err != nil {
		return err
	}
	destination, err := root.OpenFile(entry.Name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return archiveError(lpkgo.CodeCommandFailed, "archive.extract", err)
	}

	copyErr := copyExact(ctx, destination, source, entry.Size)
	destinationCloseErr := destination.Close()
	if copyErr != nil {
		return copyErr
	}
	if destinationCloseErr != nil {
		return archiveError(lpkgo.CodeCommandFailed, "archive.extract", destinationCloseErr)
	}
	if err := contextError(ctx, "archive.extract"); err != nil {
		return err
	}
	if err := root.Chmod(entry.Name, entry.Mode.Perm()); err != nil {
		return archiveError(lpkgo.CodeCommandFailed, "archive.extract", err)
	}
	return nil
}

func createSafeParents(root *os.Root, name string) error {
	parent := path.Dir(name)
	if parent == "." {
		return nil
	}
	if err := ensureNoSymlinkComponents(root, parent); err != nil {
		return err
	}
	if err := root.MkdirAll(parent, 0o755); err != nil {
		return archiveError(lpkgo.CodeCommandFailed, "archive.extract", err)
	}
	return nil
}

func ensureNoSymlinkComponents(root *os.Root, name string) error {
	if name == "." {
		return nil
	}
	components := strings.Split(name, "/")
	limit := len(components)
	for i := 0; i < limit; i++ {
		current := path.Join(components[:i+1]...)
		info, err := root.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return archiveError(lpkgo.CodeCommandFailed, "archive.extract", err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return archiveError(lpkgo.CodeConflict, "archive.extract", fmt.Errorf("path traverses a preexisting symlink"))
		}
		if i < limit-1 && !info.IsDir() {
			return archiveError(lpkgo.CodeConflict, "archive.extract", fmt.Errorf("path component is not a directory"))
		}
	}
	return nil
}

func copyExact(ctx context.Context, destination io.Writer, source io.Reader, size int64) error {
	written, err := io.CopyN(destination, &contextReader{ctx: ctx, reader: source}, size)
	if contextErr := contextError(ctx, "archive.extract"); contextErr != nil {
		return contextErr
	}
	if err != nil || written != size {
		return archiveError(lpkgo.CodeIntegrityMismatch, "archive.extract", errors.Join(err, io.ErrUnexpectedEOF))
	}
	var extra [1]byte
	n, err := source.Read(extra[:])
	if contextErr := contextError(ctx, "archive.extract"); contextErr != nil {
		return contextErr
	}
	if n != 0 || !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("entry exceeds declared size")
		}
		return archiveError(lpkgo.CodeIntegrityMismatch, "archive.extract", err)
	}
	return nil
}

func validateExtractionPath(name string, maxBytes int, rootDirectory bool) (string, error) {
	if strings.ContainsRune(name, '\x00') {
		return "", archiveError(lpkgo.CodeInvalidArgument, "archive.extract", fmt.Errorf("path contains NUL"))
	}
	slashName := strings.ReplaceAll(name, "\\", "/")
	if slashName == "" {
		if rootDirectory {
			return ".", nil
		}
		return "", archiveError(lpkgo.CodeInvalidArgument, "archive.extract", fmt.Errorf("empty path"))
	}
	if path.IsAbs(slashName) || hasVolumePrefix(slashName) {
		return "", archiveError(lpkgo.CodeInvalidArgument, "archive.extract", fmt.Errorf("absolute path"))
	}
	normalized := path.Clean(slashName)
	if normalized == "." {
		if rootDirectory {
			return normalized, nil
		}
		return "", archiveError(lpkgo.CodeInvalidArgument, "archive.extract", fmt.Errorf("empty path"))
	}
	if normalized == ".." || strings.HasPrefix(normalized, "../") || len(slashName) > maxBytes || len(normalized) > maxBytes {
		return "", archiveError(lpkgo.CodeInvalidArgument, "archive.extract", fmt.Errorf("path escapes root or exceeds limits"))
	}
	return normalized, nil
}

func validateSymlinkTarget(name, target string, maxBytes int) (string, error) {
	if target == "" || strings.ContainsRune(target, '\x00') {
		return "", archiveError(lpkgo.CodeInvalidArgument, "archive.extract", fmt.Errorf("invalid symlink target"))
	}
	slashTarget := strings.ReplaceAll(target, "\\", "/")
	if path.IsAbs(slashTarget) || hasVolumePrefix(slashTarget) || len(slashTarget) > maxBytes {
		return "", archiveError(lpkgo.CodeInvalidArgument, "archive.extract", fmt.Errorf("symlink target escapes root or exceeds limits"))
	}
	normalizedTarget := path.Clean(slashTarget)
	resolved := path.Clean(path.Join(path.Dir(name), normalizedTarget))
	if path.IsAbs(resolved) || hasVolumePrefix(resolved) || resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "", archiveError(lpkgo.CodeInvalidArgument, "archive.extract", fmt.Errorf("symlink target escapes root"))
	}
	return normalizedTarget, nil
}

func hasVolumePrefix(name string) bool {
	return len(name) >= 2 && unicode.IsLetter(rune(name[0])) && name[1] == ':'
}
