// Package tokenfile provides optional atomic token persistence.
package tokenfile

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

const maxTokenFileBytes = 64 << 10

type Store struct{ Path string }

type document struct {
	Token string `json:"token"`
}

func (store Store) Load(ctx context.Context) (string, error) {
	if err := checkContext(ctx, "auth.tokenfile.load"); err != nil {
		return "", err
	}
	file, err := os.Open(store.Path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fileError("auth.tokenfile.load", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxTokenFileBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > maxTokenFileBytes {
		return "", fileError("auth.tokenfile.load", errors.Join(readErr, closeErr, errors.New("invalid token file")))
	}
	var value document
	if err := json.Unmarshal(data, &value); err != nil {
		return "", fileError("auth.tokenfile.load", errors.New("invalid token file JSON"))
	}
	return strings.TrimSpace(value.Token), nil
}

func (store Store) Save(ctx context.Context, token string) error {
	if err := checkContext(ctx, "auth.tokenfile.save"); err != nil {
		return err
	}
	normalized := strings.TrimSpace(token)
	if normalized == "" || strings.TrimSpace(store.Path) == "" {
		return &lpkgo.Error{Code: lpkgo.CodeInvalidArgument, Op: "auth.tokenfile.save", Cause: errors.New("empty token or path")}
	}
	data, err := json.Marshal(document{Token: normalized})
	if err != nil {
		return fileError("auth.tokenfile.save", err)
	}
	directory := filepath.Dir(store.Path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fileError("auth.tokenfile.save", err)
	}
	temporary, err := os.CreateTemp(directory, ".token-*")
	if err != nil {
		return fileError("auth.tokenfile.save", err)
	}
	name := temporary.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fileError("auth.tokenfile.save", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fileError("auth.tokenfile.save", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fileError("auth.tokenfile.save", err)
	}
	if err := temporary.Close(); err != nil {
		return fileError("auth.tokenfile.save", err)
	}
	if err := os.Rename(name, store.Path); err != nil {
		return fileError("auth.tokenfile.save", err)
	}
	remove = false
	return nil
}

func (store Store) Delete(ctx context.Context) error {
	if err := checkContext(ctx, "auth.tokenfile.delete"); err != nil {
		return err
	}
	if err := os.Remove(store.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fileError("auth.tokenfile.delete", err)
	}
	return nil
}

func checkContext(ctx context.Context, op string) error {
	if ctx == nil {
		return &lpkgo.Error{Code: lpkgo.CodeInvalidArgument, Op: op, Cause: errors.New("nil context")}
	}
	if err := ctx.Err(); err != nil {
		return &lpkgo.Error{Code: lpkgo.CodeCancelled, Op: op, Cause: err}
	}
	return nil
}

func fileError(op string, cause error) error {
	return &lpkgo.Error{Code: lpkgo.CodeCommandFailed, Op: op, Cause: cause}
}
