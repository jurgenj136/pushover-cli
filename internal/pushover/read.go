package pushover

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

type apiResponse struct {
	status    int
	request   string
	receipt   string
	devices   []string
	licenses  []string
	sounds    map[string]string
	limit     int
	remaining int
	reset     int64
}

type rawAPIResponse struct {
	Status    int               `json:"status"`
	Request   string            `json:"request"`
	Receipt   string            `json:"receipt"`
	Devices   []string          `json:"devices"`
	Licenses  []string          `json:"licenses"`
	Sounds    map[string]string `json:"sounds"`
	Limit     int               `json:"limit"`
	Remaining int               `json:"remaining"`
	Reset     int64             `json:"reset"`
}

func decodeAPIResponse(response *http.Response) (apiResponse, error) {
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return apiResponse{}, fixedError("network_failure")
	}
	if len(body) > maxResponseBytes {
		return apiResponse{}, fixedError("invalid_response")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	var raw rawAPIResponse
	if err := decoder.Decode(&raw); err != nil {
		return apiResponse{}, fixedError("invalid_response")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return apiResponse{}, fixedError("invalid_response")
	}
	if raw.Status != 0 && raw.Status != 1 {
		return apiResponse{}, fixedError("invalid_response")
	}
	if raw.Request != "" && !safeOpaqueID(raw.Request, 1, 128) {
		return apiResponse{}, fixedError("invalid_response")
	}
	if raw.Receipt != "" && safeReceiptID(raw.Receipt) == "" {
		return apiResponse{}, fixedError("invalid_response")
	}
	if err := validateStrings(raw.Devices, 100, 100); err != nil {
		return apiResponse{}, err
	}
	if err := validateStrings(raw.Licenses, 100, 100); err != nil {
		return apiResponse{}, err
	}
	if len(raw.Sounds) > 1000 {
		return apiResponse{}, fixedError("invalid_response")
	}
	for name, description := range raw.Sounds {
		if !safeText(name, 100) || !safeText(description, 250) {
			return apiResponse{}, fixedError("invalid_response")
		}
	}
	if raw.Limit < 0 || raw.Remaining < 0 || raw.Reset < 0 {
		return apiResponse{}, fixedError("invalid_response")
	}
	return apiResponse{status: raw.Status, request: raw.Request, receipt: raw.Receipt, devices: append([]string(nil), raw.Devices...), licenses: append([]string(nil), raw.Licenses...), sounds: raw.Sounds, limit: raw.Limit, remaining: raw.Remaining, reset: raw.Reset}, nil
}

func validateStrings(values []string, maxCount, maxLen int) error {
	if len(values) > maxCount {
		return fixedError("invalid_response")
	}
	for _, value := range values {
		if !safeText(value, maxLen) {
			return fixedError("invalid_response")
		}
	}
	return nil
}
func safeText(value string, max int) bool {
	return value != "" && len(value) <= max && !strings.ContainsFunc(value, unicode.IsControl)
}

func (c *HTTPClient) do(request *http.Request) (*http.Response, traceState, error) {
	state := traceState{}
	request = request.WithContext(httptrace.WithClientTrace(request.Context(), state.trace()))
	response, err := c.httpClient.Do(request)
	return response, state, err
}

