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
	if got := err.Error(); got != "INTEGRITY_MISMATCH" {
		t.Fatalf("unexpected error string: %q", got)
	}
}

func TestErrorDoesNotExposeSensitiveDetails(t *testing.T) {
	err := &Error{
		Code:  CodeUnauthenticated,
		Op:    "auth.login",
		Stage: "token=stage-secret",
		Path:  "/tmp/password=path-secret",
		Cause: errors.New("private-key=cause-secret"),
	}

	if got := err.Error(); got != "UNAUTHENTICATED" {
		t.Fatalf("unexpected error string: %q", got)
	}
}

func TestErrorDoesNotExposeSensitiveOperation(t *testing.T) {
	err := &Error{Code: CodePermissionDenied, Op: "token=operation-secret"}

	if got := err.Error(); got != "PERMISSION_DENIED" {
		t.Fatalf("unexpected error string: %q", got)
	}
}

func TestErrorWithoutCause(t *testing.T) {
	err := &Error{Code: CodeInvalidArgument, Op: "lpk.write"}
	if got := err.Error(); got != "INVALID_ARGUMENT" {
		t.Fatalf("unexpected error string: %q", got)
	}
}

func TestErrorDoesNotMatchTypedNilTarget(t *testing.T) {
	err := &Error{Code: CodeNotFound}
	var target *Error

	if errors.Is(err, target) {
		t.Fatal("expected a typed-nil target not to match")
	}
}

func TestErrorDoesNotMatchWrappedTarget(t *testing.T) {
	err := &Error{Code: CodeNotFound}
	target := fmt.Errorf("wrapped target: %w", ErrNotFound)

	if errors.Is(err, target) {
		t.Fatal("expected only a direct *Error target to match")
	}
}
