package preflight

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/attachment"
	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/e2ee"
	"github.com/jurgenj136/pushover-cli/internal/operation"
	"github.com/jurgenj136/pushover-cli/internal/pushover"
	"github.com/jurgenj136/pushover-cli/internal/resolver"
)

func testScope(t *testing.T) operation.Scope {
	t.Helper()
	d := time.Second
	scope, err := (operation.Factory{}).Start(context.Background(), &d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scope.Close)
	return scope
}

type countingLoader struct {
	localCalls, remoteCalls int
	attachment              *domain.Attachment
}

func (l *countingLoader) Local(context.Context, string) (*domain.Attachment, error) {
	l.localCalls++
	return l.attachment, nil
}
func (l *countingLoader) Remote(context.Context, *url.URL) (*domain.Attachment, error) {
	l.remoteCalls++
	return l.attachment, nil
}

type fakeClient struct {
	validates, sends int
	validationErr    error
	events           *[]string
}

func (c *fakeClient) Send(context.Context, domain.TargetPayload) (pushover.SendResponse, error) {
	c.sends++
	return pushover.SendResponse{}, nil
}
func (c *fakeClient) ValidateRecipient(context.Context, string, string, []string) (pushover.RecipientValidation, error) {
	c.validates++
	if c.events != nil {
		*c.events = append(*c.events, "validate")
	}
	if c.validationErr != nil {
		return pushover.RecipientValidation{}, c.validationErr
	}
	return pushover.RecipientValidation{Valid: true}, nil
}
func (c *fakeClient) Sounds(context.Context, string) (pushover.SoundsResponse, error) {
	return pushover.SoundsResponse{}, nil
}
func (c *fakeClient) Limits(context.Context, string) (pushover.LimitsResponse, error) {
	return pushover.LimitsResponse{}, nil
}

type fakeEncryptor struct {
	calls  int
	err    error
	events *[]string
}

func (e *fakeEncryptor) EncryptPayload(_ context.Context, payload domain.TargetPayload) (domain.TargetPayload, error) {
	e.calls++
	if e.events != nil {
		*e.events = append(*e.events, "encrypt")
	}
	if e.err != nil {
		return domain.TargetPayload{}, e.err
	}
	payload.Message.Body = strings.Repeat("A", 64)
	payload.Message.Encrypted = true
	payload.Target.E2EEKey = nil
	return payload, nil
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	value, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

var _ attachment.Loader = (*countingLoader)(nil)

func suppliedInput(encrypted bool) resolver.Input {
	credentials := map[string]string{
		"token":    strings.Repeat("t", domain.IdentifierLength),
		"user_key": strings.Repeat("u", domain.IdentifierLength),
	}
	if encrypted {
		credentials["e2ee_key"] = strings.Repeat("a", 64)
	}
	data, _ := json.Marshal(credentials)
	return resolver.Input{
		Credentials: strings.NewReader(string(data)),
		Message:     domain.Message{Body: "body", Encrypted: encrypted, Priority: domain.PriorityNormal},
		Body:        resolver.BodySource{Kind: resolver.BodyPositional, Value: "body"},
	}
}

func TestFinalizedSendPlanAccessorsDefensivelyCopy(t *testing.T) {
	plan := FinalizedSendPlan{targets: []domain.TargetPayload{{Target: domain.Target{Alias: "a"}, Message: domain.Message{Body: "body", Devices: []string{"phone"}}}}, confirmation: Confirmation{Aliases: []string{"a"}}, sealed: finalPlanSeal}
	got := plan.Targets()
	got[0].Message.Devices[0] = "changed"
	got[0].Message.Body = "changed"
	aliases := plan.Confirmation().Aliases
	aliases[0] = "changed"
	if plan.targets[0].Message.Devices[0] != "phone" || plan.targets[0].Message.Body != "body" || plan.confirmation.Aliases[0] != "a" {
		t.Fatal("accessors exposed mutable plan state")
	}
}

func TestBuilderDryRunReturnsPlannedResultAndZeroPlan(t *testing.T) {
	b := Builder{Resolver: &resolver.Resolver{}, Scope: testScope(t)}
	plan, result, err := b.Build(context.Background(), Request{Resolve: suppliedInput(false), DryRun: true})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !plan.zero() {
		t.Fatal("dry run returned runnable plan")
	}
	if result == nil || !result.DryRun || len(result.Results) != 1 || result.Results[0].Outcome != domain.Planned {
		t.Fatalf("unexpected dry-run result: %#v", result)
	}
}

func TestBuilderRejectsConflictingAttachmentBeforeReads(t *testing.T) {
	loader := &countingLoader{}
	b := Builder{Resolver: &resolver.Resolver{}, Attachments: loader, Scope: testScope(t)}
	remote := mustURL(t, "https://example.com/image.png")
	_, result, err := b.Build(context.Background(), Request{Resolve: suppliedInput(false), Attachment: AttachmentInput{LocalPath: "local", RemoteURL: remote}})
	if err == nil || result != nil {
		t.Fatalf("expected conflict error and nil result, got result=%#v err=%v", result, err)
	}
	if loader.localCalls != 0 || loader.remoteCalls != 0 {
		t.Fatalf("attachment loader called before conflict validation: %#v", loader)
	}
}

func TestBuilderRejectsPoorlyCompressibleEncryptedExpansion(t *testing.T) {
	input := suppliedInput(true)
	var body strings.Builder
	for i := 0; i < domain.MaxBodyRunes; i++ {
		body.WriteRune(rune(0x1000 + i))
	}
	input.Body.Value = body.String()
	b := Builder{Resolver: &resolver.Resolver{}, Encryptor: e2ee.Encryptor{Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 16))}, Scope: testScope(t)}
	plan, result, err := b.Build(context.Background(), Request{Resolve: input})
	if err == nil || !plan.zero() || result != nil {
		t.Fatalf("expected post-encryption wire rejection, plan=%#v result=%#v err=%v", plan, result, err)
	}
}

