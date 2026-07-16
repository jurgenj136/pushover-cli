package pushover

import (
	"errors"
	"net"
	"net/http"
	"testing"
)

func TestTestClientRequiresHTTPS(t *testing.T) {
	if _, err := NewTestClient("http://test.invalid", &http.Client{}, nil); err == nil {
		t.Fatal("cleartext test endpoint accepted")
	}
}

func TestSendErrorUsesOnlyTraceProvenPrewriteFailures(t *testing.T) {
	unknownDNS := classifySendError(&net.DNSError{Err: "private.example", Name: "private.example"}, traceState{})
	if !unknownDNS.TransmissionPossible || unknownDNS.Phase != PhaseRead {
		t.Fatalf("unknown DNS = %#v", unknownDNS)
	}
	definiteDNS := classifySendError(errors.New("ignored"), traceState{dnsStart: true, dnsDone: true, dnsFailed: true})
	if definiteDNS.TransmissionPossible || definiteDNS.Phase != PhaseBeforeDial {
		t.Fatalf("traced DNS = %#v", definiteDNS)
	}
	definiteConnect := classifySendError(errors.New("ignored"), traceState{connectStart: true, connectDone: true, connectFailed: true})
	if definiteConnect.TransmissionPossible || definiteConnect.Phase != PhaseConnect {
		t.Fatalf("traced connect = %#v", definiteConnect)
	}
	definiteTLS := classifySendError(errors.New("ignored"), traceState{tlsStart: true, tlsDone: true, tlsFailed: true})
	if definiteTLS.TransmissionPossible || definiteTLS.Phase != PhaseTLS {
		t.Fatalf("traced TLS = %#v", definiteTLS)
	}
	if result := classifySendError(errors.New("ignored"), traceState{tlsStart: true, tlsDone: true}); !result.TransmissionPossible || result.Phase != PhaseRead {
		t.Fatalf("post-TLS error = %#v", result)
	}
	if result := classifySendError(errors.New("ignored"), traceState{wroteRequest: true, connectStart: true, connectDone: true, connectFailed: true}); !result.TransmissionPossible || result.Phase != PhaseWrite {
		t.Fatalf("write precedence = %#v", result)
	}
	if result := newSendError(errors.New("ignored"), PhaseDecode, true); !result.TransmissionPossible || result.Phase != PhaseDecode {
		t.Fatalf("decode classification = %#v", result)
	}
}

func TestProductionTransportDisablesProxyAndRedirects(t *testing.T) {
	client := NewClient(nil)
	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || client.httpClient.CheckRedirect == nil {
		t.Fatalf("unsafe production transport: %#v", transport)
	}
	if err := client.httpClient.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy = %v", err)
	}
}
