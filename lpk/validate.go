package lpk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/manifest"
)

func validateWriteRequest(ctx context.Context, request WriteRequest) error {
	if request.Files == nil {
		return containerError(lpkgo.CodeInvalidArgument, "lpk.validate", errors.New("nil filesystem"))
	}
	if request.Layout != LayoutV1 && request.Layout != LayoutV2 {
		return containerError(lpkgo.CodeInvalidArgument, "lpk.validate", errors.New("invalid layout"))
	}
	if request.Layout == LayoutV1 {
		data, err := fs.ReadFile(request.Files, "manifest.yml")
		if err != nil {
			return containerError(lpkgo.CodeInvalidManifest, "lpk.validate", err)
		}
		if err := contextError(ctx, "lpk.validate"); err != nil {
			return err
		}
		if _, err := manifest.Parse(data); err != nil {
			if request.AllowManifestTemplate && isManifestTemplate(data) {
				return nil
			}
			return err
		}
		return nil
	}

	packageData, err := fs.ReadFile(request.Files, "package.yml")
	if err != nil {
		return containerError(lpkgo.CodeInvalidManifest, "lpk.validate", err)
	}
	if err := contextError(ctx, "lpk.validate"); err != nil {
		return err
	}
	packageDocument, err := manifest.Parse(packageData)
	if err != nil {
		return err
	}
	hasManifest := false
	if manifestData, err := fs.ReadFile(request.Files, "manifest.yml"); err == nil {
		hasManifest = true
		if err := contextError(ctx, "lpk.validate"); err != nil {
			return err
		}
		manifestDocument, err := manifest.Parse(manifestData)
		if err != nil {
			if request.AllowManifestTemplate && isManifestTemplate(manifestData) {
				return nil
			}
			return err
		}
		if request.Strict {
			if _, err := manifest.LoadEffective(manifestDocument, packageDocument, true); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return containerError(lpkgo.CodeInvalidManifest, "lpk.validate", err)
	}
	if !hasManifest {
		info, err := fs.Stat(request.Files, "exports")
		if err != nil || !info.IsDir() {
			return invalidRoot("v2 package without manifest.yml requires exports directory")
		}
	}
	return nil
}

func isManifestTemplate(data []byte) bool {
	return bytes.Contains(data, []byte("{{")) && bytes.Contains(data, []byte("}}"))
}

func contextError(ctx context.Context, op string) error {
	if ctx == nil {
		return containerError(lpkgo.CodeInvalidArgument, op, errors.New("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return containerError(lpkgo.CodeCancelled, op, err)
	}
	return nil
}

func containerError(code lpkgo.Code, op string, cause error) error {
	if cause == nil {
		return nil
	}
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}

func nilWriterError(op string) error {
	return containerError(lpkgo.CodeInvalidArgument, op, errors.New("nil writer"))
}

func invalidRoot(format string, args ...any) error {
	return containerError(lpkgo.CodeInvalidManifest, "lpk.validate", fmt.Errorf(format, args...))
}
