package resolver

import (
	"context"
	"io"
	"os"
	"unicode/utf8"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/secrets"
)

type Resolver struct {
	Config  config.Store
	Secrets secrets.Store
}

func (r *Resolver) Resolve(ctx context.Context, input Input) (domain.ResolvedSendDraft, error) {
	if err := ctx.Err(); err != nil {
		return domain.ResolvedSendDraft{}, err
	}
	if err := validateContext(input); err != nil {
		return domain.ResolvedSendDraft{}, err
	}
	body, err := readBody(input.Body)
	if err != nil {
		return domain.ResolvedSendDraft{}, err
	}
	if input.EnvTokenSet || input.EnvUserKeySet {
		return r.resolveSupplied(input, body, suppliedCredentials{Credentials: domain.Credentials{Token: input.EnvToken, UserKey: input.EnvUserKey}, Alias: "environment"})
	}
	if input.Credentials != nil {
		credentials, err := parseCredentialsJSON(input.Credentials)
		if err != nil {
			return domain.ResolvedSendDraft{}, err
		}
		defer wipe(credentials.E2EEKey)
		return r.resolveSupplied(input, body, credentials)
	}
	return r.resolveConfigured(ctx, input, body)
}

func validateContext(input Input) error {
	if input.EnvTokenSet != input.EnvUserKeySet {
		return apperror.New("environment_credentials_incomplete", apperror.CategoryUsage, "environment credentials must be supplied together")
	}
	if input.EnvTokenSet {
		if input.Profile != "" || len(input.To) != 0 || input.Credentials != nil {
			return apperror.New("credential_context_conflict", apperror.CategoryUsage, "credential sources cannot be combined")
		}
		if err := domain.ValidateCredentials(domain.Credentials{Token: input.EnvToken, UserKey: input.EnvUserKey}); err != nil {
			return apperror.New("environment_credentials_invalid", apperror.CategoryValidation, "environment credentials are invalid")
		}
	}
	if input.Credentials != nil && (input.Profile != "" || len(input.To) != 0 || input.Body.Kind == BodyStdin) {
		return apperror.New("credential_context_conflict", apperror.CategoryUsage, "credential sources cannot be combined")
	}
	return nil
}

func (r *Resolver) resolveSupplied(input Input, body string, supplied suppliedCredentials) (domain.ResolvedSendDraft, error) {
	if err := domain.ValidateCredentials(supplied.Credentials); err != nil {
		return domain.ResolvedSendDraft{}, apperror.New("credentials_invalid", apperror.CategoryValidation, "credentials are invalid")
	}
	message := effectiveMessage(input, config.MessageDefaults{}, config.Recipient{}, body)
	target := domain.Target{Alias: supplied.Alias, Credentials: supplied.Credentials}
	if message.Encrypted {
		target.E2EEKey = append([]byte(nil), supplied.E2EEKey...)
	}
	payload := domain.TargetPayload{Target: target, Message: message}
	if err := domain.ValidateTargetPayload(payload); err != nil {
		wipe(payload.Target.E2EEKey)
		return domain.ResolvedSendDraft{}, apperror.New("message_invalid", apperror.CategoryValidation, "message configuration is invalid")
	}
	return domain.ResolvedSendDraft{Command: "send", Targets: []domain.TargetPayload{payload}}, nil
}

func (r *Resolver) resolveConfigured(ctx context.Context, input Input, body string) (domain.ResolvedSendDraft, error) {
	if r == nil || r.Config == nil || r.Secrets == nil {
		return domain.ResolvedSendDraft{}, apperror.New("resolver_unavailable", apperror.CategoryConfig, "configuration is unavailable")
	}
	if err := duplicateAliases(input.To); err != nil {
		return domain.ResolvedSendDraft{}, err
	}
	if len(input.To) == 0 {
		return domain.ResolvedSendDraft{}, apperror.New("recipient_required", apperror.CategoryUsage, "at least one recipient alias is required")
	}
	value, err := r.Config.Load(ctx)
	if err != nil {
		return domain.ResolvedSendDraft{}, apperror.Wrap("configuration_unavailable", apperror.CategoryConfig, "configuration is unavailable", err)
	}
	profileName := input.Profile
	if profileName == "" {
		profileName = value.DefaultProfile
	}
	profile, ok := value.Profiles[profileName]
	if !ok {
		return domain.ResolvedSendDraft{}, apperror.New("profile_missing", apperror.CategoryConfig, "selected profile is unavailable")
	}
	recipients := make([]config.Recipient, 0, len(input.To))
	for _, alias := range input.To {
		recipient, ok := value.Recipients[alias]
		if !ok {
			return domain.ResolvedSendDraft{}, apperror.New("recipient_missing", apperror.CategoryConfig, "requested recipient is unavailable")
		}
		recipients = append(recipients, recipient)
	}
	token, err := r.Secrets.Get(ctx, profile.TokenRef)
	if err != nil {
		return domain.ResolvedSendDraft{}, apperror.Wrap("secret_reference_missing", apperror.CategoryConfig, "required credentials are unavailable", err)
	}
	defer wipe(token)
	type resolvedRecipient struct {
		recipient config.Recipient
		userKey   []byte
		e2ee      []byte
	}
	resolved := make([]resolvedRecipient, 0, len(input.To))
	cleanup := func() {
		for _, value := range resolved {
			wipe(value.userKey)
			wipe(value.e2ee)
		}
	}
	for _, recipient := range recipients {
		userKey, err := r.Secrets.Get(ctx, recipient.UserKeyRef)
		if err != nil {
			cleanup()
			return domain.ResolvedSendDraft{}, apperror.Wrap("secret_reference_missing", apperror.CategoryConfig, "required credentials are unavailable", err)
		}
		e2ee, err := resolveE2EE(ctx, r.Secrets, recipient.E2EEKeyRef, input.Message.Encrypted)
		if err != nil {
			wipe(userKey)
			cleanup()
			return domain.ResolvedSendDraft{}, err
		}
		resolved = append(resolved, resolvedRecipient{recipient: recipient, userKey: userKey, e2ee: e2ee})
	}
	targets := make([]domain.TargetPayload, 0, len(resolved))
	for _, value := range resolved {
		message := effectiveMessage(input, profile.Defaults, value.recipient, body)
		payload := domain.TargetPayload{Target: domain.Target{Alias: value.recipient.Alias, Credentials: domain.Credentials{Token: string(token), UserKey: string(value.userKey)}, E2EEKey: append([]byte(nil), value.e2ee...), Kind: value.recipient.Kind, Validated: value.recipient.Validated}, Message: message}
		wipe(value.userKey)
		wipe(value.e2ee)
		if err := domain.ValidateTargetPayload(payload); err != nil {
			wipePayloadKeys(targets)
			wipe(payload.Target.E2EEKey)
			cleanup()
			return domain.ResolvedSendDraft{}, apperror.New("message_invalid", apperror.CategoryValidation, "message configuration is invalid")
		}
		targets = append(targets, payload)
	}
	return domain.ResolvedSendDraft{Command: "send", Targets: targets}, nil
}

