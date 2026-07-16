package delivery

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/operation"
	"github.com/jurgenj136/pushover-cli/internal/preflight"
	"github.com/jurgenj136/pushover-cli/internal/pushover"
	"github.com/jurgenj136/pushover-cli/internal/resolver"
	"github.com/jurgenj136/pushover-cli/internal/secrets"
)

func TestRunRejectsInvalidPlanBeforeConfirmationOrSend(t *testing.T) {
	client := &scriptedClient{}
	confirmer := &recordingConfirmer{}
	runner, closeScope := newRunner(t, client, confirmer, nil)
	defer closeScope()

	batch, err := runner.Run(context.Background(), preflight.FinalizedSendPlan{}, RunOptions{AssumeYes: true})
	if err == nil || len(batch.Results) != 0 {
		t.Fatalf("batch=%#v err=%v", batch, err)
	}
	if got := client.CallCount(); got != 0 {
		t.Fatalf("Send calls = %d, want 0", got)
	}
	if got := confirmer.CallCount(); got != 0 {
		t.Fatalf("confirmation calls = %d, want 0", got)
	}
}

func TestRunPreCanceledCallerReturnsOrderedDefiniteBatch(t *testing.T) {
	tests := []struct {
		name       string
		targets    int
		wantExit   int
		wantResult []domain.TargetOutcome
	}{
		{name: "one target exits definite network", targets: 1, wantExit: apperror.ExitNetwork, wantResult: []domain.TargetOutcome{domain.Failed}},
		{name: "multi target exits partial", targets: 2, wantExit: apperror.ExitPartial, wantResult: []domain.TargetOutcome{domain.Failed, domain.Skipped}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			client := &scriptedClient{}
			confirmer := &recordingConfirmer{}
			runner, closeScope := newRunner(t, client, confirmer, nil)
			defer closeScope()

			batch, err := runner.Run(ctx, finalizedPlan(t, test.targets, domain.PriorityNormal), RunOptions{})
			if err != nil {
				t.Fatalf("command error = %v, want ordered batch", err)
			}
			if len(batch.Results) != len(test.wantResult) {
				t.Fatalf("results = %#v", batch.Results)
			}
			for index, want := range test.wantResult {
				if batch.Results[index].Outcome != want {
					t.Fatalf("result[%d] = %s, want %s", index, batch.Results[index].Outcome, want)
				}
			}
			if got := apperror.Exit(nil, &batch); got != test.wantExit {
				t.Fatalf("exit = %d, want %d", got, test.wantExit)
			}
			if client.CallCount() != 0 || confirmer.CallCount() != 0 {
				t.Fatalf("pre-canceled run mutated: sends=%d confirms=%d", client.CallCount(), confirmer.CallCount())
			}
		})
	}
}

func TestRunConfirmationStateMatrix(t *testing.T) {
	tests := []struct {
		name         string
		priority     int
		assumeYes    bool
		confirmation error
		wantConfirms int
		wantSends    int
		wantRunError bool
	}{
		{name: "ordinary never confirms", priority: domain.PriorityNormal, wantSends: 1},
		{name: "emergency confirms immediately before mutation", priority: domain.PriorityEmergency, wantConfirms: 1, wantSends: 1},
		{name: "declined emergency never sends", priority: domain.PriorityEmergency, confirmation: errors.New("declined"), wantConfirms: 1, wantRunError: true},
		{name: "yes bypasses only emergency confirmation", priority: domain.PriorityEmergency, assumeYes: true, wantSends: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := finalizedPlan(t, 1, test.priority)
			client := &scriptedClient{}
			confirmer := &recordingConfirmer{err: test.confirmation}
			runner, closeScope := newRunner(t, client, confirmer, nil)
			defer closeScope()

			_, err := runner.Run(context.Background(), plan, RunOptions{AssumeYes: test.assumeYes})
			if (err != nil) != test.wantRunError {
				t.Fatalf("err=%v, want error=%v", err, test.wantRunError)
			}
			if got := confirmer.CallCount(); got != test.wantConfirms {
				t.Fatalf("confirmation calls = %d, want %d", got, test.wantConfirms)
			}
			if got := client.CallCount(); got != test.wantSends {
				t.Fatalf("Send calls = %d, want %d", got, test.wantSends)
			}
		})
	}
}