func TestBuilderAcceptsBodyOnlyEncryptedMessage(t *testing.T) {
	b := Builder{Resolver: &resolver.Resolver{}, Encryptor: e2ee.Encryptor{Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 16))}, Scope: testScope(t)}
	plan, result, err := b.Build(context.Background(), Request{Resolve: suppliedInput(true)})
	if err != nil || result != nil || !plan.IsValid() {
		t.Fatalf("body-only encrypted message rejected: plan=%#v result=%#v err=%v", plan, result, err)
	}
}

func TestDryRunValidationAndRemoteNetworkControls(t *testing.T) {
	client := &fakeClient{}
	loader := &countingLoader{attachment: &domain.Attachment{Name: "attachment.png", MediaType: "image/png", Bytes: []byte("data")}}
	b := Builder{Resolver: &resolver.Resolver{}, Attachments: loader, Validator: client, Scope: testScope(t)}
	remote := mustURL(t, "https://example.com/image.png")
	_, result, err := b.Build(context.Background(), Request{Resolve: suppliedInput(false), DryRun: true, Validate: true, Attachment: AttachmentInput{RemoteURL: remote}})
	if err != nil || result == nil || client.validates != 0 || loader.remoteCalls != 0 {
		t.Fatalf("offline dry-run performed forbidden calls: result=%#v err=%v validates=%d remote=%d", result, err, client.validates, loader.remoteCalls)
	}
	_, result, err = b.Build(context.Background(), Request{Resolve: suppliedInput(false), DryRun: true, ValidateNetwork: true, Attachment: AttachmentInput{RemoteURL: remote}})
	if err != nil || result == nil || client.validates != 0 || loader.remoteCalls != 1 {
		t.Fatalf("network dry-run call matrix mismatch: result=%#v err=%v validates=%d remote=%d", result, err, client.validates, loader.remoteCalls)
	}
	_, _, err = b.Build(context.Background(), Request{Resolve: suppliedInput(false), DryRun: true, ValidateNetwork: true, Attachment: AttachmentInput{RemoteURL: mustURL(t, "https://example.com:8443/image.png")}})
	if err != nil || loader.remoteCalls != 2 {
		t.Fatalf("explicit HTTPS port was rejected before loader policy: calls=%d err=%v", loader.remoteCalls, err)
	}
}

func TestInvalidRemoteURLIsRejectedBeforeResolverReads(t *testing.T) {
	input := suppliedInput(false)
	input.Body = resolver.BodySource{Kind: resolver.BodyFile, Value: "must-not-open", OpenFile: func(string) (io.ReadCloser, error) {
		t.Fatal("resolver read occurred before remote URL validation")
		return nil, nil
	}}
	b := Builder{Resolver: &resolver.Resolver{}, Scope: testScope(t)}
	_, result, err := b.Build(context.Background(), Request{Resolve: input, Attachment: AttachmentInput{RemoteURL: mustURL(t, "http://example.com/image.png")}})
	if err == nil || result != nil {
		t.Fatalf("invalid remote URL was accepted: result=%#v err=%v", result, err)
	}
}

