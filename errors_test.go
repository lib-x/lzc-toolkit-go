package lpkgo

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorMatchesStableCode(t *testing.T) {
	cause := fmt.Errorf("disk failed")
	err := &Error{Code: CodeIntegrityMismatch, Op: "archive.read", Path: "app.lpk", Cause: cause}

	if !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatal("expected errors.Is to match the stable code")
	}
	if !errors.Is(err, cause) {
		t.Fatal("expected errors.Is to reach the wrapped cause")
	}
	if got := err.Error(); got != "archive.read app.lpk: INTEGRITY_MISMATCH: disk failed" {
		t.Fatalf("unexpected error string: %q", got)
	}
}

func TestErrorWithoutCause(t *testing.T) {
	err := &Error{Code: CodeInvalidArgument, Op: "lpk.write"}
	if got := err.Error(); got != "lpk.write: INVALID_ARGUMENT" {
		t.Fatalf("unexpected error string: %q", got)
	}
}
