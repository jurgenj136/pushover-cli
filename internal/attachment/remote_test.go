package attachment

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

func TestRemoteSnapshotsAndRejectsUnsafeURL(t *testing.T) {
	resolver := publicResolver("images.example")
	fixture := pngWithDimensions(t, 1, 1)
	loader := RemoteLoader{
		Resolver: resolver,
		Scope:    testScope(t),
		transportFactory: func(*pinnedDialer) http.RoundTripper {
			return roundTripFunc(func(*http.Request) (*http.Response, error) {
				return imageResponse(fixture, http.StatusOK, int64(len(fixture))), nil
			})
		},
	}
	source := mustURL(t, "https://images.example/private/path?token=do-not-show")
	attachment, err := loader.Remote(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	fixture[0] ^= 0xff
	if attachment.Bytes[0] == fixture[0] {
		t.Fatal("remote snapshot retained response buffer")
	}
	for _, value := range []string{
		"http://images.example/a",
		"https://name:password@images.example/a",
		"https://images.example:444/a",
		"https:opaque",
		"https://127.0.0.1/a",
		"https://[::ffff:8.8.8.8]/a",
		"https://[fe80::1%25zone]/a",
	} {
		t.Run(value, func(t *testing.T) {
			_, err := loader.Remote(context.Background(), mustURL(t, value))
			if err == nil {
				t.Fatal("unsafe URL accepted")
			}
			if strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "/a") {
				t.Fatalf("raw URL leaked: %v", err)
			}
		})
	}
}

func TestRemoteRawTLSUsesSNIAndPinnedNumericDial(t *testing.T) {
	fixture := pngWithDimensions(t, 1, 1)
	var serverName string
	server := newTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serverName = request.TLS.ServerName
		_, _ = writer.Write(fixture)
	}))
	defer server.Close()

	dialer := &forwardingDialer{address: server.Listener.Addr().String()}
	loader := RemoteLoader{
		Resolver: publicResolver("images.example"),
		Dialer:   dialer,
		Scope:    testScope(t),
		transportFactory: func(pinned *pinnedDialer) http.RoundTripper {
			transport := newTransport(pinned)
			// Test-only TLS harness: production never weakens certificate checks.
			transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
			return transport
		},
	}
	if _, err := loader.Remote(context.Background(), mustURL(t, "https://images.example/redacted")); err != nil {
		t.Fatal(err)
	}
	if serverName != "images.example" {
		t.Fatalf("TLS SNI = %q, want hostname", serverName)
	}
	if len(dialer.requested) != 1 || dialer.requested[0] != "8.8.8.8:443" {
		t.Fatalf("transport did not use numeric pinned dial: %v", dialer.requested)
	}
}

func TestRemoteDNSPolicyAndNumericPinnedDial(t *testing.T) {
	resolver := &fakeResolver{addresses: map[string][]netip.Addr{
		"mixed.example": {netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")},
		"empty.example": nil,
		"good.example":  {netip.MustParseAddr("8.8.8.8")},
	}}
	loader := RemoteLoader{Resolver: resolver, Scope: testScope(t)}
	for _, source := range []string{"https://mixed.example/hidden", "https://empty.example/hidden"} {
		_, err := loader.Remote(context.Background(), mustURL(t, source))
		if err == nil || strings.Contains(err.Error(), "/hidden") {
			t.Fatalf("DNS policy error unsafe: %v", err)
		}
	}

	dialer := &recordingDialer{}
	pinned := newPinnedDialer(resolver, dialer)
	ctx := context.Background()
	if _, err := pinned.prepare(ctx, mustURL(t, "https://good.example/path")); err != nil {
		t.Fatal(err)
	}
	connection, err := pinned.DialContext(ctx, "tcp", "good.example:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if got := dialer.addrs; len(got) != 1 || got[0] != "8.8.8.8:443" {
		t.Fatalf("dial targets = %v, want numeric vetted address", got)
	}
	resolver.addresses["good.example"] = []netip.Addr{netip.MustParseAddr("1.1.1.1")}
	if _, err := pinned.prepare(ctx, mustURL(t, "https://good.example/redirect")); err != nil {
		t.Fatal(err)
	}
	connection, err = pinned.DialContext(ctx, "tcp", "good.example:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if got := dialer.addrs[1]; got != "1.1.1.1:443" {
		t.Fatalf("rebound dial target = %q", got)
	}
}

func TestRemoteRedirectCapRevalidationAndClosedBodies(t *testing.T) {
	resolver := publicResolver("start.example", "next.example")
	first := &trackedBody{Reader: strings.NewReader("redirect")}
	var requests []*http.Request
	loader := RemoteLoader{
		Resolver:     resolver,
		Scope:        testScope(t),
		MaxRedirects: 1,
		transportFactory: func(*pinnedDialer) http.RoundTripper {
			return roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests = append(requests, request.Clone(request.Context()))
				switch len(requests) {
				case 1:
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://next.example/first-secret"}}, Body: first, Request: request}, nil
				default:
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://start.example/second-secret"}}, Body: io.NopCloser(strings.NewReader("redirect")), Request: request}, nil
				}
			})
		},
	}
	_, err := loader.Remote(context.Background(), mustURL(t, "https://start.example/original-secret"))
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe redirect error: %v", err)
	}
	if !first.closed {
		t.Fatal("redirect response body was not closed")
	}
	if len(resolver.calls) != 2 { // initial + first fully revalidated redirect
		t.Fatalf("DNS calls = %v", resolver.calls)
	}
	if len(requests) >= 2 && (requests[1].Referer() != "" || len(requests[1].Header) != 0) {
		t.Fatalf("redirect retained headers: referer=%q headers=%v", requests[1].Referer(), requests[1].Header)
	}
}

