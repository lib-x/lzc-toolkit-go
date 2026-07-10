package signature

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	lpkgo "github.com/lib-x/lpk-go"
)

func writeFileAtomic(ctx context.Context, filename string, data []byte, mode os.FileMode) (resultErr error) {
	if err := contextError(ctx, "signature.write_file"); err != nil {
		return err
	}
	if filename == "" || filepath.Base(filepath.Clean(filename)) == "." {
		return signatureError(lpkgo.CodeInvalidArgument, "signature.write_file", fmt.Errorf("invalid filename"))
	}
	directory := filepath.Dir(filename)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return signatureError(lpkgo.CodeCommandFailed, "signature.write_file", err)
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(filename)+".tmp-*")
	if err != nil {
		return signatureError(lpkgo.CodeCommandFailed, "signature.write_file", err)
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
	if _, err := temporary.Write(data); err != nil {
		return signatureError(lpkgo.CodeCommandFailed, "signature.write_file", err)
	}
	if err := contextError(ctx, "signature.write_file"); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return signatureError(lpkgo.CodeCommandFailed, "signature.write_file", err)
	}
	if err := temporary.Close(); err != nil {
		return signatureError(lpkgo.CodeCommandFailed, "signature.write_file", err)
	}
	if err := os.Chmod(temporaryName, mode); err != nil {
		return signatureError(lpkgo.CodeCommandFailed, "signature.write_file", err)
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return signatureError(lpkgo.CodeCommandFailed, "signature.write_file", err)
	}
	committed = true
	return nil
}
