package oci

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"go.yaml.in/yaml/v3"
)

const maxMetadataBytes = 4 << 20

func ReadIndex(ctx context.Context, source io.Reader) (Index, error) {
	var index Index
	if err := decodeJSON(ctx, source, &index, "oci.read_index"); err != nil {
		return Index{}, err
	}
	return index, nil
}

func ReadLock(ctx context.Context, source io.Reader) (Lock, error) {
	if err := ociContextError(ctx, "oci.read_lock"); err != nil {
		return Lock{}, err
	}
	if source == nil {
		return Lock{}, ociError(lpkgo.CodeInvalidArgument, "oci.read_lock", errors.New("nil reader"))
	}
	data, err := io.ReadAll(io.LimitReader(source, maxMetadataBytes+1))
	if err != nil {
		return Lock{}, ociError(lpkgo.CodeInvalidConfig, "oci.read_lock", err)
	}
	if len(data) > maxMetadataBytes {
		return Lock{}, ociError(lpkgo.CodeInvalidConfig, "oci.read_lock", errors.New("images.lock exceeds metadata limit"))
	}
	var lock Lock
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return Lock{}, ociError(lpkgo.CodeInvalidConfig, "oci.read_lock", err)
	}
	return lock, nil
}

func decodeJSON(ctx context.Context, source io.Reader, target any, op string) error {
	if err := ociContextError(ctx, op); err != nil {
		return err
	}
	if source == nil {
		return ociError(lpkgo.CodeInvalidArgument, op, errors.New("nil reader"))
	}
	data, err := io.ReadAll(io.LimitReader(source, maxMetadataBytes+1))
	if err != nil {
		return ociError(lpkgo.CodeInvalidConfig, op, err)
	}
	if len(data) > maxMetadataBytes {
		return ociError(lpkgo.CodeInvalidConfig, op, errors.New("OCI metadata exceeds limit"))
	}
	if err := json.Unmarshal(data, target); err != nil {
		return ociError(lpkgo.CodeInvalidConfig, op, err)
	}
	return nil
}
