package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/attachment"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/delivery"
	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/operation"
	"github.com/jurgenj136/pushover-cli/internal/output"
	"github.com/jurgenj136/pushover-cli/internal/preflight"
	"github.com/jurgenj136/pushover-cli/internal/pushover"
	"github.com/jurgenj136/pushover-cli/internal/resolver"
	"github.com/jurgenj136/pushover-cli/internal/secrets"
)

func TestExecuteSendDryRunWithFakes(t *testing.T) {
	ctx := context.Background()
	store := secrets.NewMemoryStore()
	tokenRef, err := store.Put(ctx, []byte(strings.Repeat("t", domain.IdentifierLength)))
	if err != nil {
		t.Fatal(err)
	}
	userRef, err := store.Put(ctx, []byte(strings.Repeat("u", domain.IdentifierLength)))
	if err != nil {
		t.Fatal(err)
	}
	configuration := config.Config{
		SchemaVersion:  config.CurrentSchemaVersion,
		Backend:        config.Backend{Type: "native"},
		DefaultProfile: "primary",
		Profiles: map[string]config.Profile{
			"primary": {Name: "primary", TokenRef: tokenRef},
		},
		Recipients: map[string]config.Recipient{
			"alice": {Alias: "alice", UserKeyRef: userRef, Validated: true},
		},
	}
	client := &recordingClient{}
	dependencies := Dependencies{
		Preflight: &preflight.Builder{Resolver: &resolver.Resolver{Config: staticConfigStore{value: configuration}, Secrets: store}},
		Delivery:  &delivery.Runner{Client: client},
		Output:    output.NewFactory(),
		lookupEnv: func(string) (string, bool) { return "", false },
	}
	var stdout, stderr bytes.Buffer
	exit := Execute(ctx, []string{"--json", "send", "--to", "alice", "--dry-run", "body"}, testIO(&stdout, &stderr), dependencies)

	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr = %q", exit, stderr.String())
	}
	if got, want := stdout.String(), golden(t, "send-dry-run.json.golden"); got != want {
		t.Fatalf("stdout mismatch (-want +got):\nwant: %s\ngot:  %s", want, got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if client.sendCalls != 0 || client.validationCalls != 0 {
		t.Fatalf("dry run reached Pushover client: sends=%d validations=%d", client.sendCalls, client.validationCalls)
	}
}

func TestExecuteRejectsStructuralInputConflictsBeforeReaders(t *testing.T) {
	tests := []struct {
		name string
		args []string
		deps Dependencies
	}{
		{
			name: "multiple body sources",
			args: []string{"send", "body", "--stdin", "--file", "must-not-open"},
		},
		{
			name: "credentials stdin conflicts with body stdin",
			args: []string{"send", "--stdin", "--credentials-stdin"},
		},
		{
			name: "attachment sources conflict",
			args: []string{"send", "body", "--attachment", "must-not-open", "--attachment-url", "https://example.com/image.png"},
		},
		{
			name: "partial environment credentials",
			args: []string{"send", "body", "--to", "alice"},
			deps: Dependencies{lookupEnv: func(name string) (string, bool) {
				return "only-one-secret", name == "PUSHOVER_TOKEN"
			}},
		},
		{
			name: "environment credentials conflict with aliases",
			args: []string{"send", "body", "--to", "alice"},
			deps: Dependencies{lookupEnv: func(string) (string, bool) {
				return strings.Repeat("x", domain.IdentifierLength), true
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			input := &readMustNotHappen{}
			exit := Execute(context.Background(), test.args, IO{In: input, Out: &stdout, Err: &stderr, IsTerminal: func(uintptr) bool { return false }}, test.deps)
			if exit != 2 {
				t.Fatalf("exit = %d, want usage; stderr=%q", exit, stderr.String())
			}
			if input.reads != 0 {
				t.Fatalf("structural conflict consumed stdin %d times", input.reads)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if strings.Contains(stderr.String(), "only-one-secret") {
				t.Fatalf("stderr leaked credential: %q", stderr.String())
			}
		})
	}
}

func TestExecutePreservesExplicitZeroAndEmptyFieldPresence(t *testing.T) {
	ctx := context.Background()
	store := secrets.NewMemoryStore()
	tokenRef, err := store.Put(ctx, []byte(strings.Repeat("t", domain.IdentifierLength)))
	if err != nil {
		t.Fatal(err)
	}
	userRef, err := store.Put(ctx, []byte(strings.Repeat("u", domain.IdentifierLength)))
	if err != nil {
		t.Fatal(err)
	}
	client := &recordingClient{}
	deps := Dependencies{
		Preflight: &preflight.Builder{Resolver: &resolver.Resolver{
			Config: staticConfigStore{value: config.Config{
				SchemaVersion:  config.CurrentSchemaVersion,
				Backend:        config.Backend{Type: "native"},
				DefaultProfile: "primary",
				Profiles: map[string]config.Profile{
					"primary": {Name: "primary", TokenRef: tokenRef, Defaults: config.MessageDefaults{Priority: -1, Title: "saved title"}},
				},
				Recipients: map[string]config.Recipient{"alice": {Alias: "alice", UserKeyRef: userRef, Validated: true}},
			}},
			Secrets: store,
		}},
		Delivery:  &delivery.Runner{Client: client},
		Output:    output.NewFactory(),
		lookupEnv: func(string) (string, bool) { return "", false },
	}
	var stdout, stderr bytes.Buffer
	exit := Execute(ctx, []string{"send", "--to", "alice", "--priority=0", "--title=", "body"}, testIO(&stdout, &stderr), deps)
	if exit != 0 {
		t.Fatalf("exit = %d; stderr=%q", exit, stderr.String())
	}
	if len(client.payloads) != 1 {
		t.Fatalf("send payloads = %d, want 1", len(client.payloads))
	}
	if got := client.payloads[0].Message.Priority; got != 0 {
		t.Fatalf("priority = %d, want explicit 0", got)
	}
	if got := client.payloads[0].Message.Title; got != "" {
		t.Fatalf("title = %q, want explicit empty override", got)
	}
}

func TestExecuteOutputModesAndTimeoutContracts(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantExit  int
		wantJSON  bool
		wantPlain bool
	}{
		{name: "json parse success usage error", args: []string{"--json", "send"}, wantExit: 2, wantJSON: true},
		{name: "plain parse success usage error", args: []string{"--plain", "send"}, wantExit: 2, wantPlain: true},
		{name: "false json is not selected", args: []string{"--json=false", "--plain", "send"}, wantExit: 2, wantPlain: true},
		{name: "both selected conflict", args: []string{"--json", "--plain", "send", "body"}, wantExit: 2, wantJSON: true},
		{name: "zero timeout is invalid", args: []string{"--json", "--timeout", "0s", "send", "body"}, wantExit: 2, wantJSON: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := Execute(context.Background(), test.args, testIO(&stdout, &stderr), Dependencies{})
			if got != test.wantExit {
				t.Fatalf("exit = %d, want %d; stderr=%q", got, test.wantExit, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if test.wantJSON && !strings.HasPrefix(stderr.String(), `{"schema_version":1`) {
				t.Fatalf("stderr = %q, want JSON error", stderr.String())
			}
			if test.wantPlain && (!strings.HasPrefix(stderr.String(), "error:") || strings.HasPrefix(stderr.String(), "{")) {
				t.Fatalf("stderr = %q, want plain diagnostic", stderr.String())
			}
		})
	}
}

func TestExecuteEnvironmentPresenceUsesLookupEnvSetBits(t *testing.T) {
	t.Run("complete environment bypasses configured stores", func(t *testing.T) {
		client := &recordingClient{}
		deps := Dependencies{
			Preflight: &preflight.Builder{Resolver: &resolver.Resolver{}},
			Delivery:  &delivery.Runner{Client: client},
			Output:    output.NewFactory(),
			lookupEnv: func(name string) (string, bool) {
				if name == "PUSHOVER_TOKEN" {
					return strings.Repeat("t", domain.IdentifierLength), true
				}
				return strings.Repeat("u", domain.IdentifierLength), true
			},
		}
		var stdout, stderr bytes.Buffer
		if got := Execute(context.Background(), []string{"send", "body"}, testIO(&stdout, &stderr), deps); got != 0 {
			t.Fatalf("exit = %d; stderr=%q", got, stderr.String())
		}
		if len(client.payloads) != 1 || client.payloads[0].Target.Alias != "environment" {
			t.Fatalf("environment context was not selected: %#v", client.payloads)
		}
	})

	t.Run("set empty values are not absent", func(t *testing.T) {
		deps := Dependencies{
			Preflight: &preflight.Builder{Resolver: &resolver.Resolver{}},
			Delivery:  &delivery.Runner{Client: &recordingClient{}},
			lookupEnv: func(string) (string, bool) { return "", true },
		}
		var stdout, stderr bytes.Buffer
		if got := Execute(context.Background(), []string{"send", "body"}, testIO(&stdout, &stderr), deps); got != 4 {
			t.Fatalf("exit = %d, want validation; stderr=%q", got, stderr.String())
		}
		if strings.Contains(stderr.String(), "configuration is unavailable") {
			t.Fatalf("empty environment was treated as absent: %q", stderr.String())
		}
	})
}

func TestExecuteBodyIsBoundedAndRemoteDryRunNetworkIsOptIn(t *testing.T) {
	t.Run("stdin body is bounded before delivery", func(t *testing.T) {
		deps, client := configuredDependencies(t, config.MessageDefaults{}, nil)
		var stdout, stderr bytes.Buffer
		input := strings.NewReader(strings.Repeat("x", int(resolver.MaxBodyBytes)+1))
		io := IO{In: input, Out: &stdout, Err: &stderr, IsTerminal: func(uintptr) bool { return false }}
		if got := Execute(context.Background(), []string{"send", "--stdin", "--to", "alice"}, io, deps); got != 4 {
			t.Fatalf("exit = %d, want validation; stderr=%q", got, stderr.String())
		}
		if client.sendCalls != 0 {
			t.Fatalf("oversized body sent %d messages", client.sendCalls)
		}
	})

	t.Run("remote attachment fetch is opt in during dry run", func(t *testing.T) {
		loader := &recordingAttachmentLoader{attachment: &domain.Attachment{Name: "image.png", MediaType: "image/png", Bytes: []byte("image")}}
		deps, _ := configuredDependencies(t, config.MessageDefaults{}, loader)
		for _, test := range []struct {
			name      string
			validate  bool
			wantFetch int
		}{
			{name: "offline", wantFetch: 0},
			{name: "online", validate: true, wantFetch: 1},
		} {
			t.Run(test.name, func(t *testing.T) {
				loader.remoteCalls = 0
				args := []string{"send", "body", "--to", "alice", "--dry-run", "--attachment-url", "https://example.com/image.png"}
				if test.validate {
					args = append(args, "--validate-network")
				}
				var stdout, stderr bytes.Buffer
				if got := Execute(context.Background(), args, testIO(&stdout, &stderr), deps); got != 0 {
					t.Fatalf("exit = %d; stderr=%q", got, stderr.String())
				}
				if loader.remoteCalls != test.wantFetch {
					t.Fatalf("remote fetches = %d, want %d", loader.remoteCalls, test.wantFetch)
				}
			})
		}
	})
}

func TestExecuteParsesFullSendFieldsAndUsesOneTotalScope(t *testing.T) {
	deps, client := configuredDependencies(t, config.MessageDefaults{}, nil)
	contexts := &recordingContexts{}
	deps.Operations = operation.Factory{Clock: fixedClock{now: time.Now()}, Contexts: contexts}
	var stdout, stderr bytes.Buffer
	args := []string{
		"send", "body", "--to", "alice", "--title", "title", "--device", "one", "--device", "two", "--priority", "1",
		"--sound", "magic", "--timestamp", "123", "--ttl", "2m", "--url", "https://example.com/a", "--url-title", "open", "--html", "--validate",
	}
	if got := Execute(context.Background(), args, testIO(&stdout, &stderr), deps); got != 0 {
		t.Fatalf("exit = %d; stderr=%q", got, stderr.String())
	}
	if len(client.payloads) != 1 {
		t.Fatalf("payloads = %#v", client.payloads)
	}
	message := client.payloads[0].Message
	if message.Title != "title" || strings.Join(message.Devices, ",") != "one,two" || message.Priority != 1 || message.Sound != "magic" || message.TTL != 2*time.Minute || message.URL != "https://example.com/a" || message.URLTitle != "open" || !message.HTML {
		t.Fatalf("send flags were not translated: %#v", message)
	}
	if message.Timestamp == nil || !message.Timestamp.Equal(time.Unix(123, 0).UTC()) {
		t.Fatalf("timestamp = %#v, want Unix timestamp", message.Timestamp)
	}
	if len(contexts.created) < 3 || contexts.parents[1] != contexts.created[0] || contexts.parents[2] != contexts.created[0] {
		t.Fatalf("preflight and delivery did not share command scope: parents=%#v created=%#v", contexts.parents, contexts.created)
	}
}

func TestExecuteEmergencyNeedsConsentAndDoesNotSend(t *testing.T) {
	deps, client := configuredDependencies(t, config.MessageDefaults{}, nil)
	var stdout, stderr bytes.Buffer
	args := []string{"send", "body", "--to", "alice", "--priority", "2", "--retry", "30s", "--expire", "1m"}
	if got := Execute(context.Background(), args, testIO(&stdout, &stderr), deps); got != 8 {
		t.Fatalf("exit = %d, want confirmation; stderr=%q", got, stderr.String())
	}
	if client.sendCalls != 0 {
		t.Fatalf("unconfirmed emergency sent %d times", client.sendCalls)
	}
}

func TestExecuteEmergencyTTYConfirmationAndCancellationRendering(t *testing.T) {
	t.Run("tty confirmation is wired when runner has none", func(t *testing.T) {
		deps, client := configuredDependencies(t, config.MessageDefaults{}, nil)
		var stdout, stderr bytes.Buffer
		io := IO{In: strings.NewReader("yes\n"), Out: &stdout, Err: &stderr, IsTerminal: func(uintptr) bool { return true }}
		args := []string{"send", "body", "--to", "alice", "--priority", "2", "--retry", "30s", "--expire", "1m"}
		if got := Execute(context.Background(), args, io, deps); got != 0 {
			t.Fatalf("exit = %d; stderr=%q", got, stderr.String())
		}
		if client.sendCalls != 1 || !strings.Contains(stderr.String(), "Emergency notification") {
			t.Fatalf("confirmation did not safely proceed: sends=%d stderr=%q", client.sendCalls, stderr.String())
		}
	})

	t.Run("preflight cancellation is a safe network timeout", func(t *testing.T) {
		deps, client := configuredDependencies(t, config.MessageDefaults{}, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var stdout, stderr bytes.Buffer
		if got := Execute(ctx, []string{"send", "body", "--to", "alice"}, testIO(&stdout, &stderr), deps); got != 6 {
			t.Fatalf("exit = %d, want network timeout; stderr=%q", got, stderr.String())
		}
		if client.sendCalls != 0 || strings.Contains(stderr.String(), "context canceled") {
			t.Fatalf("cancellation leaked or sent: sends=%d stderr=%q", client.sendCalls, stderr.String())
		}
	})
}

func TestExecuteBuffersRendererFailureAndUsesErrorStreamTerminal(t *testing.T) {
	t.Run("renderer failure leaves stdout clean and fails", func(t *testing.T) {
		deps, _ := configuredDependencies(t, config.MessageDefaults{}, nil)
		deps.Output = &recordingFactory{renderer: failingRenderer{}}
		var stdout, stderr bytes.Buffer
		if got := Execute(context.Background(), []string{"send", "body", "--to", "alice", "--dry-run"}, testIO(&stdout, &stderr), deps); got == 0 {
			t.Fatalf("renderer failure returned success; stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
		if stdout.Len() != 0 {
			t.Fatalf("renderer failure committed partial stdout: %q", stdout.String())
		}
	})

	t.Run("errors use the error stream terminal", func(t *testing.T) {
		factory := &recordingFactory{renderer: noOpRenderer{}}
		deps := Dependencies{Output: factory, lookupEnv: func(string) (string, bool) { return "", false }}
		out, errOut := &fdBuffer{fd: 1}, &fdBuffer{fd: 2}
		io := IO{In: strings.NewReader(""), Out: out, Err: errOut, IsTerminal: func(fd uintptr) bool { return fd == 2 }}
		if got := Execute(context.Background(), []string{"send"}, io, deps); got != 2 {
			t.Fatalf("exit = %d", got)
		}
		if len(factory.options) != 1 || !factory.options[0].TTY || !factory.options[0].Color {
			t.Fatalf("error renderer options = %#v, want stderr terminal", factory.options)
		}
	})

	t.Run("NO_COLOR disables color through injected environment", func(t *testing.T) {
		factory := &recordingFactory{renderer: noOpRenderer{}}
		deps := Dependencies{Output: factory, lookupEnv: func(string) (string, bool) { return "1", true }}
		out, errOut := &fdBuffer{fd: 1}, &fdBuffer{fd: 2}
		io := IO{In: strings.NewReader(""), Out: out, Err: errOut, IsTerminal: func(uintptr) bool { return true }}
		if got := Execute(context.Background(), []string{"send"}, io, deps); got != 2 {
			t.Fatalf("exit = %d", got)
		}
		if len(factory.options) != 1 || factory.options[0].Color {
			t.Fatalf("NO_COLOR renderer options = %#v", factory.options)
		}
	})
}

func TestExecuteFirstInterruptCancelsBlockingConfirmation(t *testing.T) {
	deps, client := configuredDependencies(t, config.MessageDefaults{}, nil)
	signals := make(chan os.Signal, 2)
	deps.Signals = signals
	input := &blockingInput{started: make(chan struct{})}
	var stdout, stderr bytes.Buffer
	streams := IO{In: input, Out: &stdout, Err: &stderr, IsTerminal: func(uintptr) bool { return true }}
	result := make(chan int, 1)
	go func() {
		result <- Execute(context.Background(), []string{"send", "body", "--to", "alice", "--priority", "2", "--retry", "30s", "--expire", "1m"}, streams, deps)
	}()
	select {
	case <-input.started:
	case <-time.After(time.Second):
		t.Fatal("confirmation did not start")
	}
	signals <- os.Interrupt
	select {
	case exit := <-result:
		if exit != 130 {
			t.Fatalf("exit = %d, want interrupted", exit)
		}
	case <-time.After(time.Second):
		t.Fatal("first interrupt did not cancel confirmation")
	}
	if client.sendCalls != 0 {
		t.Fatalf("interrupted confirmation sent %d messages", client.sendCalls)
	}
}

func testIO(stdout, stderr io.Writer) IO {
	return IO{In: strings.NewReader(""), Out: stdout, Err: stderr, IsTerminal: func(uintptr) bool { return false }}
}

func golden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

type staticConfigStore struct{ value config.Config }

func (s staticConfigStore) Load(context.Context) (config.Config, error) { return s.value, nil }
func (s staticConfigStore) Commit(context.Context, config.Config) error {
	return errors.New("unexpected config write")
}
func (s staticConfigStore) Path() string { return "memory" }

type recordingClient struct {
	sendCalls, validationCalls int
	payloads                   []domain.TargetPayload
}

func (c *recordingClient) Send(_ context.Context, payload domain.TargetPayload) (pushover.SendResponse, error) {
	c.sendCalls++
	c.payloads = append(c.payloads, payload)
	return pushover.SendResponse{}, nil
}
func (c *recordingClient) ValidateRecipient(context.Context, string, string, []string) (pushover.RecipientValidation, error) {
	c.validationCalls++
	return pushover.RecipientValidation{Valid: true}, nil
}
func (*recordingClient) Sounds(context.Context, string) (pushover.SoundsResponse, error) {
	return pushover.SoundsResponse{}, nil
}
func (*recordingClient) Limits(context.Context, string) (pushover.LimitsResponse, error) {
	return pushover.LimitsResponse{}, nil
}

type readMustNotHappen struct{ reads int }

func (r *readMustNotHappen) Read([]byte) (int, error) {
	r.reads++
	return 0, errors.New("stdin must not be read")
}

type blockingInput struct {
	started chan struct{}
}

func (r *blockingInput) Read([]byte) (int, error) {
	select {
	case <-r.started:
	default:
		close(r.started)
	}
	select {}
}

func configuredDependencies(t *testing.T, defaults config.MessageDefaults, loader attachment.Loader) (Dependencies, *recordingClient) {
	t.Helper()
	ctx := context.Background()
	store := secrets.NewMemoryStore()
	tokenRef, err := store.Put(ctx, []byte(strings.Repeat("t", domain.IdentifierLength)))
	if err != nil {
		t.Fatal(err)
	}
	userRef, err := store.Put(ctx, []byte(strings.Repeat("u", domain.IdentifierLength)))
	if err != nil {
		t.Fatal(err)
	}
	client := &recordingClient{}
	return Dependencies{
		Preflight: &preflight.Builder{Resolver: &resolver.Resolver{Config: staticConfigStore{value: config.Config{
			SchemaVersion:  config.CurrentSchemaVersion,
			Backend:        config.Backend{Type: "native"},
			DefaultProfile: "primary",
			Profiles:       map[string]config.Profile{"primary": {Name: "primary", TokenRef: tokenRef, Defaults: defaults}},
			Recipients:     map[string]config.Recipient{"alice": {Alias: "alice", UserKeyRef: userRef, Validated: true}},
		}}, Secrets: store}, Attachments: loader, Validator: client},
		Delivery:  &delivery.Runner{Client: client},
		Output:    output.NewFactory(),
		lookupEnv: func(string) (string, bool) { return "", false },
	}, client
}

type recordingAttachmentLoader struct {
	localCalls, remoteCalls int
	attachment              *domain.Attachment
}

func (r *recordingAttachmentLoader) Local(context.Context, string) (*domain.Attachment, error) {
	r.localCalls++
	return r.attachment, nil
}
func (r *recordingAttachmentLoader) Remote(context.Context, *url.URL) (*domain.Attachment, error) {
	r.remoteCalls++
	return r.attachment, nil
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type recordingContexts struct {
	parents []context.Context
	created []context.Context
}

func (c *recordingContexts) WithDeadline(parent context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithDeadline(parent, deadline)
	c.parents = append(c.parents, parent)
	c.created = append(c.created, ctx)
	return ctx, cancel
}

type fdBuffer struct {
	bytes.Buffer
	fd uintptr
}

func (b *fdBuffer) Fd() uintptr { return b.fd }

type recordingFactory struct {
	renderer output.Renderer
	options  []output.RenderOptions
}

func (f *recordingFactory) New(_ output.Mode, options output.RenderOptions) (output.Renderer, error) {
	f.options = append(f.options, options)
	return f.renderer, nil
}

type failingRenderer struct{}

func (failingRenderer) Result(w io.Writer, _ output.CommandResult) error {
	_, _ = io.WriteString(w, "partial output")
	return errors.New("renderer failed")
}
func (failingRenderer) Error(io.Writer, string, *apperror.Error) error { return nil }

type noOpRenderer struct{}

func (noOpRenderer) Result(io.Writer, output.CommandResult) error   { return nil }
func (noOpRenderer) Error(io.Writer, string, *apperror.Error) error { return nil }
