package attachment

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/operation"
)

const (
	defaultMaxRedirects = 3
	connectTimeout      = 5 * time.Second
)

// Loader is the attachment boundary consumed by delivery preflight.
type Loader interface {
	Local(context.Context, string) (*domain.Attachment, error)
	Remote(context.Context, *url.URL) (*domain.Attachment, error)
}

// NameResolver and ContextDialer are deliberately narrow seams. The former
// makes every DNS result inspectable; the latter proves that production dials
// a vetted numeric target rather than re-resolving a hostname.
type NameResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type ContextDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

// RemoteLoader fetches a single immutable remote image snapshot. A zero Scope
// is rejected: attachment work must always live under the caller's operation
// deadline.
type RemoteLoader struct {
	Resolver     NameResolver
	Dialer       ContextDialer
	Scope        operation.Scope
	MaxRedirects int

	// transportFactory is a package-private test seam. Production always uses
	// newTransport; tests may inject a raw transport only after the same URL,
	// DNS, and numeric-dial policy has been approved.
	transportFactory func(*pinnedDialer) http.RoundTripper
}

func (r RemoteLoader) Local(ctx context.Context, path string) (*domain.Attachment, error) {
	return Local(ctx, path)
}

func (r RemoteLoader) Remote(ctx context.Context, source *url.URL) (*domain.Attachment, error) {
	if r.Scope.Context() == nil {
		return nil, invalidScope()
	}
	if source == nil {
		return nil, invalidRemoteURL()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	phaseCtx, cancel, err := r.Scope.Phase(operation.PhaseRemoteAttachment)
	if err != nil {
		return nil, err
	}
	defer cancel()
	stopCallerCancellation := context.AfterFunc(ctx, cancel)
	defer stopCallerCancellation()
	if err := ctx.Err(); err != nil {
		return nil, remoteFailure("", source.String(), err)
	}
	maxRedirects := r.MaxRedirects
	if maxRedirects == 0 {
		maxRedirects = defaultMaxRedirects
	}
	if maxRedirects < 0 {
		return nil, apperror.New("attachment_redirects", apperror.CategoryValidation, "attachment redirect limit must not be negative")
	}

	requestURL := *source // caller mutation cannot split preflight from request.
	rawSource := requestURL.String()
	pinned := newPinnedDialer(r.Resolver, r.Dialer)
	origin, err := pinned.prepare(phaseCtx, &requestURL)
	if err != nil {
		return nil, remoteFailure("", rawSource, err)
	}

	transport := http.RoundTripper(newTransport(pinned))
	if r.transportFactory != nil {
		transport = r.transportFactory(pinned)
	}
	client := &http.Client{
		Transport: transport,
		Jar:       nil,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return errors.New("redirect limit reached")
			}
			// Do not let net/http synthesize Referer or retain any previous headers.
			// #nosec G119 -- replacing the complete header map is the deliberate
			// defense: no credential or caller header can cross an origin redirect.
			request.Header = make(http.Header)
			request.Header.Del("Referer")
			_, err := pinned.prepare(phaseCtx, request.URL)
			return err
		},
	}

	request, err := http.NewRequestWithContext(phaseCtx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, remoteFailure(origin, rawSource, err)
	}
	response, err := client.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, remoteFailure(origin, rawSource, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, remoteFailure(origin, rawSource, fmt.Errorf("unexpected HTTP status %d", response.StatusCode))
	}
	if response.ContentLength > domain.MaxAttachmentBytes {
		return nil, attachmentTooLarge()
	}
	data, err := readBounded(phaseCtx, response.Body)
	if err != nil {
		if typed := attachmentError(err); typed != nil {
			return nil, typed
		}
		return nil, remoteFailure(origin, rawSource, err)
	}
	return snapshotImage(data)
}

