package e2ee

import (
	"context"
	"crypto/rand"
	"errors"
	"io"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

var errEncryptionFailed = errors.New("encryption failed")

// Service is the encryption boundary used by send preflight.
type Service interface {
	EncryptPayload(context.Context, domain.TargetPayload) (domain.TargetPayload, error)
}

// Encryptor transforms Pushover's supported encrypted fields. Random defaults
// to crypto/rand.Reader; it is injectable solely for deterministic tests.
type Encryptor struct {
	Random io.Reader
}

// EncryptPayload encrypts message body and each present optional encrypted
// field independently. It returns no partial payload on failure.
func (e Encryptor) EncryptPayload(ctx context.Context, input domain.TargetPayload) (domain.TargetPayload, error) {
	if err := ctx.Err(); err != nil {
		return domain.TargetPayload{}, err
	}

	key, err := parseKeyBytes(input.Target.E2EEKey)
	if err != nil {
		return domain.TargetPayload{}, err
	}
	defer clear(key[:])

	random := e.Random
	if random == nil {
		random = rand.Reader
	}
	output := cloneTargetPayload(input)
	defer clear(output.Target.E2EEKey)

	if output.Message.Body, err = encryptField(key[:], random, output.Message.Body); err != nil {
		return domain.TargetPayload{}, err
	}
	for _, field := range []*string{&output.Message.Title, &output.Message.URL, &output.Message.URLTitle} {
		if err := ctx.Err(); err != nil {
			return domain.TargetPayload{}, err
		}
		if *field == "" {
			continue
		}
		if *field, err = encryptField(key[:], random, *field); err != nil {
			return domain.TargetPayload{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return domain.TargetPayload{}, err
	}
	clear(output.Target.E2EEKey)
	output.Target.E2EEKey = nil
	return output, nil
}

func cloneTargetPayload(input domain.TargetPayload) domain.TargetPayload {
	output := input
	output.Target.E2EEKey = append([]byte(nil), input.Target.E2EEKey...)
	output.Message.Devices = append([]string(nil), input.Message.Devices...)
	if input.Message.Timestamp != nil {
		timestamp := *input.Message.Timestamp
		output.Message.Timestamp = &timestamp
	}
	if input.Message.Attachment != nil {
		attachment := *input.Message.Attachment
		attachment.Bytes = append([]byte(nil), input.Message.Attachment.Bytes...)
		output.Message.Attachment = &attachment
	}
	return output
}
