package pushover

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
)

// SendError retains an internal cause for programmatic classification. Its
// string form is fixed so credentials, paths, and hostile upstream text stay
// out of diagnostics.
type SendError struct {
	Cause                error
	Phase                TransmissionPhase
	TransmissionPossible bool
}

func (e *SendError) Error() string {
	if e == nil {
		return "Pushover send failed"
	}
	if e.TransmissionPossible {
		return "Pushover send may have been transmitted"
	}
	return "Pushover send could not be transmitted"
}
func (e *SendError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
func (e *SendError) Format(state fmt.State, verb rune) { _, _ = io.WriteString(state, e.Error()) }

func newSendError(cause error, phase TransmissionPhase, possible bool) *SendError {
	return &SendError{Cause: cause, Phase: phase, TransmissionPossible: possible}
}

type traceState struct {
	wroteRequest, gotConn                    bool
	dnsStart, dnsDone, dnsFailed             bool
	connectStart, connectDone, connectFailed bool
	tlsStart, tlsDone, tlsFailed             bool
}

func (s *traceState) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSStart:          func(httptrace.DNSStartInfo) { s.dnsStart = true },
		DNSDone:           func(info httptrace.DNSDoneInfo) { s.dnsDone = true; s.dnsFailed = info.Err != nil },
		ConnectStart:      func(_, _ string) { s.connectStart = true },
		ConnectDone:       func(_, _ string, err error) { s.connectDone = true; s.connectFailed = err != nil },
		GotConn:           func(httptrace.GotConnInfo) { s.gotConn = true },
		TLSHandshakeStart: func() { s.tlsStart = true },
		TLSHandshakeDone:  func(_ tls.ConnectionState, err error) { s.tlsDone = true; s.tlsFailed = err != nil },
		WroteHeaders:      func() { s.wroteRequest = true },
		WroteRequest:      func(httptrace.WroteRequestInfo) { s.wroteRequest = true },
	}
}

func classifySendError(cause error, state traceState) *SendError {
	// A request byte or any unknown state makes delivery ambiguous. Only known
	// pre-write dial/TLS failures are definitive.
	if state.wroteRequest {
		return newSendError(cause, PhaseWrite, true)
	}
	if state.dnsStart && state.dnsDone && state.dnsFailed && !state.gotConn && !state.tlsStart {
		return newSendError(cause, PhaseBeforeDial, false)
	}
	if state.tlsStart && (!state.tlsDone || state.tlsFailed) {
		return newSendError(cause, PhaseTLS, false)
	}
	if state.connectStart && state.connectDone && state.connectFailed && !state.gotConn && !state.tlsStart {
		return newSendError(cause, PhaseConnect, false)
	}
	return newSendError(cause, PhaseRead, true)
}

func responseError(statusCode, status int) error {
	if statusCode >= 200 && statusCode < 300 && status == 1 {
		return nil
	}
	switch {
	case statusCode == http.StatusTooManyRequests:
		return apperror.New("quota_exceeded", apperror.CategoryQuota, "Pushover quota exceeded")
	case statusCode == http.StatusRequestTimeout:
		return apperror.New("network_failure", apperror.CategoryNetwork, "Pushover service unavailable", apperror.WithRetryable(true))
	case statusCode >= 500:
		return apperror.New("network_failure", apperror.CategoryNetwork, "Pushover service unavailable", apperror.WithRetryable(true))
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return apperror.New("authentication_failed", apperror.CategoryAuthentication, "Pushover authentication failed")
	case statusCode >= 400 || status == 0:
		return apperror.New("validation_failed", apperror.CategoryValidation, "Pushover rejected the request")
	default:
		return apperror.New("network_failure", apperror.CategoryNetwork, "Pushover returned an invalid response")
	}
}

func safeRequestID(value string) string {
	if safeOpaqueID(value, 1, 128) {
		return value
	}
	return ""
}
func safeReceiptID(value string) string {
	if len(value) != 30 {
		return ""
	}
	for _, c := range value {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return ""
		}
	}
	return value
}
func safeOpaqueID(value string, min, max int) bool {
	if len(value) < min || len(value) > max {
		return false
	}
	for _, c := range value {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func redactedTransportError(error) error {
	return apperror.New("network_failure", apperror.CategoryNetwork, "Pushover request failed", apperror.WithRetryable(true))
}
func isExplicitTransient(err error) bool {
	if err == nil {
		return false
	}
	var temporary interface{ Temporary() bool }
	return errors.As(err, &temporary) && temporary.Temporary()
}
func isTimeout(err error) bool {
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}