func TestRunSendsInInputOrderOnceAndContinuesAfterDefiniteFailure(t *testing.T) {
	plan := finalizedPlan(t, 2, domain.PriorityNormal)
	quota := 17
	client := &scriptedClient{send: func(_ context.Context, _ domain.TargetPayload, call int) (pushover.SendResponse, error) {
		switch call {
		case 0:
			return pushover.SendResponse{RequestID: "definite", QuotaRemaining: &quota}, &pushover.SendError{
				Cause:                apperror.New("network_failure", apperror.CategoryNetwork, "Pushover request failed"),
				Phase:                pushover.PhaseConnect,
				TransmissionPossible: false,
			}
		case 1:
			return pushover.SendResponse{RequestID: "sent", ReceiptID: strings.Repeat("r", 30), QuotaRemaining: &quota}, nil
		default:
			t.Fatalf("unexpected retry/call %d", call)
			return pushover.SendResponse{}, nil
		}
	}}
	runner, closeScope := newRunner(t, client, nil, nil)
	defer closeScope()

	batch, err := runner.Run(context.Background(), plan, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := client.Aliases(); strings.Join(got, ",") != "one,two" {
		t.Fatalf("send aliases = %v", got)
	}
	if len(batch.Results) != 2 || batch.Results[0].Outcome != domain.Failed || batch.Results[1].Outcome != domain.Sent {
		t.Fatalf("results = %#v", batch.Results)
	}
	if batch.Results[0].RequestID != "definite" || batch.Results[0].QuotaRemaining == nil || *batch.Results[0].QuotaRemaining != quota {
		t.Fatalf("failed response metadata was lost: %#v", batch.Results[0])
	}
	if batch.Results[1].RequestID != "sent" || batch.Results[1].ReceiptID != strings.Repeat("r", 30) {
		t.Fatalf("successful response metadata was lost: %#v", batch.Results[1])
	}
	if got := apperror.Exit(nil, &batch); got != apperror.ExitPartial {
		t.Fatalf("exit = %d, want partial", got)
	}
}

func TestRunClassifiesOnlyTypedTransmissionPossibility(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		outcome domain.TargetOutcome
		exit    int
	}{
		{
			name:    "typed definite failure",
			err:     &pushover.SendError{Cause: apperror.New("network_failure", apperror.CategoryNetwork, "Pushover request failed"), TransmissionPossible: false},
			outcome: domain.Failed,
			exit:    apperror.ExitNetwork,
		},
		{
			name:    "typed possible transmission",
			err:     &pushover.SendError{Cause: errors.New("hidden"), TransmissionPossible: true},
			outcome: domain.Ambiguous,
			exit:    apperror.ExitAmbiguous,
		},
		{
			name:    "unknown errors default ambiguous",
			err:     errors.New("untyped transport failure"),
			outcome: domain.Ambiguous,
			exit:    apperror.ExitAmbiguous,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &scriptedClient{send: func(context.Context, domain.TargetPayload, int) (pushover.SendResponse, error) {
				return pushover.SendResponse{}, test.err
			}}
			runner, closeScope := newRunner(t, client, nil, nil)
			defer closeScope()
			batch, err := runner.Run(context.Background(), finalizedPlan(t, 1, domain.PriorityNormal), RunOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(batch.Results) != 1 || batch.Results[0].Outcome != test.outcome {
				t.Fatalf("results = %#v", batch.Results)
			}
			if got := apperror.Exit(nil, &batch); got != test.exit {
				t.Fatalf("exit = %d, want %d", got, test.exit)
			}
		})
	}
}

