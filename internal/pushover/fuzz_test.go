package pushover

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func FuzzAPIError(f *testing.F) {
	for _, seed := range []string{"{}", `{"status":1}`, `{"status":0,"errors":["hostile\\u001b[2J"]}`, strings.Repeat("x", maxResponseBytes+1)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		response := &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(value))}
		_, _ = decodeAPIResponse(response)
	})
}
