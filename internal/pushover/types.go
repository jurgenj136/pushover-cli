// Package pushover implements the bounded HTTP boundary for the Pushover API.
package pushover

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

const (
	productionBaseURL = "https://api.pushover.net"
	maxResponseBytes  = 1 << 20
)

type SendResponse struct {
	RequestID      string
	ReceiptID      string
	QuotaRemaining *int
	QuotaLimit     *int64
	QuotaReset     *int64
}

type RecipientValidation struct {
	Valid     bool
	Devices   []string
	Licenses  []string
	RequestID string
}

type Sound struct{ Name, Description string }
type SoundsResponse struct {
	Sounds    []Sound
	RequestID string
}
type LimitsResponse struct {
	Limit, Remaining int
	Reset            int64
	RequestID        string
}

type TransmissionPhase string

const (
	PhaseBeforeDial TransmissionPhase = "before_dial"
	PhaseConnect    TransmissionPhase = "connect"
	PhaseTLS        TransmissionPhase = "tls"
	PhaseWrite      TransmissionPhase = "write"
	PhaseRead       TransmissionPhase = "read"
	PhaseDecode     TransmissionPhase = "decode"
)

type Client interface {
	Send(context.Context, domain.TargetPayload) (SendResponse, error)
	ValidateRecipient(context.Context, string, string, []string) (RecipientValidation, error)
	Sounds(context.Context, string) (SoundsResponse, error)
	Limits(context.Context, string) (LimitsResponse, error)
}

type Backoff interface {
	Wait(context.Context, int) error
}

// HTTPClient is safe for sequential reuse. The production constructor fixes its
// endpoint and transport; NewTestClient exists solely for hermetic TLS tests.
type HTTPClient struct {
	httpClient *http.Client
	baseURL    string
	backoff    Backoff
}

// NewClient returns a production client with normal TLS verification and no
// proxy or redirect support.
func NewClient(backoff Backoff) *HTTPClient {
	return &HTTPClient{httpClient: productionHTTPClient(), baseURL: productionBaseURL, backoff: normalizeBackoff(backoff)}
}

// New is a compact alias for NewClient.
func New(backoff Backoff) *HTTPClient { return NewClient(backoff) }

// NewTestClient permits a hermetic HTTPS endpoint and client. It deliberately
// rejects cleartext endpoints so tests cannot accidentally weaken TLS policy.
func NewTestClient(baseURL string, client *http.Client, backoff Backoff) (*HTTPClient, error) {
	if err := validateHTTPSBaseURL(baseURL); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fixedError("invalid_client")
	}
	return &HTTPClient{httpClient: client, baseURL: trimBaseURL(baseURL), backoff: normalizeBackoff(backoff)}, nil
}

func productionHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  10 * time.Second,
		MaxResponseHeaderBytes: 32 << 10,
		IdleConnTimeout:        90 * time.Second,
		MaxIdleConns:           4,
		MaxIdleConnsPerHost:    2,
		ForceAttemptHTTP2:      true,
	}
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
