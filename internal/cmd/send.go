package cmd

import (
	"bytes"
	"context"
	"net/url"
	"strconv"
	"time"

	"github.com/alecthomas/kong"
	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/delivery"
	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/output"
	"github.com/jurgenj136/pushover-cli/internal/preflight"
	"github.com/jurgenj136/pushover-cli/internal/resolver"
)

type SendCommand struct {
	To               []string `name:"to" help:"Recipient alias (repeatable)."`
	Title            string   `name:"title" help:"Message title."`
	Devices          []string `name:"device" help:"Target device (repeatable)."`
	Priority         int      `name:"priority" help:"Message priority (-2 through 2)."`
	Sound            string   `name:"sound" help:"Notification sound."`
	Timestamp        string   `name:"timestamp" help:"Unix seconds or RFC3339 timestamp."`
	TTL              string   `name:"ttl" help:"Time-to-live duration."`
	URL              string   `name:"url" help:"HTTPS action URL."`
	URLTitle         string   `name:"url-title" help:"Action URL title."`
	HTML             bool     `name:"html" help:"Enable HTML formatting."`
	Monospace        bool     `name:"monospace" help:"Enable monospace formatting."`
	Attachment       string   `name:"attachment" help:"Local image attachment."`
	AttachmentURL    string   `name:"attachment-url" help:"Remote HTTPS image attachment."`
	Encrypted        bool     `name:"encrypted" help:"Encrypt supported fields."`
	Retry            string   `name:"retry" help:"Emergency retry interval."`
	Expire           string   `name:"expire" help:"Emergency expiry duration."`
	Validate         bool     `name:"validate" help:"Validate recipients before sending."`
	ValidateNetwork  bool     `name:"validate-network" help:"Allow network attachment validation in a dry run."`
	DryRun           bool     `name:"dry-run" help:"Build and print a plan without sending."`
	Yes              bool     `name:"yes" help:"Accept required emergency confirmation."`
	Stdin            bool     `name:"stdin" help:"Read the message body from standard input."`
	File             string   `name:"file" help:"Read the message body from a file."`
	CredentialsStdin bool     `name:"credentials-stdin" help:"Read one-off credentials JSON from standard input."`
	Body             string   `arg:"" optional:"" help:"Message body."`
}

func executeSend(ctx context.Context, streams IO, deps Dependencies, root rootCommand, parsed *kong.Context) int {
	if ctx == nil {
		ctx = context.Background()
	}
	mode := selectedMode(root.JSON, root.Plain)
	request, err := root.Send.request(parsed, streams, deps, root.Profile)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, "send", asAppError(err), root.NoColor)
	}
	timeout, err := parseTimeout(root.Timeout, flagSeen(parsed, "timeout"))
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, "send", asAppError(err), root.NoColor)
	}
	scope, err := startScope(ctx, timeout, deps)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, "send", asAppError(err), root.NoColor)
	}
	defer scope.Close()

	builder, runner, err := deps.openRuntime(scope.Context(), scope)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, "send", asAppError(err), root.NoColor)
	}
	if builder.Resolver == nil {
		return renderErrorWithColor(streams, deps, mode, "send", apperror.New("preflight_unavailable", apperror.CategoryInternal, apperror.MessageInternal), root.NoColor)
	}
	builder.Scope = scope
	runner.Scope = scope
	runner.Signals = deps.Signals
	if runner.Confirmer == nil {
		runner.Confirmer = delivery.TerminalConfirmer{Input: streams.In, Stderr: streams.Err, IsTTY: terminalInput(streams)}
	}

	plan, planned, err := builder.Build(scope.Context(), request)
	if planned != nil {
		return renderBatch(streams, deps, mode, root.NoColor, *planned, err)
	}
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, "send", asAppError(err), root.NoColor)
	}
	if request.DryRun {
		return renderErrorWithColor(streams, deps, mode, "send", apperror.New("dry_run_failed", apperror.CategoryInternal, apperror.MessageInternal), root.NoColor)
	}
	if runner.Client == nil {
		return renderErrorWithColor(streams, deps, mode, "send", apperror.New("delivery_unavailable", apperror.CategoryInternal, apperror.MessageInternal), root.NoColor)
	}
	batch, runErr := runner.Run(scope.Context(), plan, delivery.RunOptions{AssumeYes: root.Send.Yes})
	if len(batch.Results) > 0 {
		return renderBatch(streams, deps, mode, root.NoColor, batch, runErr)
	}
	if runErr != nil {
		return renderErrorWithColor(streams, deps, mode, "send", asAppError(runErr), root.NoColor)
	}
	return renderErrorWithColor(streams, deps, mode, "send", apperror.New("delivery_empty", apperror.CategoryInternal, apperror.MessageInternal), root.NoColor)
}