func newTransport(dialer *pinnedDialer) *http.Transport {
	return &http.Transport{
		Proxy:                  nil,
		DialContext:            dialer.DialContext,
		DisableCompression:     true,
		DisableKeepAlives:      true,
		ForceAttemptHTTP2:      false,
		TLSHandshakeTimeout:    connectTimeout,
		ResponseHeaderTimeout:  10 * time.Second,
		MaxResponseHeaderBytes: 32 << 10,
		TLSNextProto:           map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
}

type pinnedDialer struct {
	resolver NameResolver
	dialer   ContextDialer

	mu     sync.RWMutex
	routes map[string][]netip.Addr
}

func newPinnedDialer(resolver NameResolver, dialer ContextDialer) *pinnedDialer {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if dialer == nil {
		dialer = &net.Dialer{Timeout: connectTimeout}
	}
	return &pinnedDialer{resolver: resolver, dialer: dialer, routes: make(map[string][]netip.Addr)}
}

func (d *pinnedDialer) prepare(parent context.Context, source *url.URL) (string, error) {
	host, origin, err := validatedRemoteURL(source)
	if err != nil {
		return "", err
	}
	addresses, err := d.resolve(parent, host)
	if err != nil {
		return "", err
	}
	d.mu.Lock()
	d.routes[strings.ToLower(host)] = append([]netip.Addr(nil), addresses...)
	d.mu.Unlock()
	return origin, nil
}

func (d *pinnedDialer) resolve(parent context.Context, host string) ([]netip.Addr, error) {
	if literal, err := netip.ParseAddr(host); err == nil {
		if !IsPublicAddress(literal) {
			return nil, errors.New("destination is not public")
		}
		return []netip.Addr{literal}, nil
	}
	ctx, cancel := context.WithTimeout(parent, connectTimeout)
	defer cancel()
	addresses, err := d.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		if err == nil {
			err = errors.New("empty DNS result")
		}
		return nil, err
	}
	for _, address := range addresses {
		if !IsPublicAddress(address) {
			return nil, errors.New("destination is not public")
		}
	}
	return append([]netip.Addr(nil), addresses...), nil
}

func (d *pinnedDialer) DialContext(parent context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "443" {
		return nil, errors.New("unapproved dial target")
	}
	d.mu.RLock()
	addresses := append([]netip.Addr(nil), d.routes[strings.ToLower(host)]...)
	d.mu.RUnlock()
	if len(addresses) == 0 {
		return nil, errors.New("unapproved dial target")
	}

	ctx, cancel := context.WithTimeout(parent, connectTimeout)
	defer cancel()
	var lastErr error
	for _, candidate := range addresses {
		connection, err := d.dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), "443"))
		if err == nil {
			return connection, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("unable to connect to approved destination")
	}
	return nil, lastErr
}

func validatedRemoteURL(source *url.URL) (host, origin string, err error) {
	if source == nil || !source.IsAbs() || source.Opaque != "" || !strings.EqualFold(source.Scheme, "https") || source.User != nil || source.Host == "" || strings.ContainsAny(source.Host, "\r\n") {
		return "", "", invalidRemoteURL()
	}
	host = source.Hostname()
	if host == "" || strings.ContainsAny(host, "\r\n%") {
		return "", "", invalidRemoteURL()
	}
	port := source.Port()
	if port != "" && port != "443" {
		return "", "", invalidRemoteURL()
	}
	// A bare trailing colon is neither an absent port nor HTTPS' default port.
	if port == "" && strings.HasSuffix(source.Host, ":") {
		return "", "", invalidRemoteURL()
	}
	canonicalHost := strings.ToLower(host)
	if strings.Contains(canonicalHost, ":") {
		canonicalHost = "[" + canonicalHost + "]"
	}
	if port == "443" {
		origin = "https://" + canonicalHost + ":443"
	} else {
		origin = "https://" + canonicalHost
	}
	return host, origin, nil
}

func remoteFailure(origin, rawURL string, cause error) error {
	message := "remote attachment fetch failed"
	if origin != "" {
		message += " for " + origin
	}
	return wrapRemote("attachment_remote", message, cause, rawURL)
}

func attachmentError(err error) *apperror.Error {
	var typed *apperror.Error
	if errors.As(err, &typed) {
		return typed
	}
	return nil
}