func (c *HTTPClient) ValidateRecipient(ctx context.Context, token, user string, devices []string) (RecipientValidation, error) {
	if err := validateCredentials(token, user); err != nil {
		return RecipientValidation{}, err
	}
	values := url.Values{"token": {token}, "user": {user}}
	if len(devices) > 0 {
		values.Set("device", strings.Join(devices, ","))
	}
	parsed, err := c.readWithRetry(ctx, http.MethodPost, "/1/users/validate.json", strings.NewReader(values.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return RecipientValidation{}, err
	}
	if err := responseError(parsed.code, parsed.response.status); err != nil {
		return RecipientValidation{}, err
	}
	return RecipientValidation{Valid: true, Devices: parsed.response.devices, Licenses: parsed.response.licenses, RequestID: safeRequestID(parsed.response.request)}, nil
}

func (c *HTTPClient) Sounds(ctx context.Context, token string) (SoundsResponse, error) {
	if err := validateToken(token); err != nil {
		return SoundsResponse{}, err
	}
	parsed, err := c.readWithRetry(ctx, http.MethodGet, "/1/sounds.json?token="+url.QueryEscape(token), nil, "")
	if err != nil {
		return SoundsResponse{}, err
	}
	if err := responseError(parsed.code, parsed.response.status); err != nil {
		return SoundsResponse{}, err
	}
	sounds := make([]Sound, 0, len(parsed.response.sounds))
	for name, description := range parsed.response.sounds {
		sounds = append(sounds, Sound{Name: name, Description: description})
	}
	sort.Slice(sounds, func(i, j int) bool { return sounds[i].Name < sounds[j].Name })
	return SoundsResponse{Sounds: sounds, RequestID: safeRequestID(parsed.response.request)}, nil
}

func (c *HTTPClient) Limits(ctx context.Context, token string) (LimitsResponse, error) {
	if err := validateToken(token); err != nil {
		return LimitsResponse{}, err
	}
	parsed, err := c.readWithRetry(ctx, http.MethodGet, "/1/apps/limits.json?token="+url.QueryEscape(token), nil, "")
	if err != nil {
		return LimitsResponse{}, err
	}
	if err := responseError(parsed.code, parsed.response.status); err != nil {
		return LimitsResponse{}, err
	}
	return LimitsResponse{Limit: parsed.response.limit, Remaining: parsed.response.remaining, Reset: parsed.response.reset, RequestID: safeRequestID(parsed.response.request)}, nil
}

type readResult struct {
	code     int
	response apiResponse
}

func (c *HTTPClient) readWithRetry(ctx context.Context, method, path string, body io.Reader, contentType string) (readResult, error) {
	// Callers use compact replayable forms or nil bodies. Read once so a retry
	// cannot accidentally issue a different request.
	var data []byte
	if body != nil {
		var err error
		data, err = io.ReadAll(body)
		if err != nil {
			return readResult{}, fixedError("network_failure")
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(data))
		if err != nil {
			return readResult{}, fixedError("network_failure")
		}
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		response, _, err := c.do(request)
		if err != nil {
			if attempt == 0 && (isExplicitTransient(err) || isTimeout(err)) && retryHasRoom(ctx) && c.backoff.Wait(ctx, attempt) == nil {
				continue
			}
			return readResult{}, redactedTransportError(err)
		}
		if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
			_ = response.Body.Close()
			if attempt == 0 && retryHasRoom(ctx) && c.backoff.Wait(ctx, attempt) == nil {
				continue
			}
			return readResult{}, responseError(response.StatusCode, 0)
		}
		parsed, decodeErr := decodeAPIResponse(response)
		_ = response.Body.Close()
		if decodeErr != nil {
			return readResult{}, decodeErr
		}
		return readResult{code: response.StatusCode, response: parsed}, nil
	}
	return readResult{}, fixedError("network_failure")
}

func retryHasRoom(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	deadline, ok := ctx.Deadline()
	return !ok || time.Until(deadline) >= 5*time.Second
}

func validateCredentials(token, user string) error {
	if err := validateToken(token); err != nil {
		return err
	}
	if err := validateToken(user); err != nil {
		return err
	}
	return nil
}
func validateToken(value string) error {
	if err := domain.ValidateIdentifier(value); err != nil {
		return fixedError("invalid_payload")
	}
	return nil
}

func parseQuotaInt(value string) *int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return nil
	}
	return &parsed
}
func parseQuotaInt64(value string) *int64 {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return nil
	}
	return &parsed
}

type productionBackoff struct{}

func normalizeBackoff(backoff Backoff) Backoff {
	if backoff != nil {
		return backoff
	}
	return productionBackoff{}
}
func (productionBackoff) Wait(ctx context.Context, attempt int) error {
	delay := 5 * time.Second
	if attempt > 0 {
		delay *= 2
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