func TestRunFirstInterruptStopsBeforeAndBetweenTargets(t *testing.T) {
	t.Run("before first target", func(t *testing.T) {
		signals := make(chan os.Signal, 1)
		signals <- os.Interrupt
		client := &scriptedClient{}
		runner, closeScope := newRunner(t, client, nil, signals)
		defer closeScope()

		batch, err := runner.Run(context.Background(), finalizedPlan(t, 2, domain.PriorityNormal), RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		assertInterruptedSkipped(t, batch, 0, 2)
	})
	t.Run("between targets", func(t *testing.T) {
		signals := make(chan os.Signal, 1)
		client := &scriptedClient{send: func(_ context.Context, _ domain.TargetPayload, call int) (pushover.SendResponse, error) {
			if call == 0 {
				signals <- os.Interrupt
			}
			return pushover.SendResponse{RequestID: "sent"}, nil
		}}
		runner, closeScope := newRunner(t, client, nil, signals)
		defer closeScope()

		batch, err := runner.Run(context.Background(), finalizedPlan(t, 2, domain.PriorityNormal), RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		assertInterruptedSkipped(t, batch, 1, 1)
	})
}

func TestRunLinearizesSignalAndCallerCancellationBeforeLaunch(t *testing.T) {
	tests := []struct {
		name      string
		interrupt func(chan<- os.Signal, context.CancelFunc)
		wantInt   bool
	}{
		{name: "queued signal in pre-launch window", interrupt: func(signals chan<- os.Signal, _ context.CancelFunc) { signals <- os.Interrupt }, wantInt: true},
		{name: "caller cancellation in pre-launch window", interrupt: func(_ chan<- os.Signal, cancel context.CancelFunc) { cancel() }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signals := make(chan os.Signal, 1)
			caller, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := &scriptedClient{}
			runner, closeScope := newRunner(t, client, nil, signals)
			defer closeScope()
			atGate := make(chan struct{})
			release := make(chan struct{})
			runner.beforeSchedule = func() {
				close(atGate)
				<-release
			}
			plan := finalizedPlan(t, 2, domain.PriorityNormal)
			result := make(chan domain.BatchResult, 1)
			errs := make(chan error, 1)
			go func() {
				batch, err := runner.Run(caller, plan, RunOptions{})
				result <- batch
				errs <- err
			}()
			<-atGate
			test.interrupt(signals, cancel)
			close(release)
			batch := <-result
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
			if got := client.CallCount(); got != 0 {
				t.Fatalf("Send calls = %d, want 0", got)
			}
			if len(batch.Results) != 2 {
				t.Fatalf("results = %#v", batch.Results)
			}
			if test.wantInt {
				assertInterruptedSkipped(t, batch, 0, 2)
			}
		})
	}
}

func TestRunFirstSignalCancelsBlockingConfirmationAndSkipsAll(t *testing.T) {
	signals := make(chan os.Signal, 1)
	confirmer := &blockingConfirmer{started: make(chan struct{})}
	client := &scriptedClient{}
	runner, closeScope := newRunner(t, client, confirmer, signals)
	defer closeScope()
	plan := finalizedPlan(t, 2, domain.PriorityEmergency)
	result := make(chan domain.BatchResult, 1)
	errs := make(chan error, 1)
	go func() {
		batch, err := runner.Run(context.Background(), plan, RunOptions{})
		result <- batch
		errs <- err
	}()
	<-confirmer.started
	signals <- os.Interrupt
	select {
	case batch := <-result:
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		assertInterruptedSkipped(t, batch, 0, 2)
		if got := apperror.Exit(nil, &batch); got != apperror.ExitInterrupted {
			t.Fatalf("exit = %d, want interrupted", got)
		}
	case <-time.After(time.Second):
		t.Fatal("confirmation did not unblock after first signal")
	}
	if got := client.CallCount(); got != 0 {
		t.Fatalf("Send calls = %d, want 0", got)
	}
}

func TestRunCancellationDuringConfirmationReturnsOrderedDefiniteBatch(t *testing.T) {
	tests := []struct {
		name   string
		cancel func(context.CancelFunc, *Runner)
	}{
		{name: "caller", cancel: func(cancel context.CancelFunc, _ *Runner) { cancel() }},
		{name: "scope", cancel: func(_ context.CancelFunc, runner *Runner) { runner.Scope.Close() }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			caller, cancel := context.WithCancel(context.Background())
			defer cancel()
			confirmer := &blockingConfirmer{started: make(chan struct{})}
			client := &scriptedClient{}
			runner, closeScope := newRunner(t, client, confirmer, nil)
			defer closeScope()
			plan := finalizedPlan(t, 2, domain.PriorityEmergency)
			result := make(chan domain.BatchResult, 1)
			errs := make(chan error, 1)
			go func() {
				batch, err := runner.Run(caller, plan, RunOptions{})
				result <- batch
				errs <- err
			}()
			<-confirmer.started
			test.cancel(cancel, runner)
			batch := <-result
			if err := <-errs; err != nil {
				t.Fatalf("command error = %v, want ordered batch", err)
			}
			if batch.Interrupted || len(batch.Results) != 2 || batch.Results[0].Outcome != domain.Failed || batch.Results[1].Outcome != domain.Skipped {
				t.Fatalf("batch = %#v", batch)
			}
			if got := client.CallCount(); got != 0 {
				t.Fatalf("Send calls = %d, want 0", got)
			}
		})
	}
}

