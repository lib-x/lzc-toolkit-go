package lpkgo

type Code string

const (
	CodeInvalidArgument     Code = "INVALID_ARGUMENT"
	CodeInvalidConfig       Code = "INVALID_CONFIG"
	CodeInvalidManifest     Code = "INVALID_MANIFEST"
	CodeUnsupportedFormat   Code = "UNSUPPORTED_FORMAT"
	CodeIncompatibleBackend Code = "INCOMPATIBLE_BACKEND"
	CodeUnauthenticated     Code = "UNAUTHENTICATED"
	CodePermissionDenied    Code = "PERMISSION_DENIED"
	CodeNotFound            Code = "NOT_FOUND"
	CodeConflict            Code = "CONFLICT"
	CodeCommandFailed       Code = "COMMAND_FAILED"
	CodeRemoteUnavailable   Code = "REMOTE_UNAVAILABLE"
	CodeIntegrityMismatch   Code = "INTEGRITY_MISMATCH"
	CodeCancelled           Code = "CANCELLED"
)

type Error struct {
	Code       Code
	Op         string
	Stage      string
	Path       string
	StatusCode int
	Retryable  bool
	Cause      error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return e != nil && ok && other != nil && other.Code != "" && e.Code == other.Code
}

func Wrap(code Code, op string, cause error) error {
	if cause == nil {
		return nil
	}
	return &Error{Code: code, Op: op, Cause: cause}
}

var (
	ErrInvalidArgument     = &Error{Code: CodeInvalidArgument}
	ErrInvalidConfig       = &Error{Code: CodeInvalidConfig}
	ErrInvalidManifest     = &Error{Code: CodeInvalidManifest}
	ErrUnsupportedFormat   = &Error{Code: CodeUnsupportedFormat}
	ErrIncompatibleBackend = &Error{Code: CodeIncompatibleBackend}
	ErrUnauthenticated     = &Error{Code: CodeUnauthenticated}
	ErrPermissionDenied    = &Error{Code: CodePermissionDenied}
	ErrNotFound            = &Error{Code: CodeNotFound}
	ErrConflict            = &Error{Code: CodeConflict}
	ErrCommandFailed       = &Error{Code: CodeCommandFailed}
	ErrRemoteUnavailable   = &Error{Code: CodeRemoteUnavailable}
	ErrIntegrityMismatch   = &Error{Code: CodeIntegrityMismatch}
	ErrCancelled           = &Error{Code: CodeCancelled}
)