func resolveE2EE(ctx context.Context, store secrets.Store, ref domain.SecretRef, required bool) ([]byte, error) {
	if !required {
		return nil, nil
	}
	if ref == "" {
		return nil, apperror.New("encryption_key_missing", apperror.CategoryConfig, "encryption credentials are unavailable")
	}
	key, err := store.Get(ctx, ref)
	if err != nil {
		return nil, apperror.Wrap("encryption_key_missing", apperror.CategoryConfig, "encryption credentials are unavailable", err)
	}
	if err := domain.ValidateE2EEKey(key); err != nil {
		wipe(key)
		return nil, apperror.New("encryption_key_invalid", apperror.CategoryValidation, "encryption credentials are invalid")
	}
	return key, nil
}

func duplicateAliases(aliases []string) error {
	seen := make(map[string]struct{}, len(aliases))
	for _, alias := range aliases {
		if _, exists := seen[alias]; exists {
			return apperror.New("duplicate_recipient", apperror.CategoryUsage, "recipient aliases must be unique")
		}
		seen[alias] = struct{}{}
	}
	return nil
}

func readBody(source BodySource) (string, error) {
	var reader io.Reader
	var closeReader func() error
	switch source.Kind {
	case BodyPositional:
		reader = &stringReader{value: source.Value}
	case BodyStdin:
		if source.Reader == nil {
			return "", apperror.New("body_source_invalid", apperror.CategoryUsage, "exactly one message body source is required")
		}
		reader = source.Reader
	case BodyFile:
		open := source.OpenFile
		if open == nil {
			open = func(name string) (io.ReadCloser, error) {
				// #nosec G304 -- name is the explicit user-selected message body file.
				return os.Open(name)
			}
		}
		file, err := open(source.Value)
		if err != nil {
			return "", apperror.New("body_file_unavailable", apperror.CategoryConfig, "message body file is unavailable")
		}
		reader, closeReader = file, file.Close
	default:
		return "", apperror.New("body_source_invalid", apperror.CategoryUsage, "exactly one message body source is required")
	}
	data, err := readBoundedBody(io.LimitReader(reader, MaxBodyBytes+1))
	defer wipe(data)
	if closeReader != nil {
		if closeErr := closeReader(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		return "", apperror.New("body_read_failed", apperror.CategoryConfig, "message body could not be read")
	}
	if len(data) > int(MaxBodyBytes) || !utf8.Valid(data) || len(data) == 0 || utf8.RuneCount(data) > domain.MaxBodyRunes {
		return "", apperror.New("body_invalid", apperror.CategoryValidation, "message body is invalid")
	}
	return string(data), nil
}

// readBoundedBody preserves the MaxBodyBytes+1 sentinel without trusting a
// reader's advertised length. A reader returning (0, nil) is malformed; fail
// instead of spinning forever before the first network operation.
func readBoundedBody(reader io.Reader) ([]byte, error) {
	data := make([]byte, 0, MaxBodyBytes+1)
	buffer := make([]byte, 8192)
	for {
		if int64(len(data)) >= MaxBodyBytes+1 {
			return data, nil
		}
		remaining := int(MaxBodyBytes + 1 - int64(len(data)))
		if remaining < len(buffer) {
			buffer = buffer[:remaining]
		}
		count, err := reader.Read(buffer)
		if count > 0 {
			data = append(data, buffer[:count]...)
		}
		if err == io.EOF {
			return data, nil
		}
		if err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, io.ErrNoProgress
		}
	}
}

type stringReader struct {
	value  string
	offset int
}

func (r *stringReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.value) {
		return 0, io.EOF
	}
	n := copy(p, r.value[r.offset:])
	r.offset += n
	return n, nil
}
func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func wipePayloadKeys(payloads []domain.TargetPayload) {
	for index := range payloads {
		wipe(payloads[index].Target.E2EEKey)
	}
}
