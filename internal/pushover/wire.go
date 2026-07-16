package pushover

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/url"
	"strconv"
	"strings"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

func payloadValues(payload domain.TargetPayload) url.Values {
	message := payload.Message
	values := url.Values{
		"token":    {payload.Target.Credentials.Token},
		"user":     {payload.Target.Credentials.UserKey},
		"message":  {message.Body},
		"priority": {strconv.Itoa(message.Priority)},
	}
	if message.Title != "" {
		values.Set("title", message.Title)
	}
	if len(message.Devices) > 0 {
		values.Set("device", strings.Join(message.Devices, ","))
	}
	if message.Sound != "" {
		values.Set("sound", message.Sound)
	}
	if message.URL != "" {
		values.Set("url", message.URL)
	}
	if message.URLTitle != "" {
		values.Set("url_title", message.URLTitle)
	}
	if message.Timestamp != nil {
		values.Set("timestamp", strconv.FormatInt(message.Timestamp.Unix(), 10))
	}
	if message.TTL != 0 {
		values.Set("ttl", strconv.FormatInt(int64(message.TTL.Seconds()), 10))
	}
	if message.HTML {
		values.Set("html", "1")
	}
	if message.Monospace {
		values.Set("monospace", "1")
	}
	if message.Priority == domain.PriorityEmergency {
		values.Set("retry", strconv.FormatInt(int64(message.Retry.Seconds()), 10))
		values.Set("expire", strconv.FormatInt(int64(message.Expire.Seconds()), 10))
	}
	if message.Encrypted {
		values.Set("encrypted", "1")
	}
	return values
}

func encodedPayload(payload domain.TargetPayload) (io.Reader, string, error) {
	values := payloadValues(payload)
	if payload.Message.Attachment == nil {
		return strings.NewReader(values.Encode()), "application/x-www-form-urlencoded", nil
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, values := range values {
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				return nil, "", err
			}
		}
	}
	part, err := writer.CreateFormFile("attachment", "attachment")
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(payload.Message.Attachment.Bytes); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &body, writer.FormDataContentType(), nil
}
