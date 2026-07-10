package archive

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

func WriteFileAtomic(ctx context.Context, filename string, source fs.FS, options WriteOptions) (result WriteResult, resultErr error) {
	result.Format = options.Format
	if err := contextError(ctx, "archive.write_file_atomic"); err != nil {
		return result, err
	}
	if filename == "" || filepath.Base(filepath.Clean(filename)) == "." {
		return result, archiveError(lpkgo.CodeInvalidArgument, "archive.write_file_atomic", fmt.Errorf("invalid destination filename"))
	}

	directory := filepath.Dir(filename)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return result, archiveError(lpkgo.CodeCommandFailed, "archive.write_file_atomic", err)
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(filename)+".tmp-*")
	if err != nil {
		return result, archiveError(lpkgo.CodeCommandFailed, "archive.write_file_atomic", err)
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

	result, err = Write(ctx, temporary, source, options)
	if err != nil {
		return result, err
	}
	if err := contextError(ctx, "archive.write_file_atomic"); err != nil {
		return result, err
	}
	if err := temporary.Sync(); err != nil {
		return result, archiveError(lpkgo.CodeCommandFailed, "archive.write_file_atomic", err)
	}
	if err := contextError(ctx, "archive.write_file_atomic"); err != nil {
		return result, err
	}
	if err := temporary.Close(); err != nil {
		return result, archiveError(lpkgo.CodeCommandFailed, "archive.write_file_atomic", err)
	}
	if err := os.Chmod(temporaryName, 0o644); err != nil {
		return result, archiveError(lpkgo.CodeCommandFailed, "archive.write_file_atomic", err)
	}
	if err := contextError(ctx, "archive.write_file_atomic"); err != nil {
		return result, err
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return result, archiveError(lpkgo.CodeCommandFailed, "archive.write_file_atomic", err)
	}
	committed = true
	return result, nil
}