func TestRunInterruptsActiveRequestOnlyOnSecondSignal(t *testing.T) {
	t.Run("first signal lets current request finish", func(t *testing.T) {
		signals := make(chan os.Signal, 2)
		started, release := make(chan struct{}), make(chan struct{})
		client := &scriptedClient{send: func(ctx context.Context, _ domain.TargetPayload, call int) (pushover.SendResponse, error) {
			if call != 0 {
				t.Fatalf("unexpected scheduled target %d", call)
			}
			close(started)
			select {
			case <-release:
				return pushover.SendResponse{RequestID: "sent"}, nil
			case <-ctx.Done():
				return pushover.SendResponse{}, &pushover.SendError{Cause: ctx.Err(), Phase: pushover.PhaseWrite, TransmissionPossible: true}
			}
		}}
		runner, closeScope := newRunner(t, client, nil, signals)
		defer closeScope()
		plan := finalizedPlan(t, 2, domain.PriorityNormal)
		result := make(chan domain.BatchResult, 1)
		errs := make(chan error, 1)
		go func() {
			batch, err := runner.Run(context.Background(), plan, RunOptions{})
			result <- batch
			errs <- err
		}()
		<-started
		signals <- os.Interrupt
		close(release)
		batch := <-result
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		assertInterruptedSkipped(t, batch, 1, 1)
		if batch.Results[0].Outcome != domain.Sent {
			t.Fatalf("active outcome = %s, want sent", batch.Results[0].Outcome)
		}
	})

	t.Run("second signal cancels active request and preserves ambiguity", func(t *testing.T) {
		signals := make(chan os.Signal, 2)
		started := make(chan struct{})
		client := &scriptedClient{send: func(ctx context.Context, _ domain.TargetPayload, call int) (pushover.SendResponse, error) {
			if call != 0 {
				t.Fatalf("unexpected scheduled target %d", call)
			}
			close(started)
			<-ctx.Done()
			return pushover.SendResponse{RequestID: "possibly-sent"}, &pushover.SendError{Cause: ctx.Err(), Phase: pushover.PhaseWrite, TransmissionPossible: true}
		}}
		runner, closeScope := newRunner(t, client, nil, signals)
		defer closeScope()
		plan := finalizedPlan(t, 2, domain.PriorityNormal)
		result := make(chan domain.BatchResult, 1)
		errs := make(chan error, 1)
		go func() {
			batch, err := runner.Run(context.Background(), plan, RunOptions{})
			result <- batch
			errs <- err
		}()
		<-started
		signals <- os.Interrupt
		signals <- os.Interrupt
		batch := <-result
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if !batch.Interrupted || len(batch.Results) != 2 || batch.Results[0].Outcome != domain.Ambiguous || batch.Results[1].Outcome != domain.Skipped {
			t.Fatalf("results = %#v", batch)
		}
		if got := apperror.Exit(nil, &batch); got != apperror.ExitAmbiguous {
			t.Fatalf("exit = %d, want ambiguity", got)
		}
	})
}

func TestRunUsesScopedSendPhaseAndCallerCancellation(t *testing.T) {
	caller, cancelCaller := context.WithCancel(context.Background())
	defer cancelCaller()
	started := make(chan struct{})
	client := &scriptedClient{send: func(ctx context.Context, _ domain.TargetPayload, _ int) (pushover.SendResponse, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 16*time.Second {
			t.Errorf("send context lacks the bounded send phase deadline: %v", deadline)
		}
		close(started)
		<-ctx.Done()
		return pushover.SendResponse{}, &pushover.SendError{Cause: ctx.Err(), Phase: pushover.PhaseWrite, TransmissionPossible: true}
	}}
	runner, closeScope := newRunner(t, client, nil, nil)
	defer closeScope()
	plan := finalizedPlan(t, 1, domain.PriorityNormal)
	result := make(chan domain.BatchResult, 1)
	errs := make(chan error, 1)
	go func() {
		batch, err := runner.Run(caller, plan, RunOptions{})
		result <- batch
		errs <- err
	}()
	<-started
	cancelCaller()
	batch := <-result
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if len(batch.Results) != 1 || batch.Results[0].Outcome != domain.Ambiguous {
		t.Fatalf("results = %#v", batch.Results)
	}
}

func assertInterruptedSkipped(t *testing.T, batch domain.BatchResult, sent, skipped int) {
	t.Helper()
	if !batch.Interrupted {
		t.Fatal("batch was not marked interrupted")
	}
	gotSent, gotSkipped := 0, 0
	for _, result := range batch.Results {
		if result.Outcome == domain.Sent {
			gotSent++
		}
		if result.Outcome == domain.Skipped {
			gotSkipped++
		}
	}
	if gotSent != sent || gotSkipped != skipped {
		t.Fatalf("outcomes = %#v, want sent=%d skipped=%d", batch.Results, sent, skipped)
	}
}

