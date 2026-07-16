package e2ee

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestEncryptPayloadEncryptsPresentFieldsAndDoesNotMutateInput(t *testing.T) {
	timestamp := time.Unix(1_700_000_000, 0).UTC()
	input := domain.TargetPayload{
		Target: domain.Target{Alias: "phone", E2EEKey: []byte(testKey)},
		Message: domain.Message{
			Body:       "hello, 🌍",
			Title:      "title",
			URL:        "https://example.com/path",
			URLTitle:   "open",
			Devices:    []string{"iphone"},
			Timestamp:  &timestamp,
			Attachment: &domain.Attachment{Name: "image.png", MediaType: "image/png", Bytes: []byte{1, 2, 3}},
		},
	}
	original := clonePayload(input)
	random := append(bytes.Repeat([]byte{0x11}, 16), bytes.Repeat([]byte{0x22}, 16)...)
	random = append(random, bytes.Repeat([]byte{0x33}, 16)...)
	random = append(random, bytes.Repeat([]byte{0x44}, 16)...)

	got, err := (Encryptor{Random: bytes.NewReader(random)}).EncryptPayload(context.Background(), input)
	if err != nil {
		t.Fatalf("EncryptPayload() error = %v", err)
	}
	if !equalPayload(input, original) {
		t.Fatalf("EncryptPayload() mutated input\n got: %#v\nwant: %#v", input, original)
	}
	if len(got.Target.E2EEKey) != 0 {
		t.Fatal("EncryptPayload() returned E2EE key")
	}
	if got.Message.Timestamp == input.Message.Timestamp || got.Message.Attachment == input.Message.Attachment || &got.Message.Devices[0] == &input.Message.Devices[0] || &got.Message.Attachment.Bytes[0] == &input.Message.Attachment.Bytes[0] {
		t.Fatal("EncryptPayload() retained mutable input aliases")
	}
	got.Message.Timestamp = nil
	got.Message.Devices[0] = "changed"
	got.Message.Attachment.Bytes[0] = 9
	if input.Message.Timestamp == nil || input.Message.Devices[0] != "iphone" || input.Message.Attachment.Bytes[0] != 1 {
		t.Fatal("EncryptPayload() output can mutate input")
	}
	if got.Message.Body == input.Message.Body || got.Message.Title == input.Message.Title || got.Message.URL == input.Message.URL || got.Message.URLTitle == input.Message.URLTitle {
		t.Fatal("EncryptPayload() left a plaintext field in its output")
	}

	key, err := ParseKey(testKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []struct{ name, got, want string }{
		{"body", got.Message.Body, input.Message.Body},
		{"title", got.Message.Title, input.Message.Title},
		{"url", got.Message.URL, input.Message.URL},
		{"url title", got.Message.URLTitle, input.Message.URLTitle},
	} {
		if plain := decryptEnvelope(t, key, field.got); plain != field.want {
			t.Errorf("%s plaintext = %q, want %q", field.name, plain, field.want)
		}
	}
	for _, field := range []struct {
		name  string
		value string
		iv    byte
	}{
		{"body", got.Message.Body, 0x11},
		{"title", got.Message.Title, 0x22},
		{"url", got.Message.URL, 0x33},
		{"url title", got.Message.URLTitle, 0x44},
	} {
		envelope, err := base64.StdEncoding.DecodeString(field.value)
		if err != nil {
			t.Fatal(err)
		}
		if len(envelope) < 16 || !bytes.Equal(envelope[:16], bytes.Repeat([]byte{field.iv}, 16)) {
			t.Errorf("%s does not use its supplied fresh IV", field.name)
		}
	}
}

func TestEncryptPayloadLeavesAbsentOptionalFieldsAbsent(t *testing.T) {
	input := domain.TargetPayload{Target: domain.Target{E2EEKey: []byte(testKey)}, Message: domain.Message{Body: "body"}}
	got, err := (Encryptor{Random: bytes.NewReader(bytes.Repeat([]byte{0x99}, 16))}).EncryptPayload(context.Background(), input)
	if err != nil {
		t.Fatalf("EncryptPayload() error = %v", err)
	}
	if got.Message.Title != "" || got.Message.URL != "" || got.Message.URLTitle != "" {
		t.Fatalf("EncryptPayload() encrypted absent optional fields: %#v", got.Message)
	}
}

func TestEncryptPayloadFailsClosed(t *testing.T) {
	input := domain.TargetPayload{Target: domain.Target{E2EEKey: []byte("bad")}, Message: domain.Message{Body: "body"}}
	got, err := (Encryptor{Random: bytes.NewReader(bytes.Repeat([]byte{0x99}, 16))}).EncryptPayload(context.Background(), input)
	if err == nil || err.Error() != "invalid encryption key" {
		t.Fatalf("EncryptPayload() error = %v, want fixed invalid-key error", err)
	}
	if got.Message.Body != "" || len(got.Target.E2EEKey) != 0 {
		t.Fatalf("EncryptPayload() returned data on failed encryption: %#v", got)
	}
}

func TestEncryptPayloadHonorsContextBetweenFields(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelAfterReader{remaining: 16, cancel: cancel}
	input := domain.TargetPayload{Target: domain.Target{E2EEKey: []byte(testKey)}, Message: domain.Message{Body: "body", Title: "title"}}
	got, err := (Encryptor{Random: reader}).EncryptPayload(ctx, input)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("EncryptPayload() error = %v, want context cancellation", err)
	}
	if got.Message.Body != "" || len(got.Target.E2EEKey) != 0 {
		t.Fatalf("EncryptPayload() returned data on cancellation: %#v", got)
	}
}