func (s SendCommand) request(parsed *kong.Context, streams IO, deps Dependencies, profile string) (preflight.Request, error) {
	stdin, file, positional := flagSeen(parsed, "stdin"), flagSeen(parsed, "file"), positionalSeen(parsed, "body")
	if countTrue(stdin, file, positional) != 1 {
		return preflight.Request{}, apperror.New("body_source_conflict", apperror.CategoryUsage, "exactly one message body source is required")
	}
	credentialsStdin := flagSeen(parsed, "credentials-stdin")
	if credentialsStdin && stdin {
		return preflight.Request{}, apperror.New("credential_context_conflict", apperror.CategoryUsage, "credential sources cannot be combined")
	}
	if flagSeen(parsed, "attachment") && flagSeen(parsed, "attachment-url") {
		return preflight.Request{}, apperror.New("attachment_conflict", apperror.CategoryUsage, "local and remote attachments cannot be combined")
	}
	timestamp, err := parseTimestamp(s.Timestamp, flagSeen(parsed, "timestamp"))
	if err != nil {
		return preflight.Request{}, err
	}
	ttl, err := parseDuration(s.TTL, flagSeen(parsed, "ttl"), "ttl")
	if err != nil {
		return preflight.Request{}, err
	}
	retry, err := parseDuration(s.Retry, flagSeen(parsed, "retry"), "retry")
	if err != nil {
		return preflight.Request{}, err
	}
	expire, err := parseDuration(s.Expire, flagSeen(parsed, "expire"), "expire")
	if err != nil {
		return preflight.Request{}, err
	}

	remoteURL, err := parseRemoteURL(s.AttachmentURL, flagSeen(parsed, "attachment-url"))
	if err != nil {
		return preflight.Request{}, err
	}
	token, tokenSet := deps.environment("PUSHOVER_TOKEN")
	userKey, userKeySet := deps.environment("PUSHOVER_USER_KEY")
	if err := validateCredentialContext(profile, s.To, credentialsStdin, tokenSet, userKeySet); err != nil {
		return preflight.Request{}, err
	}
	input := resolver.Input{
		Profile:       profile,
		To:            append([]string(nil), s.To...),
		EnvToken:      token,
		EnvUserKey:    userKey,
		EnvTokenSet:   tokenSet,
		EnvUserKeySet: userKeySet,
		Message: domain.Message{
			Title: s.Title, Devices: append([]string(nil), s.Devices...), Priority: s.Priority,
			Timestamp: timestamp, TTL: ttl, Sound: s.Sound, URL: s.URL, URLTitle: s.URLTitle,
			HTML: s.HTML, Monospace: s.Monospace, Encrypted: s.Encrypted, Retry: retry, Expire: expire,
		},
		MessageFields: resolver.MessageFieldSet{
			Title: flagSeen(parsed, "title"), URL: flagSeen(parsed, "url"), URLTitle: flagSeen(parsed, "url-title"),
			Sound: flagSeen(parsed, "sound"), Devices: flagSeen(parsed, "device"), Priority: flagSeen(parsed, "priority"), TTL: flagSeen(parsed, "ttl"),
		},
	}
	switch {
	case positional:
		input.Body = resolver.BodySource{Kind: resolver.BodyPositional, Value: s.Body}
	case stdin:
		input.Body = resolver.BodySource{Kind: resolver.BodyStdin, Reader: streams.In}
	case file:
		input.Body = resolver.BodySource{Kind: resolver.BodyFile, Value: s.File}
	}
	if credentialsStdin {
		input.Credentials = streams.In
	}
	return preflight.Request{
		Resolve:         input,
		Attachment:      preflight.AttachmentInput{LocalPath: attachmentValue(s.Attachment, flagSeen(parsed, "attachment")), RemoteURL: remoteURL},
		DryRun:          s.DryRun,
		Validate:        s.Validate,
		ValidateNetwork: s.ValidateNetwork,
	}, nil
}