type scriptedClient struct {
	mu    sync.Mutex
	calls []domain.TargetPayload
	send  func(context.Context, domain.TargetPayload, int) (pushover.SendResponse, error)
}

func (c *scriptedClient) Send(ctx context.Context, payload domain.TargetPayload) (pushover.SendResponse, error) {
	c.mu.Lock()
	call := len(c.calls)
	c.calls = append(c.calls, payload)
	send := c.send
	c.mu.Unlock()
	if send == nil {
		return pushover.SendResponse{RequestID: "sent"}, nil
	}
	return send(ctx, payload, call)
}
func (c *scriptedClient) ValidateRecipient(context.Context, string, string, []string) (pushover.RecipientValidation, error) {
	return pushover.RecipientValidation{}, errors.New("unexpected validation")
}
func (c *scriptedClient) Sounds(context.Context, string) (pushover.SoundsResponse, error) {
	return pushover.SoundsResponse{}, errors.New("unexpected sounds")
}
func (c *scriptedClient) Limits(context.Context, string) (pushover.LimitsResponse, error) {
	return pushover.LimitsResponse{}, errors.New("unexpected limits")
}
func (c *scriptedClient) CallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}
func (c *scriptedClient) Aliases() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]string, len(c.calls))
	for i := range c.calls {
		result[i] = c.calls[i].Target.Alias
	}
	return result
}

type recordingConfirmer struct {
	mu    sync.Mutex
	calls []preflight.Confirmation
	err   error
}

type blockingConfirmer struct{ started chan struct{} }

func (c *blockingConfirmer) Confirm(ctx context.Context, _ preflight.Confirmation) error {
	close(c.started)
	<-ctx.Done()
	return ctx.Err()
}

func (c *recordingConfirmer) Confirm(context.Context, preflight.Confirmation) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, preflight.Confirmation{})
	return c.err
}
func (c *recordingConfirmer) CallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

type staticConfig struct{ value config.Config }

func (s staticConfig) Load(context.Context) (config.Config, error) { return s.value, nil }
func (s staticConfig) Commit(context.Context, config.Config) error {
	return errors.New("unexpected commit")
}
func (s staticConfig) Path() string { return "" }

func finalizedPlan(t *testing.T, count, priority int) preflight.FinalizedSendPlan {
	t.Helper()
	ctx := context.Background()
	store := secrets.NewMemoryStore()
	token, err := store.Put(ctx, []byte(strings.Repeat("a", domain.IdentifierLength)))
	if err != nil {
		t.Fatal(err)
	}
	aliases := make([]string, count)
	recipients := make(map[string]config.Recipient, count)
	for i := range aliases {
		aliases[i] = []string{"one", "two", "three"}[i]
		user, putErr := store.Put(ctx, []byte(strings.Repeat(string(rune('b'+i)), domain.IdentifierLength)))
		if putErr != nil {
			t.Fatal(putErr)
		}
		recipients[aliases[i]] = config.Recipient{Alias: aliases[i], UserKeyRef: user, Kind: domain.KindUser}
	}
	scope, err := (operation.Factory{}).Start(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scope.Close)
	message := domain.Message{Priority: priority}
	if priority == domain.PriorityEmergency {
		message.Retry = domain.MinEmergencyRetry
		message.Expire = time.Minute
	}
	builder := &preflight.Builder{
		Resolver: &resolver.Resolver{Config: staticConfig{value: config.Config{SchemaVersion: config.CurrentSchemaVersion, Backend: config.Backend{Type: "native"}, DefaultProfile: "default", Profiles: map[string]config.Profile{"default": {Name: "default", TokenRef: token}}, Recipients: recipients}}, Secrets: store},
		Scope:    scope,
	}
	plan, result, buildErr := builder.Build(ctx, preflight.Request{Resolve: resolver.Input{To: aliases, Message: message, MessageFields: resolver.MessageFieldSet{Priority: true}, Body: resolver.BodySource{Kind: resolver.BodyPositional, Value: "body"}}})
	if buildErr != nil || result != nil || !plan.IsValid() {
		t.Fatalf("plan=%#v result=%#v err=%v", plan, result, buildErr)
	}
	return plan
}

func newRunner(t *testing.T, client pushover.Client, confirmer Confirmer, signals <-chan os.Signal) (*Runner, func()) {
	t.Helper()
	scope, err := (operation.Factory{}).Start(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return &Runner{Client: client, Confirmer: confirmer, Signals: signals, Scope: scope}, scope.Close
}