func TestEncryptPayloadRejectsUnusableRandomness(t *testing.T) {
	input := domain.TargetPayload{Target: domain.Target{E2EEKey: []byte(testKey)}, Message: domain.Message{Body: "body"}}
	for _, random := range []io.Reader{
		bytes.NewReader(bytes.Repeat([]byte{0x99}, 15)),
		errReader{err: errors.New("reader failure")},
		&zeroThenDataReader{data: bytes.Repeat([]byte{0x99}, 16)},
	} {
		got, err := (Encryptor{Random: random}).EncryptPayload(context.Background(), input)
		if err == nil || err.Error() != "encryption failed" {
			t.Errorf("EncryptPayload() error = %v, want fixed encryption error", err)
		}
		if got.Message.Body != "" || len(got.Target.E2EEKey) != 0 {
			t.Errorf("EncryptPayload() returned data on random failure: %#v", got)
		}
	}
}

func TestEncryptPayloadRejectsInvalidRandomReaderCount(t *testing.T) {
	input := domain.TargetPayload{Target: domain.Target{E2EEKey: []byte(testKey)}, Message: domain.Message{Body: "body"}}
	panicked := false
	var err error
	func() {
		defer func() { panicked = recover() != nil }()
		_, err = (Encryptor{Random: invalidCountReader{}}).EncryptPayload(context.Background(), input)
	}()
	if panicked || err == nil || err.Error() != "encryption failed" {
		t.Fatalf("EncryptPayload() panicked=%t error=%v, want safe random failure", panicked, err)
	}
}

type cancelAfterReader struct {
	remaining int
	cancel    context.CancelFunc
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

type zeroThenDataReader struct {
	zero bool
	data []byte
}

type invalidCountReader struct{}

func (invalidCountReader) Read(dst []byte) (int, error) { return len(dst) + 1, nil }

func (r *zeroThenDataReader) Read(dst []byte) (int, error) {
	if !r.zero {
		r.zero = true
		return 0, nil
	}
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	count := copy(dst, r.data)
	r.data = r.data[count:]
	return count, nil
}

func (r *cancelAfterReader) Read(dst []byte) (int, error) {
	if r.remaining == 0 {
		return 0, nil
	}
	count := min(len(dst), r.remaining)
	for index := range dst[:count] {
		dst[index] = 0x55
	}
	r.remaining -= count
	if r.remaining == 0 {
		r.cancel()
	}
	return count, nil
}

func clonePayload(value domain.TargetPayload) domain.TargetPayload {
	copy := value
	copy.Target.E2EEKey = append([]byte(nil), value.Target.E2EEKey...)
	copy.Message.Devices = append([]string(nil), value.Message.Devices...)
	if value.Message.Attachment != nil {
		attachment := *value.Message.Attachment
		attachment.Bytes = append([]byte(nil), value.Message.Attachment.Bytes...)
		copy.Message.Attachment = &attachment
	}
	return copy
}

func equalPayload(left, right domain.TargetPayload) bool {
	return left.Target.Alias == right.Target.Alias && bytes.Equal(left.Target.E2EEKey, right.Target.E2EEKey) &&
		left.Message.Body == right.Message.Body && left.Message.Title == right.Message.Title &&
		left.Message.URL == right.Message.URL && left.Message.URLTitle == right.Message.URLTitle &&
		strings.Join(left.Message.Devices, "\x00") == strings.Join(right.Message.Devices, "\x00")
}