func validateCredentialContext(profile string, aliases []string, credentialsStdin, tokenSet, userKeySet bool) error {
	if tokenSet != userKeySet {
		return apperror.New("environment_credentials_incomplete", apperror.CategoryUsage, "environment credentials must be supplied together")
	}
	if tokenSet && (profile != "" || len(aliases) != 0 || credentialsStdin) {
		return apperror.New("credential_context_conflict", apperror.CategoryUsage, "credential sources cannot be combined")
	}
	if credentialsStdin && (profile != "" || len(aliases) != 0) {
		return apperror.New("credential_context_conflict", apperror.CategoryUsage, "credential sources cannot be combined")
	}
	return nil
}

func attachmentValue(value string, set bool) string {
	if !set {
		return ""
	}
	return value
}

func flagSeen(ctx *kong.Context, name string) bool {
	if ctx == nil {
		return false
	}
	for _, path := range ctx.Path {
		if path.Flag != nil && path.Flag.Name == name {
			return true
		}
	}
	return false
}

func positionalSeen(ctx *kong.Context, name string) bool {
	if ctx == nil {
		return false
	}
	for _, path := range ctx.Path {
		if path.Positional != nil && path.Positional.Name == name {
			return true
		}
	}
	return false
}

func countTrue(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func parseDuration(raw string, present bool, name string) (time.Duration, error) {
	if !present {
		return 0, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, apperror.New("invalid_"+name, apperror.CategoryUsage, name+" must be a duration")
	}
	return value, nil
}

func parseTimestamp(raw string, present bool) (*time.Time, error) {
	if !present {
		return nil, nil
	}
	if isBase10Integer(raw) {
		seconds, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, apperror.New("invalid_timestamp", apperror.CategoryUsage, "timestamp must be Unix seconds or RFC3339")
		}
		value := time.Unix(seconds, 0).UTC()
		return &value, nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, apperror.New("invalid_timestamp", apperror.CategoryUsage, "timestamp must be Unix seconds or RFC3339")
	}
	return &value, nil
}

func isBase10Integer(raw string) bool {
	if raw == "" {
		return false
	}
	if raw[0] == '-' {
		raw = raw[1:]
	}
	if raw == "" {
		return false
	}
	for _, value := range raw {
		if value < '0' || value > '9' {
			return false
		}
	}
	return true
}

func parseRemoteURL(raw string, present bool) (*url.URL, error) {
	if !present {
		return nil, nil
	}
	value, err := url.Parse(raw)
	if err != nil {
		return nil, apperror.New("attachment_url", apperror.CategoryValidation, "remote attachment URL must be an absolute HTTPS URL without credentials")
	}
	return value, nil
}

func renderBatch(streams IO, deps Dependencies, mode output.Mode, noColor bool, batch domain.BatchResult, commandErr error) int {
	renderer, err := newRenderer(streams, deps, mode, noColor, streams.Out)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, "send", asAppError(err), noColor)
	}
	result := output.SendResult{Meta: output.Meta{Command: "send", Status: sendStatus(batch)}, Batch: batch}
	var encoded bytes.Buffer
	if err := renderer.Result(&encoded, result); err != nil {
		return renderErrorWithColor(streams, deps, mode, "send", asAppError(err), noColor)
	}
	if err := commitOutput(streams.Out, encoded.Bytes()); err != nil {
		return renderErrorWithColor(streams, deps, mode, "send", asAppError(err), noColor)
	}
	return apperror.Exit(commandErr, &batch)
}

func sendStatus(batch domain.BatchResult) string {
	if batch.DryRun {
		return "dry_run"
	}
	sent, failed, ambiguous, skipped := 0, 0, 0, 0
	for _, result := range batch.Results {
		switch result.Outcome {
		case domain.Sent:
			sent++
		case domain.Failed:
			failed++
		case domain.Ambiguous:
			ambiguous++
		case domain.Skipped:
			skipped++
		}
	}
	if sent == len(batch.Results) && !batch.Interrupted {
		return "success"
	}
	if sent == 0 && ambiguous > 0 {
		return "ambiguous"
	}
	if batch.Interrupted && sent == 0 && ambiguous == 0 {
		return "interrupted"
	}
	if sent == 0 && failed > 0 && ambiguous == 0 {
		return "failed"
	}
	_ = skipped
	return "partial"
}
