package pushover

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type scriptedRoundTripper struct {
	calls int
	fn    func(*http.Request, int) (*http.Response, error)
}

func (s *scriptedRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	s.calls++
	return s.fn(request, s.calls)
}

type countingBackoff struct{ calls int }

func (b *countingBackoff) Wait(context.Context, int) error { b.calls++; return nil }

func TestSoundsUsesOfficialGETAndRetriesAServiceFailure(t *testing.T) {
	transport := &scriptedRoundTripper{fn: func(request *http.Request, call int) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Path != "/1/sounds.json" || request.URL.Query().Get("token") != testToken {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL)
		}
		body := `{"status":1,"request":"request-1","sounds":{"zeta":"Zeta","alpha":"Alpha"}}`
		status := http.StatusOK
		if call == 1 {
			status = http.StatusServiceUnavailable
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: request}, nil
	}}
	backoff := &countingBackoff{}
	client, err := NewTestClient("https://test.invalid", &http.Client{Transport: transport}, backoff)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Sounds(context.Background(), testToken)
	if err != nil {
		t.Fatal(err)
	}
	if transport.calls != 2 || backoff.calls != 1 {
		t.Fatalf("calls=%d backoff=%d", transport.calls, backoff.calls)
	}
	if got := response.Sounds; len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "zeta" {
		t.Fatalf("sounds = %#v", got)
	}
}

func TestReadDoesNotStartBackoffWhenOperationCannotAccommodateIt(t *testing.T) {
	transport := &scriptedRoundTripper{fn: func(request *http.Request, _ int) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(`{"status":1,"request":"request-1"}`)), Header: make(http.Header), Request: request}, nil
	}}
	backoff := &countingBackoff{}
	client, err := NewTestClient("https://test.invalid", &http.Client{Transport: transport}, backoff)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = client.Sounds(ctx, testToken)
	if err == nil {
		t.Fatal("service failure accepted")
	}
	if backoff.calls != 0 {
		t.Fatalf("backoff calls = %d, want 0 when 5s phase budget cannot fit", backoff.calls)
	}
}

func TestReadTransportErrorIsRedacted(t *testing.T) {
	secret := "https://api.pushover.net/1/sounds.json?token=" + testToken
	client, err := NewTestClient("https://test.invalid", &http.Client{Transport: &scriptedRoundTripper{fn: func(*http.Request, int) (*http.Response, error) { return nil, errors.New(secret) }}}, &countingBackoff{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Sounds(context.Background(), testToken)
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestReadEndpointsReturnBoundedTypedData(t *testing.T) {
	transport := &scriptedRoundTripper{fn: func(request *http.Request, _ int) (*http.Response, error) {
		switch request.URL.Path {
		case "/1/users/validate.json":
			if request.Method != http.MethodPost {
				t.Fatalf("validation method = %s", request.Method)
			}
			encoded, _ := io.ReadAll(request.Body)
			if !strings.Contains(string(encoded), "device=phone%2Cwatch") {
				t.Fatalf("validation devices = %q", encoded)
			}
			return jsonResponse(request, http.StatusOK, `{"status":1,"request":"request-1","devices":["phone"],"licenses":["trial"]}`), nil
		case "/1/apps/limits.json":
			return jsonResponse(request, http.StatusOK, `{"status":1,"request":"request-1","limit":10000,"remaining":9999,"reset":1700000000}`), nil
		default:
			t.Fatalf("unexpected endpoint %q", request.URL.Path)
			return nil, nil
		}
	}}
	client, err := NewTestClient("https://test.invalid", &http.Client{Transport: transport}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := client.ValidateRecipient(context.Background(), testToken, testUser, []string{"phone", "watch"})
	if err != nil || !validation.Valid || len(validation.Devices) != 1 || len(validation.Licenses) != 1 {
		t.Fatalf("validation = %#v, %v", validation, err)
	}
	limits, err := client.Limits(context.Background(), testToken)
	if err != nil || limits.Limit != 10000 || limits.Remaining != 9999 || limits.Reset != 1700000000 {
		t.Fatalf("limits = %#v, %v", limits, err)
	}
}

func TestReadRetriesEmptyServiceFailureBeforeDecoding(t *testing.T) {
	transport := &scriptedRoundTripper{fn: func(request *http.Request, call int) (*http.Response, error) {
		if call == 1 {
			return jsonResponse(request, http.StatusServiceUnavailable, "hostile upstream body"), nil
		}
		return jsonResponse(request, http.StatusOK, `{"status":1,"request":"request-1","sounds":{}}`), nil
	}}
	backoff := &countingBackoff{}
	client, _ := NewTestClient("https://test.invalid", &http.Client{Transport: transport}, backoff)
	if _, err := client.Sounds(context.Background(), testToken); err != nil {
		t.Fatal(err)
	}
	if transport.calls != 2 || backoff.calls != 1 {
		t.Fatalf("calls=%d backoff=%d", transport.calls, backoff.calls)
	}
}

func jsonResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: request}
}
