// Package apperror defines stable, display-safe command errors and exit mapping.
package apperror

import (
	"errors"
	"fmt"
	"io"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

const (
	CodeInternal    = "internal"
	MessageInternal = "internal error"

	CategoryInternal       = "internal"
	CategoryUsage          = "usage"
	CategoryConfig         = "configuration"
	CategoryAuthentication = "authentication"
	CategoryValidation     = "validation"
	CategoryQuota          = "quota"
	CategoryNetwork        = "network"
	CategoryConfirmation   = "confirmation"
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

func (e *Error) Error() string {
	if e == nil {
		return MessageInternal
	}
	return Sanitize(e.Message, e.secrets...)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Format prevents fmt's %#v and %+v struct paths from exposing the retained
// cause or secret registry. Every verb emits only the sanitized safe message.
func (e *Error) Format(state fmt.State, verb rune) {
	message := e.Error()
	if verb == 'q' {
		_, _ = fmt.Fprintf(state, "%q", message)
		return
	}
	_, _ = io.WriteString(state, message)
}

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
	if errors.As(err, &typed) && typed != nil {
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
		Message:  MessageInternal,
	}
}

func safeCode(value string) string {
	if value == "" {
		return CodeInternal
	}
	return Sanitize(value)
}

func safeCategory(value string) string {
	switch value {
	case CategoryInternal, CategoryUsage, CategoryConfig, CategoryAuthentication, CategoryValidation, CategoryQuota, CategoryNetwork, CategoryConfirmation:
		return value
	default:
		return CategoryInternal
	}
}
