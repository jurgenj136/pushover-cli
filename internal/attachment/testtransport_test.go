package attachment

import (
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/jurgenj136/pushover-cli/internal/operation"
)

type fakeResolver struct {
	mu        sync.Mutex
	addresses map[string][]netip.Addr
	err       error
	calls     []string
}

type deadlineResolver struct {
	addresses []netip.Addr
	deadline  time.Time
}

func (r *deadlineResolver) LookupNetIP(ctx context.Context, _ string, _ string) ([]netip.Addr, error) {
	r.deadline, _ = ctx.Deadline()
	return append([]netip.Addr(nil), r.addresses...), nil
}

func (r *fakeResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, host)
	return append([]netip.Addr(nil), r.addresses[host]...), r.err
}

type recordingDialer struct {
	mu    sync.Mutex
	addrs []string
}

type forwardingDialer struct {
	address   string
	mu        sync.Mutex
	requested []string
	deadline  time.Time
}

func (d *forwardingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.mu.Lock()
	d.requested = append(d.requested, address)
	d.deadline, _ = ctx.Deadline()
	d.mu.Unlock()
	return (&net.Dialer{}).DialContext(ctx, network, d.address)
}

func (d *recordingDialer) DialContext(_ context.Context, _ string, address string) (net.Conn, error) {
	d.mu.Lock()
	d.addrs = append(d.addrs, address)
	d.mu.Unlock()
	left, right := net.Pipe()
	go right.Close()
	return left, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

func testScope(t *testing.T) operation.Scope {
	t.Helper()
	scope, err := (operation.Factory{}).Start(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scope.Close)
	return scope
}

func publicResolver(hosts ...string) *fakeResolver {
	addresses := make(map[string][]netip.Addr, len(hosts))
	for _, host := range hosts {
		addresses[host] = []netip.Addr{netip.MustParseAddr("8.8.8.8")}
	}
	return &fakeResolver{addresses: addresses}
}

func pngWithDimensions(t *testing.T, width, height uint32) []byte {
	t.Helper()
	buffer := new(bytesBuffer)
	image := image.NewRGBA(image.Rect(0, 0, 1, 1))
	image.Set(0, 0, color.White)
	if err := png.Encode(buffer, image); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()
	binary.BigEndian.PutUint32(data[16:20], width)
	binary.BigEndian.PutUint32(data[20:24], height)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data
}

// bytesBuffer avoids exposing mutable test fixture buffers to the production API.
type bytesBuffer struct{ data []byte }

func (b *bytesBuffer) Write(value []byte) (int, error) {
	b.data = append(b.data, value...)
	return len(value), nil
}
func (b *bytesBuffer) Bytes() []byte { return append([]byte(nil), b.data...) }
