//go:build live

// Package live contains the deliberately opt-in Pushover smoke test. It is
// excluded from ordinary builds and never constructs a client at package init.
package live

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/pushover"
)

var errLiveOptIn = errors.New("live test requires PUSHOVER_LIVE_CONFIRM=YES, PUSHOVER_LIVE_TOKEN, and PUSHOVER_LIVE_USER_KEY")

type liveCredentials struct {
	token string
	user  string
}

// liveClient checks every opt-in condition before invoking factory. Keeping
// the factory injected makes the no-client-before-confirmation rule testable.
func liveClient(factory func() pushover.Client) (pushover.Client, liveCredentials, error) {
	if os.Getenv("PUSHOVER_LIVE_CONFIRM") != "YES" {
		return nil, liveCredentials{}, errLiveOptIn
	}
	credentials := liveCredentials{token: os.Getenv("PUSHOVER_LIVE_TOKEN"), user: os.Getenv("PUSHOVER_LIVE_USER_KEY")}
	if domain.ValidateCredentials(domain.Credentials{Token: credentials.token, UserKey: credentials.user}) != nil {
		return nil, liveCredentials{}, errLiveOptIn
	}
	if factory == nil {
		return nil, liveCredentials{}, errors.New("live client factory is nil")
	}
	return factory(), credentials, nil
}

func TestLiveSendRequiresConfirmationBeforeClient(t *testing.T) {
	for _, test := range []struct {
		name, confirmation, token, user string
	}{
		{"missing confirmation", "", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		{"missing token", "YES", "", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		{"missing user key", "YES", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("PUSHOVER_LIVE_CONFIRM", test.confirmation)
			t.Setenv("PUSHOVER_LIVE_TOKEN", test.token)
			t.Setenv("PUSHOVER_LIVE_USER_KEY", test.user)
			constructed := false
			client, _, err := liveClient(func() pushover.Client {
				constructed = true
				return nil
			})
			if !errors.Is(err, errLiveOptIn) {
				t.Fatalf("error = %v, want opt-in error", err)
			}
			if client != nil || constructed {
				t.Fatalf("client=%v constructed=%v; incomplete opt-in must stop before construction", client, constructed)
			}
		})
	}
}

// TestLiveSend is intentionally never part of normal CI. With all three
// dedicated variables set it performs exactly one lowest-priority (-2) send.
func TestLiveSend(t *testing.T) {
	client, credentials, err := liveClient(func() pushover.Client { return pushover.NewClient(nil) })
	if err != nil {
		t.Skip(err)
	}
	title := "[pushover-cli live test] " + time.Now().UTC().Format(time.RFC3339)
	_, err = client.Send(context.Background(), domain.TargetPayload{
		Target:  domain.Target{Credentials: domain.Credentials{Token: credentials.token, UserKey: credentials.user}},
		Message: domain.Message{Body: "pushover-cli live smoke test", Title: title, Priority: domain.PriorityLowest},
	})
	if err != nil {
		t.Fatalf("live send failed: %v", err)
	}
}