func TestRemoteRejectsUnsafeRedirectTargets(t *testing.T) {
	for _, location := range []string{
		"http://next.example/path-secret",
		"https://name:password@next.example/path-secret",
		"https://next.example:444/path-secret",
		"https://private.example/path-secret",
	} {
		t.Run(location, func(t *testing.T) {
			resolver := publicResolver("start.example", "next.example", "private.example")
			resolver.addresses["private.example"] = []netip.Addr{netip.MustParseAddr("10.0.0.1")}
			loader := RemoteLoader{
				Resolver: resolver, Scope: testScope(t),
				transportFactory: func(*pinnedDialer) http.RoundTripper {
					return roundTripFunc(func(request *http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{location}}, Body: io.NopCloser(strings.NewReader("redirect")), Request: request}, nil
					})
				},
			}
			_, err := loader.Remote(context.Background(), mustURL(t, "https://start.example/original-secret"))
			if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "password") {
				t.Fatalf("unsafe redirect error: %v", err)
			}
		})
	}
}

func TestRemoteBoundsAndCancellation(t *testing.T) {
	resolver := publicResolver("images.example")
	base := RemoteLoader{Resolver: resolver, Scope: testScope(t)}
	prefix := pngWithDimensions(t, 1, 1)
	exact := append(append([]byte(nil), prefix...), make([]byte, domain.MaxAttachmentBytes-len(prefix))...)
	base.transportFactory = func(*pinnedDialer) http.RoundTripper {
		return roundTripFunc(func(*http.Request) (*http.Response, error) {
			return imageResponse(exact, http.StatusOK, int64(len(exact))), nil
		})
	}
	if _, err := base.Remote(context.Background(), mustURL(t, "https://images.example/exact")); err != nil {
		t.Fatalf("exact remote boundary rejected: %v", err)
	}
	large := append(append([]byte(nil), exact...), 0)
	base.transportFactory = func(*pinnedDialer) http.RoundTripper {
		return roundTripFunc(func(*http.Request) (*http.Response, error) {
			// The claimed length is deliberately tiny; the streaming sentinel wins.
			return imageResponse(large, http.StatusOK, 1), nil
		})
	}
	if _, err := base.Remote(context.Background(), mustURL(t, "https://images.example/large")); err == nil {
		t.Fatal("oversized lying stream accepted")
	}
	base.transportFactory = func(*pinnedDialer) http.RoundTripper {
		return roundTripFunc(func(*http.Request) (*http.Response, error) {
			return imageResponse([]byte("not image"), http.StatusOK, 9), nil
		})
	}
	if _, err := base.Remote(context.Background(), mustURL(t, "https://images.example/corrupt")); err == nil {
		t.Fatal("corrupt remote image accepted")
	}
	closed := &trackedBody{Reader: bytes.NewReader(prefix)}
	base.transportFactory = func(*pinnedDialer) http.RoundTripper {
		return roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: closed, ContentLength: int64(len(prefix))}, nil
		})
	}
	if _, err := base.Remote(context.Background(), mustURL(t, "https://images.example/non-2xx")); err == nil || !closed.closed {
		t.Fatalf("non-2xx was accepted or body leaked: err=%v closed=%t", err, closed.closed)
	}
	closed = &trackedBody{Reader: bytes.NewReader(prefix)}
	base.transportFactory = func(*pinnedDialer) http.RoundTripper {
		return roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: closed, ContentLength: domain.MaxAttachmentBytes + 1}, nil
		})
	}
	if _, err := base.Remote(context.Background(), mustURL(t, "https://images.example/advertised-large")); err == nil || !closed.closed {
		t.Fatalf("oversized advertised response accepted or body leaked: err=%v closed=%t", err, closed.closed)
	}

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	base.transportFactory = func(*pinnedDialer) http.RoundTripper {
		return roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: &waitForCancel{ctx: request.Context(), started: started}, Request: request}, nil
		})
	}
	done := make(chan error, 1)
	go func() { _, err := base.Remote(ctx, mustURL(t, "https://images.example/slow")); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("mid-fetch cancellation succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("caller cancellation did not cancel fetch")
	}
}

