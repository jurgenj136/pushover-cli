package pushover

import (
	"context"
	"net/http"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

func (c *HTTPClient) Send(ctx context.Context, payload domain.TargetPayload) (SendResponse, error) {
	if err := validatePayload(payload); err != nil {
		return SendResponse{}, newSendError(err, PhaseBeforeDial, true)
	}
	body, contentType, err := encodedPayload(payload)
	if err != nil {
		return SendResponse{}, newSendError(err, PhaseBeforeDial, true)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/1/messages.json", body)
	if err != nil {
		return SendResponse{}, newSendError(err, PhaseBeforeDial, true)
	}
	// A send is non-idempotent. Do not leave a replayable body available to
	// any transport behavior beyond this package's single explicit attempt.
	request.GetBody = nil
	request.Header.Set("Content-Type", contentType)
	response, trace, err := c.do(request)
	if err != nil {
		return SendResponse{}, classifySendError(err, trace)
	}
	defer func() { _ = response.Body.Close() }()
	parsed, err := decodeAPIResponse(response)
	if err != nil {
		return SendResponse{}, newSendError(err, PhaseDecode, true)
	}
	if err := responseError(response.StatusCode, parsed.status); err != nil {
		return SendResponse{}, newSendError(err, PhaseRead, true)
	}
	return SendResponse{RequestID: safeRequestID(parsed.request), ReceiptID: safeReceiptID(parsed.receipt), QuotaRemaining: parseQuotaInt(response.Header.Get("X-Limit-App-Remaining")), QuotaLimit: parseQuotaInt64(response.Header.Get("X-Limit-App-Limit")), QuotaReset: parseQuotaInt64(response.Header.Get("X-Limit-App-Reset"))}, nil
}
