package apperror

import "fmt"

// Sentinel errors for typed error handling across layers.
// Handlers switch on these instead of matching raw error strings.

type Code int

const (
	ErrValidation   Code = 400
	ErrUnauthorized Code = 401
	ErrForbidden    Code = 403
	ErrNotFound     Code = 404
	ErrConflict     Code = 409
	ErrTooMany      Code = 429
	ErrInternal     Code = 500
)

// AppError carries an HTTP-friendly code + user-facing message.
type AppError struct {
	Code    Code
	Message string
}

func (e *AppError) Error() string { return e.Message }

// Constructors

func Validation(msg string) *AppError   { return &AppError{Code: ErrValidation, Message: msg} }
func Unauthorized(msg string) *AppError { return &AppError{Code: ErrUnauthorized, Message: msg} }
func Forbidden(msg string) *AppError    { return &AppError{Code: ErrForbidden, Message: msg} }
func NotFound(msg string) *AppError     { return &AppError{Code: ErrNotFound, Message: msg} }
func Conflict(msg string) *AppError     { return &AppError{Code: ErrConflict, Message: msg} }
func Internal(msg string) *AppError     { return &AppError{Code: ErrInternal, Message: msg} }

func Wrap(msg string, err error) *AppError {
	return &AppError{Code: ErrInternal, Message: fmt.Sprintf("%s: %v", msg, err)}
}
