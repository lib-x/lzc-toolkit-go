package oci

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	lpkgo "github.com/lib-x/lpk-go"
	"go.yaml.in/yaml/v3"
)

func WriteIndex(ctx context.Context, destination io.Writer, index Index) error {
	return writeJSON(ctx, destination, index, "oci.write_index", true)
}

func WriteManifest(ctx context.Context, destination io.Writer, manifest Manifest) error {
	return writeJSON(ctx, destination, manifest, "oci.write_manifest", false)
}

func WriteLock(ctx context.Context, destination io.Writer, lock Lock) error {
	if err := ociContextError(ctx, "oci.write_lock"); err != nil {
		return err
	}
	if destination == nil {
		return ociError(lpkgo.CodeInvalidArgument, "oci.write_lock", errors.New("nil writer"))
	}
	encoder := yaml.NewEncoder(&contextWriter{ctx: ctx, writer: destination})
	encoder.SetIndent(2)
	if err := encoder.Encode(lock); err != nil {
		return ociError(lpkgo.CodeCommandFailed, "oci.write_lock", err)
	}
	if err := encoder.Close(); err != nil {
		return ociError(lpkgo.CodeCommandFailed, "oci.write_lock", err)
	}
	return nil
}

func writeJSON(ctx context.Context, destination io.Writer, value any, op string, indent bool) error {
	if err := ociContextError(ctx, op); err != nil {
		return err
	}
	if destination == nil {
		return ociError(lpkgo.CodeInvalidArgument, op, errors.New("nil writer"))
	}
	encoder := json.NewEncoder(&contextWriter{ctx: ctx, writer: destination})
	if indent {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(value); err != nil {
		return ociError(lpkgo.CodeCommandFailed, op, err)
	}
	return nil
}

type contextWriter struct {
	ctx    context.Context
	writer io.Writer
}

func (w *contextWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.writer.Write(data)
}
