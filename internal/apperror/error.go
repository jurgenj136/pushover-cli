// Package apperror defines stable, display-safe command errors and exit mapping.
package apperror

import (
	"errors"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

const (
	CodeInternal = "internal"

	CategoryInternal     = "internal"
	CategoryUsage        = "usage"
	CategoryConfig       = "configuration"
	CategoryValidation   = "validation"
	CategoryQuota        = "quota"
	CategoryNetwork      = "network"
	CategoryConfirmation = "confirmation"
)

// Error carries the stable data renderers may expose. cause is deliberately
// private and Error never includes it, preventing wrapped transport errors from
// leaking secrets or terminal controls into user-facing output.
type Error struct {
	Code      string
	Category  string
	Message   string
	Retryable bool
	Ambiguous bool

	cause   error
	secrets []string
}

func (e *Error) Error() string { return Sanitize(e.Message, e.secrets...) }

func (e *Error) Unwrap() error { return e.cause }

type Option func(*Error)

func WithRetryable(value bool) Option { return func(e *Error) { e.Retryable = value } }
func WithAmbiguous(value bool) Option { return func(e *Error) { e.Ambiguous = value } }
func WithSecrets(values ...string) Option {
	return func(e *Error) { e.secrets = append(e.secrets, values...) }
}

func New(code, category, message string, options ...Option) *Error {
	e := &Error{Code: code, Category: category, Message: message}
	for _, option := range options {
		option(e)
	}
	e.Code = safeCode(e.Code)
	e.Category = safeCategory(e.Category)
	e.Message = Sanitize(e.Message, e.secrets...)
	return e
}

func Wrap(code, category, message string, cause error, options ...Option) *Error {
	e := New(code, category, message, options...)
	e.cause = cause
	return e
}

// SafeDetail is the sole conversion from an internal error to a per-target
// result. It never renders a cause and defaults unknown errors to internal.
func SafeDetail(err error) *domain.ErrorDetail {
	if err == nil {
		return nil
	}
	var typed *Error
	if errors.As(err, &typed) {
		return &domain.ErrorDetail{
			Code:      safeCode(typed.Code),
			Category:  safeCategory(typed.Category),
			Message:   Sanitize(typed.Message, typed.secrets...),
			Retryable: typed.Retryable,
			Ambiguous: typed.Ambiguous,
		}
	}
	return &domain.ErrorDetail{
		Code:     CodeInternal,
		Category: CategoryInternal,
		Message:  Sanitize(err.Error()),
	}
}

func safeCode(value string) string {
	if value == "" {
		return CodeInternal
	}
	return Sanitize(value)
}

func safeCategory(value string) string {
	if value == "" {
		return CategoryInternal
	}
	return Sanitize(value)
}
