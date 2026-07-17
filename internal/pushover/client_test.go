package pushover

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

const (
	testToken = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testUser  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func timePtr(unix int64) *time.Time {
	value := time.Unix(unix, 0).UTC()
	return &value
}

func TestSendEncodesCompleteForm(t *testing.T) {
	var got url.Values
	server := newTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1/messages.json" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		got = r.PostForm
		w.Header().Set("X-Limit-App-Remaining", "99")
		_, _ = w.Write([]byte(`{"status":1,"request":"request-123"}`))
	}))
	defer server.Close()

	client, err := NewTestClient(server.URL, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	timestamp := int64(1_700_000_000)
	response, err := client.Send(context.Background(), domain.TargetPayload{
		Target:  domain.Target{Credentials: domain.Credentials{Token: testToken, UserKey: testUser}},
		Message: domain.Message{Body: "body", Title: "title", URL: "https://example.com", URLTitle: "link", Devices: []string{"phone", "watch"}, Priority: 0, Sound: "pushover", Timestamp: timePtr(timestamp), TTL: 60 * time.Second, HTML: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.RequestID != "request-123" || response.QuotaRemaining == nil || *response.QuotaRemaining != 99 {
		t.Fatalf("response = %#v", response)
	}
	for key, want := range map[string]string{"token": testToken, "user": testUser, "message": "body", "title": "title", "url": "https://example.com", "url_title": "link", "device": "phone,watch", "priority": "0", "sound": "pushover", "timestamp": "1700000000", "ttl": "60", "html": "1"} {
		if got.Get(key) != want {
			t.Errorf("%s = %q, want %q", key, got.Get(key), want)
		}
	}
}

func TestSendUsesGenericMultipartAttachmentName(t *testing.T) {
	var contentType string
	var body []byte
	transport := &scriptedRoundTripper{fn: func(request *http.Request, _ int) (*http.Response, error) {
		contentType = request.Header.Get("Content-Type")
		body, _ = io.ReadAll(request.Body)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":1,"request":"request-1"}`)), Request: request}, nil
	}}
	client, err := NewTestClient("https://test.invalid", &http.Client{Transport: transport}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Send(context.Background(), domain.TargetPayload{Target: domain.Target{Credentials: domain.Credentials{Token: testToken, UserKey: testUser}}, Message: domain.Message{Body: "body", Attachment: &domain.Attachment{Name: "private-image.png", MediaType: "image/png", Bytes: []byte("fixture")}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(contentType, "multipart/form-data;") {
		t.Fatalf("content type = %q", contentType)
	}
	if bytes.Contains(body, []byte("private-image")) {
		t.Fatalf("attachment filename was not generic: %q", body)
	}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	foundAttachment := false
	for {
		part, partErr := reader.NextPart()
		if partErr == io.EOF {
			break
		}
		if partErr != nil {
			t.Fatal(partErr)
		}
		if part.FormName() != "attachment" {
			continue
		}
		foundAttachment = true
		if part.FileName() != "attachment" || part.Header.Get("Content-Type") != "image/png" {
			t.Fatalf("attachment headers = %v", part.Header)
		}
		break
	}
	if !foundAttachment {
		t.Fatal("multipart body omitted attachment")
	}
}

func TestSendEncryptedWireIncludesOnlyExplicitEncryptedFields(t *testing.T) {
	var got url.Values
	transport := &scriptedRoundTripper{fn: func(request *http.Request, _ int) (*http.Response, error) {
		encoded, _ := io.ReadAll(request.Body)
		got, _ = url.ParseQuery(string(encoded))
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":1,"request":"request-1"}`)), Request: request}, nil
	}}
	client, err := NewTestClient("https://test.invalid", &http.Client{Transport: transport}, nil)
	if err != nil {
		t.Fatal(err)
	}
	envelope := validEnvelope()
	payload := domain.TargetPayload{Target: domain.Target{Credentials: domain.Credentials{Token: testToken, UserKey: testUser}}, Message: domain.Message{Body: envelope, Title: envelope, URL: envelope, URLTitle: envelope, Encrypted: true}}
	if _, err := client.Send(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"message": envelope, "title": envelope, "url": envelope, "url_title": envelope, "encrypted": "1", "priority": "0"} {
		if got.Get(key) != want {
			t.Errorf("%s = %q, want %q", key, got.Get(key), want)
		}
	}
	if got.Get("html") != "" || got.Get("monospace") != "" || got.Get("attachment") != "" {
		t.Fatalf("unexpected encrypted wire fields: %v", got)
	}
}

func TestSendNeverRetriesAndKeepsFailuresTyped(t *testing.T) {
	transport := &scriptedRoundTripper{fn: func(request *http.Request, _ int) (*http.Response, error) {
		return jsonResponse(request, http.StatusServiceUnavailable, "hostile failure with "+testToken), nil
	}}
	client, err := NewTestClient("https://test.invalid", &http.Client{Transport: transport}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Send(context.Background(), domain.TargetPayload{Target: domain.Target{Credentials: domain.Credentials{Token: testToken, UserKey: testUser}}, Message: domain.Message{Body: "body"}})
	var send *SendError
	if !errors.As(err, &send) || !send.TransmissionPossible || transport.calls != 1 {
		t.Fatalf("send=%#v calls=%d", err, transport.calls)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("upstream secret leaked: %v", err)
	}
}

func TestSendLocalValidationFailureIsDefinitivelyUnsent(t *testing.T) {
	transport := &scriptedRoundTripper{fn: func(*http.Request, int) (*http.Response, error) {
		t.Fatal("transport called for invalid payload")
		return nil, nil
	}}
	client, err := NewTestClient("https://test.invalid", &http.Client{Transport: transport}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Send(context.Background(), domain.TargetPayload{
		Target: domain.Target{Credentials: domain.Credentials{Token: testToken, UserKey: testUser}},
	})
	var send *SendError
	if !errors.As(err, &send) || send.TransmissionPossible || send.Phase != PhaseBeforeDial || transport.calls != 0 {
		t.Fatalf("send error = %#v calls = %d", err, transport.calls)
	}
}

func TestSendRequestConstructionFailureIsDefinitivelyUnsent(t *testing.T) {
	transport := &scriptedRoundTripper{fn: func(*http.Request, int) (*http.Response, error) {
		t.Fatal("transport called when request construction failed")
		return nil, nil
	}}
	client, err := NewTestClient("https://test.invalid", &http.Client{Transport: transport}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Send(nil, //nolint:staticcheck // deliberate nil context tests request-construction failure
		domain.TargetPayload{
			Target:  domain.Target{Credentials: domain.Credentials{Token: testToken, UserKey: testUser}},
			Message: domain.Message{Body: "body"},
		})
	var send *SendError
	if !errors.As(err, &send) || send.TransmissionPossible || send.Phase != PhaseBeforeDial || transport.calls != 0 {
		t.Fatalf("send error = %#v calls = %d", err, transport.calls)
	}
}

func TestSendClassifiesAPIErrorsAndMalformedBodiesSafely(t *testing.T) {
	for _, test := range []struct {
		name           string
		status         int
		body, category string
		phase          TransmissionPhase
	}{
		{"authentication", http.StatusUnauthorized, `{"status":0}`, "authentication", PhaseRead},
		{"validation", http.StatusBadRequest, `{"status":0}`, "validation", PhaseRead},
		{"quota", http.StatusTooManyRequests, `{"status":0}`, "quota", PhaseRead},
		{"service", http.StatusServiceUnavailable, `{"status":0}`, "network", PhaseRead},
		{"malformed", http.StatusOK, `not json ` + testToken, "network", PhaseDecode},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, _ := NewTestClient("https://test.invalid", &http.Client{Transport: &scriptedRoundTripper{fn: func(request *http.Request, _ int) (*http.Response, error) {
				return jsonResponse(request, test.status, test.body), nil
			}}}, nil)
			_, err := client.Send(context.Background(), domain.TargetPayload{Target: domain.Target{Credentials: domain.Credentials{Token: testToken, UserKey: testUser}}, Message: domain.Message{Body: "body"}})
			var send *SendError
			if !errors.As(err, &send) || !send.TransmissionPossible || send.Phase != test.phase {
				t.Fatalf("send error = %#v", err)
			}
			if detail := apperror.SafeDetail(send.Cause); detail.Category != test.category {
				t.Fatalf("detail = %#v", detail)
			}
			if strings.Contains(err.Error(), testToken) {
				t.Fatalf("hostile body leaked: %v", err)
			}
		})
	}
}

func TestSendRejectsOversizedResponseAsAmbiguousDecode(t *testing.T) {
	body := `{"status":1,"request":"request-1","padding":"` + strings.Repeat("x", maxResponseBytes) + `"}`
	client, _ := NewTestClient("https://test.invalid", &http.Client{Transport: &scriptedRoundTripper{fn: func(request *http.Request, _ int) (*http.Response, error) {
		return jsonResponse(request, http.StatusOK, body), nil
	}}}, nil)
	_, err := client.Send(context.Background(), domain.TargetPayload{Target: domain.Target{Credentials: domain.Credentials{Token: testToken, UserKey: testUser}}, Message: domain.Message{Body: "body"}})
	var send *SendError
	if !errors.As(err, &send) || !send.TransmissionPossible || send.Phase != PhaseDecode {
		t.Fatalf("send error = %#v", err)
	}
}

func TestSendRejectsMalformedEncryptedPayloadBeforeNetwork(t *testing.T) {
	for _, test := range []struct {
		name    string
		message domain.Message
		kind    domain.TargetKind
	}{
		{"malformed envelope", domain.Message{Body: "YQ==", Encrypted: true}, domain.KindUser},
		{"url title without URL", domain.Message{Body: validEnvelope(), URLTitle: validEnvelope(), Encrypted: true}, domain.KindUser},
		{"invalid emergency", domain.Message{Body: validEnvelope(), Encrypted: true, Priority: domain.PriorityEmergency}, domain.KindUser},
		{"group device", domain.Message{Body: validEnvelope(), Encrypted: true, Devices: []string{"phone"}}, domain.KindGroup},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := &scriptedRoundTripper{fn: func(*http.Request, int) (*http.Response, error) {
				t.Fatal("network call for invalid encrypted payload")
				return nil, nil
			}}
			client, _ := NewTestClient("https://test.invalid", &http.Client{Transport: transport}, nil)
			_, err := client.Send(context.Background(), domain.TargetPayload{Target: domain.Target{Kind: test.kind, Credentials: domain.Credentials{Token: testToken, UserKey: testUser}}, Message: test.message})
			var send *SendError
			if !errors.As(err, &send) || send.TransmissionPossible || send.Phase != PhaseBeforeDial || transport.calls != 0 {
				t.Fatalf("send error = %#v calls=%d", err, transport.calls)
			}
		})
	}
}

func validEnvelope() string { return base64.StdEncoding.EncodeToString(make([]byte, 64)) }

func newTLSServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable for TLS harness: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.StartTLS()
	return server
}
