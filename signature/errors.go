package signature

import (
	"context"
	"fmt"

	lpkgo "github.com/lib-x/lpk-go"
)

func contextError(ctx context.Context, op string) error {
	if ctx == nil {
		return signatureError(lpkgo.CodeInvalidArgument, op, fmt.Errorf("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return signatureError(lpkgo.CodeCancelled, op, err)
	}
	return nil
}

func signatureError(code lpkgo.Code, op string, cause error) error {
	if cause == nil {
		return nil
	}
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}

func integrityError(op string, format string, args ...any) error {
	return signatureError(lpkgo.CodeIntegrityMismatch, op, fmt.Errorf(format, args...))
}