func TestResolverEmbeddedAttachmentIsRejectedBeforeReads(t *testing.T) {
	input := suppliedInput(false)
	input.Message.Attachment = &domain.Attachment{Bytes: []byte("caller supplied")}
	input.Body = resolver.BodySource{Kind: resolver.BodyFile, Value: "must-not-open", OpenFile: func(string) (io.ReadCloser, error) {
		t.Fatal("resolver read occurred before embedded attachment rejection")
		return nil, nil
	}}
	b := Builder{Resolver: &resolver.Resolver{}, Scope: testScope(t)}
	_, result, err := b.Build(context.Background(), Request{Resolve: input})
	if err == nil || result != nil {
		t.Fatalf("embedded attachment was accepted: result=%#v err=%v", result, err)
	}
}

func TestRealValidationOccursBeforeEncryption(t *testing.T) {
	events := []string{}
	client := &fakeClient{events: &events}
	encryptor := &fakeEncryptor{events: &events}
	b := Builder{Resolver: &resolver.Resolver{}, Validator: client, Encryptor: encryptor, Scope: testScope(t)}
	_, _, err := b.Build(context.Background(), Request{Resolve: suppliedInput(true), Validate: true})
	if err == nil || client.validates != 1 || encryptor.calls != 1 || strings.Join(events, ",") != "validate,encrypt" {
		t.Fatalf("expected validation before encryption, events=%v validates=%d encryptions=%d err=%v", events, client.validates, encryptor.calls, err)
	}
}

func TestPreflightFailuresAlwaysReturnZeroPlanAndNilResult(t *testing.T) {
	validationErr := apperror.New("validation_failed", apperror.CategoryValidation, "validation failed")
	encryptionErr := apperror.New("encryption_failed", apperror.CategoryValidation, "encryption failed")
	cases := []struct {
		name      string
		validator error
		encryptor error
	}{
		{name: "validator", validator: validationErr},
		{name: "encryptor", encryptor: encryptionErr},
		{name: "wire"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeClient{validationErr: test.validator}
			encryptor := &fakeEncryptor{err: test.encryptor}
			input := suppliedInput(test.name != "validator")
			b := Builder{Resolver: &resolver.Resolver{}, Validator: client, Encryptor: encryptor, Scope: testScope(t)}
			plan, result, err := b.Build(context.Background(), Request{Resolve: input, Validate: test.validator != nil})
			if err == nil || result != nil || plan.IsValid() {
				t.Fatalf("failure triad mismatch: plan=%#v result=%#v err=%v", plan, result, err)
			}
		})
	}
}

func TestRealPlanSealsAndCopiesMutablePayloadState(t *testing.T) {
	input := suppliedInput(false)
	input.MessageFields.Devices = true
	input.Message.Devices = []string{"phone"}
	timestamp := time.Unix(1700000000, 0)
	input.Message.Timestamp = &timestamp
	loader := &countingLoader{attachment: &domain.Attachment{Name: "attachment.png", MediaType: "image/png", Bytes: []byte("data")}}
	b := Builder{Resolver: &resolver.Resolver{}, Attachments: loader, Scope: testScope(t)}
	plan, result, err := b.Build(context.Background(), Request{Resolve: input, Attachment: AttachmentInput{LocalPath: "fixture"}})
	if err != nil || result != nil || !plan.IsValid() {
		t.Fatalf("real plan rejected: plan=%#v result=%#v err=%v", plan, result, err)
	}
	targets := plan.Targets()
	if len(targets) != 1 || targets[0].Target.E2EEKey != nil || targets[0].Message.Attachment == nil {
		t.Fatalf("unexpected finalized payload: %#v", targets)
	}
	targets[0].Message.Devices[0] = "changed"
	targets[0].Message.Attachment.Bytes[0] = 'x'
	*targets[0].Message.Timestamp = time.Unix(1, 0)
	again := plan.Targets()
	if again[0].Message.Devices[0] != "phone" || again[0].Message.Attachment.Bytes[0] != 'd' || again[0].Message.Timestamp.Unix() != 1700000000 {
		t.Fatal("finalized plan accessor exposed mutable payload state")
	}
}
