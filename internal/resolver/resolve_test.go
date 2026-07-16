package resolver

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

type resolverConfig struct {
	value config.Config
	err   error
}

func (s resolverConfig) Load(context.Context) (config.Config, error) { return s.value, s.err }
func (s resolverConfig) Commit(context.Context, config.Config) error { return nil }
func (s resolverConfig) Path() string                                { return "" }

type resolverSecrets struct {
	values map[domain.SecretRef][]byte
	gets   int
}

func (s *resolverSecrets) Get(_ context.Context, ref domain.SecretRef) ([]byte, error) {
	s.gets++
	value, ok := s.values[ref]
	if !ok {
		return nil, errors.New("not found")
	}
	return append([]byte(nil), value...), nil
}
func (s *resolverSecrets) Put(context.Context, []byte) (domain.SecretRef, error) {
	return "", errors.New("not implemented")
}
func (s *resolverSecrets) Delete(context.Context, domain.SecretRef) error { return nil }
func (s *resolverSecrets) Check(context.Context, domain.SecretRef) error  { return nil }

func TestResolveConfiguredPreservesAliasOrderAndExplicitDefaults(t *testing.T) {
	token, first, second := strings.Repeat("a", 30), strings.Repeat("b", 30), strings.Repeat("c", 30)
	store := &resolverSecrets{values: map[domain.SecretRef][]byte{"token": []byte(token), "first": []byte(first), "second": []byte(second)}}
	configuration := config.Config{SchemaVersion: 1, Backend: config.Backend{Type: "native"}, DefaultProfile: "default", Profiles: map[string]config.Profile{"default": {Name: "default", TokenRef: "token", Defaults: config.MessageDefaults{Title: "saved", Priority: -1, TTL: time.Minute, Devices: []string{"profile"}}}}, Recipients: map[string]config.Recipient{"one": {Alias: "one", UserKeyRef: "first", Kind: domain.KindUser, Devices: []string{"recipient"}}, "two": {Alias: "two", UserKeyRef: "second", Kind: domain.KindUser}}}
	draft, err := (&Resolver{Config: resolverConfig{value: configuration}, Secrets: store}).Resolve(context.Background(), Input{To: []string{"two", "one"}, Message: domain.Message{Priority: 0}, MessageFields: MessageFieldSet{Priority: true}, Body: BodySource{Kind: BodyPositional, Value: "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Targets) != 2 || draft.Targets[0].Target.Alias != "two" || draft.Targets[1].Target.Alias != "one" {
		t.Fatalf("order = %#v", draft.Targets)
	}
	if got := draft.Targets[0].Message; got.Title != "saved" || got.Priority != 0 || got.TTL != time.Minute || got.Devices[0] != "profile" {
		t.Fatalf("first effective message = %#v", got)
	}
	if got := draft.Targets[1].Message.Devices[0]; got != "recipient" {
		t.Fatalf("recipient devices = %q", got)
	}
}
func TestResolveSelectsExplicitProfileAndHonorsEmptyFieldMasks(t *testing.T) {
	token, user := strings.Repeat("a", 30), strings.Repeat("b", 30)
	store := &resolverSecrets{values: map[domain.SecretRef][]byte{"one-token": []byte(token), "two-token": []byte(token), "user": []byte(user)}}
	configuration := config.Config{SchemaVersion: 1, Backend: config.Backend{Type: "native"}, DefaultProfile: "one", Profiles: map[string]config.Profile{
		"one": {Name: "one", TokenRef: "one-token", Defaults: config.MessageDefaults{Title: "default", Sound: "pushover", Devices: []string{"saved"}, Priority: -1, TTL: time.Minute}},
		"two": {Name: "two", TokenRef: "two-token", Defaults: config.MessageDefaults{Title: "explicit-profile", URL: "https://example.com"}},
	}, Recipients: map[string]config.Recipient{"r": {Alias: "r", UserKeyRef: "user"}}}
	draft, err := (&Resolver{Config: resolverConfig{value: configuration}, Secrets: store}).Resolve(context.Background(), Input{Profile: "two", To: []string{"r"}, Message: domain.Message{Title: "", Sound: "", Devices: []string{}, Priority: 0, TTL: 0}, MessageFields: MessageFieldSet{Title: true, Sound: true, Devices: true, Priority: true, TTL: true}, Body: BodySource{Kind: BodyPositional, Value: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	message := draft.Targets[0].Message
	if message.Title != "" || message.URL != "https://example.com" || message.Sound != "" || len(message.Devices) != 0 || message.Priority != 0 || message.TTL != 0 {
		t.Fatalf("explicit masks lost: %#v", message)
	}
}
func TestResolveRejectsDuplicatesBeforeSecretReadsAndReturnsNoPartialDraft(t *testing.T) {
	store := &resolverSecrets{values: map[domain.SecretRef][]byte{}}
	resolver := &Resolver{Config: resolverConfig{}, Secrets: store}
	draft, err := resolver.Resolve(context.Background(), Input{To: []string{"ops", "ops"}, Body: BodySource{Kind: BodyPositional, Value: "x"}})
	if err == nil || len(draft.Targets) != 0 || store.gets != 0 {
		t.Fatalf("draft=%#v gets=%d err=%v", draft, store.gets, err)
	}
}
func TestResolveEncryptedBatchFailsClosed(t *testing.T) {
	token, user := strings.Repeat("a", 30), strings.Repeat("b", 30)
	store := &resolverSecrets{values: map[domain.SecretRef][]byte{"token": []byte(token), "user": []byte(user)}}
	configuration := config.Config{SchemaVersion: 1, Backend: config.Backend{Type: "native"}, DefaultProfile: "p", Profiles: map[string]config.Profile{"p": {Name: "p", TokenRef: "token"}}, Recipients: map[string]config.Recipient{"r": {Alias: "r", UserKeyRef: "user"}}}
	draft, err := (&Resolver{Config: resolverConfig{value: configuration}, Secrets: store}).Resolve(context.Background(), Input{To: []string{"r"}, Message: domain.Message{Encrypted: true}, Body: BodySource{Kind: BodyPositional, Value: "x"}})
	if err == nil || len(draft.Targets) != 0 {
		t.Fatalf("encrypted draft = %#v, %v", draft, err)
	}
}
func TestResolveEncryptedBatchWithLaterBadKeyReturnsZeroDraft(t *testing.T) {
	token, user, key := strings.Repeat("a", 30), strings.Repeat("b", 30), strings.Repeat("c", 64)
	store := &resolverSecrets{values: map[domain.SecretRef][]byte{"token": []byte(token), "one": []byte(user), "two": []byte(user), "valid": []byte(key), "invalid": []byte("bad")}}
	configuration := config.Config{SchemaVersion: 1, Backend: config.Backend{Type: "native"}, DefaultProfile: "p", Profiles: map[string]config.Profile{"p": {Name: "p", TokenRef: "token"}}, Recipients: map[string]config.Recipient{"one": {Alias: "one", UserKeyRef: "one", E2EEKeyRef: "valid"}, "two": {Alias: "two", UserKeyRef: "two", E2EEKeyRef: "invalid"}}}
	draft, err := (&Resolver{Config: resolverConfig{value: configuration}, Secrets: store}).Resolve(context.Background(), Input{To: []string{"one", "two"}, Message: domain.Message{Encrypted: true}, Body: BodySource{Kind: BodyPositional, Value: "x"}})
	if err == nil || len(draft.Targets) != 0 {
		t.Fatalf("draft=%#v err=%v", draft, err)
	}
}
func TestContextConflictsAreRejectedBeforeReadingBody(t *testing.T) {
	reader := &countReader{}
	_, err := (&Resolver{}).Resolve(context.Background(), Input{Credentials: strings.NewReader(`{}`), Body: BodySource{Kind: BodyStdin, Reader: reader}})
	if err == nil || reader.reads != 0 {
		t.Fatalf("err=%v reads=%d", err, reader.reads)
	}
	_, err = (&Resolver{}).Resolve(context.Background(), Input{EnvTokenSet: true, EnvUserKeySet: true, EnvToken: "", EnvUserKey: "", Body: BodySource{Kind: BodyPositional, Value: "x"}})
	if err == nil {
		t.Fatal("accepted present-but-empty environment pair")
	}
	credentials := &countReader{}
	_, err = (&Resolver{}).Resolve(context.Background(), Input{EnvTokenSet: true, EnvUserKeySet: true, EnvToken: strings.Repeat("a", 30), EnvUserKey: strings.Repeat("b", 30), Credentials: credentials, Body: BodySource{Kind: BodyPositional, Value: "x"}})
	if err == nil || credentials.reads != 0 {
		t.Fatalf("environment/credentials conflict read=%d err=%v", credentials.reads, err)
	}
}
func TestReadBodyBoundariesAndReaderFailures(t *testing.T) {
	exactBytes := strings.Repeat("x", int(MaxBodyBytes))
	if got, err := readBoundedBody(strings.NewReader(exactBytes)); err != nil || len(got) != int(MaxBodyBytes) {
		t.Fatalf("byte boundary read: %d %v", len(got), err)
	}
	if _, err := readBody(BodySource{Kind: BodyPositional, Value: exactBytes}); err == nil {
		t.Fatal("accepted byte-valid but rune-invalid body")
	}
	if _, err := readBody(BodySource{Kind: BodyPositional, Value: strings.Repeat("x", int(MaxBodyBytes)+1)}); err == nil {
		t.Fatal("accepted body over byte limit")
	}
	if _, err := readBody(BodySource{Kind: BodyPositional, Value: string([]byte{0xff})}); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
	if _, err := readBody(BodySource{Kind: BodyPositional, Value: strings.Repeat("x", domain.MaxBodyRunes+1)}); err == nil {
		t.Fatal("accepted body over rune limit")
	}
	if got, err := readBody(BodySource{Kind: BodyPositional, Value: strings.Repeat("😀", domain.MaxBodyRunes)}); err != nil || len([]rune(got)) != domain.MaxBodyRunes {
		t.Fatalf("valid rune boundary: %d %v", len([]rune(got)), err)
	}
	failing := &readClose{err: errors.New("read")}
	if _, err := readBody(BodySource{Kind: BodyFile, Value: "hidden", OpenFile: func(string) (io.ReadCloser, error) { return failing, nil }}); err == nil || !failing.closed {
		t.Fatal("read/close error was not handled")
	}
	closeOnly := &readClose{data: []byte("x"), closeErr: errors.New("close")}
	if _, err := readBody(BodySource{Kind: BodyFile, Value: "hidden", OpenFile: func(string) (io.ReadCloser, error) { return closeOnly, nil }}); err == nil || !closeOnly.closed {
		t.Fatal("close error was not handled")
	}
	if _, err := readBody(BodySource{Kind: BodyStdin, Reader: zeroReader{}}); err == nil {
		t.Fatal("accepted no-progress body reader")
	}
}

func TestInvalidBodyStopsBeforeSecretReads(t *testing.T) {
	store := &resolverSecrets{values: map[domain.SecretRef][]byte{}}
	_, err := (&Resolver{Config: resolverConfig{}, Secrets: store}).Resolve(context.Background(), Input{To: []string{"r"}, Body: BodySource{Kind: BodyPositional, Value: strings.Repeat("x", domain.MaxBodyRunes+1)}})
	if err == nil || store.gets != 0 {
		t.Fatalf("err=%v gets=%d", err, store.gets)
	}
}

func TestResolveDoesNotReturnPartialDraftForLaterMissingReference(t *testing.T) {
	token, first := strings.Repeat("a", 30), strings.Repeat("b", 30)
	store := &resolverSecrets{values: map[domain.SecretRef][]byte{"token": []byte(token), "first": []byte(first)}}
	configuration := config.Config{SchemaVersion: 1, Backend: config.Backend{Type: "native"}, DefaultProfile: "p", Profiles: map[string]config.Profile{"p": {Name: "p", TokenRef: "token"}}, Recipients: map[string]config.Recipient{"one": {Alias: "one", UserKeyRef: "first"}, "two": {Alias: "two", UserKeyRef: "missing"}}}
	draft, err := (&Resolver{Config: resolverConfig{value: configuration}, Secrets: store}).Resolve(context.Background(), Input{To: []string{"one", "two"}, Body: BodySource{Kind: BodyPositional, Value: "x"}})
	if err == nil || len(draft.Targets) != 0 {
		t.Fatalf("draft=%#v err=%v", draft, err)
	}
}

type zeroReader struct{}

func (zeroReader) Read([]byte) (int, error) { return 0, nil }

type countReader struct{ reads int }

func (r *countReader) Read([]byte) (int, error) { r.reads++; return 0, io.EOF }

type readClose struct {
	data          []byte
	err, closeErr error
	closed        bool
}

func (r *readClose) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}
func (r *readClose) Close() error { r.closed = true; return r.closeErr }