func TestRemotePhaseAndTransportHeaderBound(t *testing.T) {
	resolver := &deadlineResolver{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}}
	var requestDeadline time.Time
	loader := RemoteLoader{
		Resolver: resolver, Scope: testScope(t),
		transportFactory: func(*pinnedDialer) http.RoundTripper {
			return roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requestDeadline, _ = request.Context().Deadline()
				return imageResponse(pngWithDimensions(t, 1, 1), http.StatusOK, 0), nil
			})
		},
	}
	before := time.Now()
	if _, err := loader.Remote(context.Background(), mustURL(t, "https://images.example/phase")); err != nil {
		t.Fatal(err)
	}
	if delta := resolver.deadline.Sub(before); delta < 4*time.Second || delta > 6*time.Second {
		t.Fatalf("DNS deadline budget = %s, want 5s", delta)
	}
	if delta := requestDeadline.Sub(before); delta < 19*time.Second || delta > 21*time.Second {
		t.Fatalf("remote phase deadline budget = %s, want 20s", delta)
	}

	// This raw TLS test transport is configured only after policy approval; it
	// verifies Transport's header-size enforcement without changing production.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable for raw TLS timeout harness: %v", err)
	}
	defer func() { _ = listener.Close() }()
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = connection.Close() }()
		_, _ = io.ReadAll(connection)
	}()
	pinned := newPinnedDialer(publicResolver("images.example"), &forwardingDialer{address: listener.Addr().String()})
	if _, err := pinned.prepare(context.Background(), mustURL(t, "https://images.example/a")); err != nil {
		t.Fatal(err)
	}
	transport := newTransport(pinned)
	transport.TLSHandshakeTimeout = 20 * time.Millisecond
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	client := &http.Client{Transport: transport}
	response, err := client.Get("https://images.example/a")
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("stalled TLS handshake bypassed timeout")
	}

	headerServer := newTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("X-Too-Large", strings.Repeat("x", 33<<10))
		_, _ = writer.Write(pngWithDimensions(t, 1, 1))
	}))
	defer headerServer.Close()
	pinned = newPinnedDialer(publicResolver("images.example"), &forwardingDialer{address: headerServer.Listener.Addr().String()})
	if _, err := pinned.prepare(context.Background(), mustURL(t, "https://images.example/a")); err != nil {
		t.Fatal(err)
	}
	transport = newTransport(pinned)
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	client = &http.Client{Transport: transport}
	response, err = client.Get("https://images.example/a")
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("response headers above 32KiB were accepted")
	}

	slowHeaderServer := newTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = writer.Write(pngWithDimensions(t, 1, 1))
	}))
	defer slowHeaderServer.Close()
	pinned = newPinnedDialer(publicResolver("images.example"), &forwardingDialer{address: slowHeaderServer.Listener.Addr().String()})
	if _, err := pinned.prepare(context.Background(), mustURL(t, "https://images.example/a")); err != nil {
		t.Fatal(err)
	}
	transport = newTransport(pinned)
	transport.ResponseHeaderTimeout = 20 * time.Millisecond
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	client = &http.Client{Transport: transport}
	response, err = client.Get("https://images.example/a")
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("stalled response headers bypassed timeout")
	}
}

func TestRemoteRejectsZeroScopeAndTransportDisablesProxy(t *testing.T) {
	loader := RemoteLoader{Resolver: publicResolver("images.example")}
	if _, err := loader.Remote(context.Background(), mustURL(t, "https://images.example/a")); err == nil {
		t.Fatal("zero operation scope accepted")
	}
	transport := newTransport(newPinnedDialer(publicResolver("images.example"), &recordingDialer{}))
	if transport.Proxy != nil || !transport.DisableCompression || !transport.DisableKeepAlives || transport.ForceAttemptHTTP2 || transport.TLSHandshakeTimeout != 5*time.Second || transport.ResponseHeaderTimeout != 10*time.Second || transport.MaxResponseHeaderBytes != 32<<10 {
		t.Fatalf("unsafe transport: %#v", transport)
	}
}

func TestRemoteRejectsNegativeRedirectLimitBeforeDNS(t *testing.T) {
	resolver := publicResolver("images.example")
	loader := RemoteLoader{Resolver: resolver, Scope: testScope(t), MaxRedirects: -1}
	if _, err := loader.Remote(context.Background(), mustURL(t, "https://images.example/path")); err == nil {
		t.Fatal("negative redirect limit accepted")
	}
	if len(resolver.calls) != 0 {
		t.Fatalf("invalid redirect limit performed DNS: %v", resolver.calls)
	}
}

func imageResponse(data []byte, status int, length int64) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data)), ContentLength: length}
}

func mustURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func newTLSServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable for raw TLS harness: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.StartTLS()
	return server
}

type waitForCancel struct {
	ctx     context.Context
	started chan<- struct{}
	once    bool
}

func (r *waitForCancel) Read([]byte) (int, error) {
	if !r.once {
		r.once = true
		close(r.started)
	}
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}
func (*waitForCancel) Close() error { return nil }
