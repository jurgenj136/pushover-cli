package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

func TestInitializeAtomicallyAllowsOneCreator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	stores := []*FileStore{NewFileStore(path), NewFileStore(path)}
	values := []Config{validConfig(), validConfig()}
	values[1].Profiles = map[string]Profile{"other": {Name: "other", TokenRef: "other-token"}}
	values[1].DefaultProfile = "other"

	start := make(chan struct{})
	errs := make([]error, 2)
	var wait sync.WaitGroup
	for index := range stores {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			errs[index] = stores[index].Initialize(context.Background(), values[index])
		}(index)
	}
	close(start)
	wait.Wait()

	succeeded, alreadyExists := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrAlreadyExists):
			alreadyExists++
		default:
			t.Fatalf("unexpected initialization error: %v", err)
		}
	}
	if succeeded != 1 || alreadyExists != 1 {
		t.Fatalf("successes=%d already-exists=%d errors=%v", succeeded, alreadyExists, errs)
	}
	loaded, err := NewFileStore(path).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DefaultProfile != "default" && loaded.DefaultProfile != "other" {
		t.Fatalf("unexpected winner: %#v", loaded)
	}
}

func TestInitializeDoesNotReplaceExistingConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	store := NewFileStore(path)
	first, second := validConfig(), validConfig()
	second.Profiles = map[string]Profile{"other": {Name: "other", TokenRef: "other-token"}}
	second.DefaultProfile = "other"
	if err := store.Initialize(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(context.Background(), second); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("err=%v, want ErrAlreadyExists", err)
	}
	loaded, err := store.Load(context.Background())
	if err != nil || loaded.DefaultProfile != "default" {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
}

func TestInitializePublishesOnlyAfterSyncAndReportsDurability(t *testing.T) {
	path := "/config/config.yaml"
	filesystem := newFakeFS()
	if err := NewFileStoreWith(path, filesystem, fixedClock{}).Initialize(context.Background(), validConfig()); err != nil {
		t.Fatal(err)
	}
	want := []string{"mkdir", "chmod:/config", "openfile:/config/.config.yaml.123000000.tmp", "file.chmod:/config/.config.yaml.123000000.tmp", "write:/config/.config.yaml.123000000.tmp", "sync:/config/.config.yaml.123000000.tmp", "close:/config/.config.yaml.123000000.tmp", "link", "remove", "syncdir"}
	if strings.Join(filesystem.events, "|") != strings.Join(want, "|") {
		t.Fatalf("events=%v\nwant=%v", filesystem.events, want)
	}

	postPublish := newFakeFS()
	postPublish.fail["syncdir"] = errors.New("directory flush")
	err := NewFileStoreWith(path, postPublish, fixedClock{}).Initialize(context.Background(), validConfig())
	if OutcomeOf(err) != AppliedDurabilityUnknown {
		t.Fatalf("outcome=%s err=%v", OutcomeOf(err), err)
	}
	if _, exists := postPublish.contents[path]; !exists {
		t.Fatal("published config should remain after directory sync failure")
	}

	prePublish := newFakeFS()
	prePublish.fail["link"] = errors.New("link failed")
	err = NewFileStoreWith(path, prePublish, fixedClock{}).Initialize(context.Background(), validConfig())
	if OutcomeOf(err) != Unchanged {
		t.Fatalf("outcome=%s err=%v", OutcomeOf(err), err)
	}
	if _, exists := prePublish.contents[path]; exists {
		t.Fatal("configuration was visible after failed publication")
	}
}

func TestLoadStrictSchemaAndNotFound(t *testing.T) {
	path := "/config/config.yaml"
	missing := NewFileStoreWith(path, newFakeFS(), fixedClock{})
	if _, err := missing.Load(context.Background()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	for _, document := range []string{
		"schema_version: 1\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\nunknown: true\n",
		"schema_version: 1\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\n---\nschema_version: 1\n",
		"schema_version: 1\nschema_version: 1\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\n",
		"schema_version: 1\nbackend: {type: native}\nprofiles: {}\nrecipients: {}\nprofiles: [\n",
		"schema_version: 1\nbackend: {type: native}\nprofiles: {p: {name: p, token_ref: token, defaults: {ttl: 900}}}\nrecipients: {}\n",
	} {
		filesystem := newFakeFS()
		filesystem.contents[path] = []byte(document)
		store := NewFileStoreWith(path, filesystem, fixedClock{})
		if _, err := store.Load(context.Background()); err == nil {
			t.Fatalf("Load accepted invalid document: %q", document)
		}
		if strings.Join(filesystem.events, "|") != "open|close:/config/config.yaml" {
			t.Fatalf("Load wrote config: %v", filesystem.events)
		}
	}
}

func TestLoadCanonicalV1Fixture(t *testing.T) {
	path := "/config/config.yaml"
	fixture, err := os.ReadFile("testdata/v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	filesystem := newFakeFS()
	filesystem.contents[path] = fixture
	config, err := NewFileStoreWith(path, filesystem, fixedClock{}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := config.Profiles["default"].Defaults.TTL; got != 15*time.Minute {
		t.Fatalf("ttl = %s", got)
	}
	if config.Recipients["ops"].Kind != domain.KindUser {
		t.Fatalf("kind = %q", config.Recipients["ops"].Kind)
	}
	encoded := newFakeFS()
	if err := NewFileStoreWith(path, encoded, fixedClock{}).Commit(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	if got := string(encoded.contents[path]); got != string(fixture) {
		t.Fatalf("canonical encoding mismatch\ngot:\n%s\nwant:\n%s", got, fixture)
	}
}

func TestCommitAtomicOrderingAndFailures(t *testing.T) {
	path := "/config/config.yaml"
	config := validConfig()
	filesystem := newFakeFS()
	store := NewFileStoreWith(path, filesystem, fixedClock{})
	if err := store.Commit(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	want := []string{"mkdir", "chmod:/config", "openfile:/config/.config.yaml.123000000.tmp", "file.chmod:/config/.config.yaml.123000000.tmp", "write:/config/.config.yaml.123000000.tmp", "sync:/config/.config.yaml.123000000.tmp", "close:/config/.config.yaml.123000000.tmp", "rename", "syncdir"}
	if strings.Join(filesystem.events, "|") != strings.Join(want, "|") {
		t.Fatalf("events = %v\nwant %v", filesystem.events, want)
	}
	if _, ok := filesystem.contents[path]; !ok {
		t.Fatal("config was not renamed into place")
	}

	for _, stage := range []string{"write", "sync", "close", "rename"} {
		t.Run(stage, func(t *testing.T) {
			failed := newFakeFS()
			failed.contents[path] = []byte("old")
			failed.fail[stage] = errors.New(stage)
			err := NewFileStoreWith(path, failed, fixedClock{}).Commit(context.Background(), config)
			if err == nil || OutcomeOf(err) != Unchanged {
				t.Fatalf("error outcome = %v (%v)", OutcomeOf(err), err)
			}
			if got := string(failed.contents[path]); got != "old" {
				t.Fatalf("prior config changed: %q", got)
			}
		})
	}
	postRename := newFakeFS()
	postRename.fail["syncdir"] = errors.New("directory flush")
	err := NewFileStoreWith(path, postRename, fixedClock{}).Commit(context.Background(), config)
	if OutcomeOf(err) != AppliedDurabilityUnknown {
		t.Fatalf("outcome = %s, want applied unknown (%v)", OutcomeOf(err), err)
	}
	if _, ok := postRename.contents[path]; !ok {
		t.Fatal("visible config should remain after directory sync failure")
	}
}

func TestCommitCreatesPrivatePOSIXFiles(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("POSIX mode semantics")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "nested", "config.yaml")
	if err := NewFileStore(path).Commit(context.Background(), validConfig()); err != nil {
		t.Fatal(err)
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %o", parent.Mode().Perm())
	}
	if file.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o", file.Mode().Perm())
	}
}

func TestCommitValidatesBeforeMutation(t *testing.T) {
	filesystem := newFakeFS()
	bad := validConfig()
	bad.Profiles["default"] = Profile{Name: "default"}
	err := NewFileStoreWith("/config/config.yaml", filesystem, fixedClock{}).Commit(context.Background(), bad)
	if err == nil || OutcomeOf(err) != Unchanged {
		t.Fatalf("err = %v", err)
	}
	if len(filesystem.events) != 0 {
		t.Fatalf("mutation before validation: %v", filesystem.events)
	}
}

func TestCommitErrorRedactsUnderlyingPaths(t *testing.T) {
	underlying := pathCause("open /Users/alice/.config/pushover-cli/config.yaml: permission denied")
	err := NewCommitError(Unchanged, underlying)
	for _, candidate := range []error{err, fmt.Errorf("trusted wrapper: %w", err)} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			rendered := fmt.Sprintf(format, candidate)
			if strings.Contains(rendered, "/Users/alice") || strings.Contains(rendered, "permission denied") {
				t.Fatalf("CommitError leaked private cause with %s: %q", format, rendered)
			}
		}
	}
	if !errors.Is(err, underlying) {
		t.Fatal("CommitError must retain its private unwrap cause")
	}
}

func TestOutcomeOfHandlesTypedNilAndWrappedErrors(t *testing.T) {
	var typedNil *CommitError
	var asError error = typedNil
	if got := OutcomeOf(asError); got != Unchanged {
		t.Fatalf("typed nil outcome = %s", got)
	}
	if got := typedNil.Error(); got != "configuration was not changed" {
		t.Fatalf("typed nil Error = %q", got)
	}
	if typedNil.Unwrap() != nil {
		t.Fatal("typed nil Unwrap must be nil")
	}
	if got := OutcomeOf(errors.New("ordinary")); got != Unchanged {
		t.Fatalf("ordinary outcome = %s", got)
	}
	wrapped := fmt.Errorf("trusted wrapper: %w", NewCommitError(AppliedDurabilityUnknown, errors.New("private")))
	if got := OutcomeOf(wrapped); got != AppliedDurabilityUnknown {
		t.Fatalf("wrapped outcome = %s", got)
	}
}

type pathCause string

func (e pathCause) Error() string { return string(e) }

func validConfig() Config {
	return Config{SchemaVersion: CurrentSchemaVersion, Backend: Backend{Type: "native"}, DefaultProfile: "default", Profiles: map[string]Profile{"default": {Name: "default", TokenRef: domain.SecretRef("token-ref"), Defaults: MessageDefaults{Title: "Alerts", TTL: 15 * time.Minute}}}, Recipients: map[string]Recipient{"ops": {Alias: "ops", UserKeyRef: domain.SecretRef("user-ref"), Kind: domain.KindUser, Validated: true}}}
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Unix(0, 123_000_000).UTC() }

type fakeFS struct {
	contents map[string][]byte
	files    map[string]*fakeFile
	events   []string
	fail     map[string]error
}

func newFakeFS() *fakeFS {
	return &fakeFS{contents: map[string][]byte{}, files: map[string]*fakeFile{}, fail: map[string]error{}}
}
func (f *fakeFS) MkdirAll(string, fs.FileMode) error {
	f.events = append(f.events, "mkdir")
	return f.failed("mkdir")
}
func (f *fakeFS) Open(path string) (File, error) {
	f.events = append(f.events, "open")
	if err := f.failed("open"); err != nil {
		return nil, err
	}
	data, ok := f.contents[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return &fakeFile{path: path, reader: bytes.NewReader(data), owner: f}, nil
}
func (f *fakeFS) OpenFile(path string, _ int, _ fs.FileMode) (File, error) {
	f.events = append(f.events, "openfile:"+path)
	if err := f.failed("openfile"); err != nil {
		return nil, err
	}
	if _, exists := f.contents[path]; exists {
		return nil, fs.ErrExist
	}
	file := &fakeFile{path: path, owner: f}
	f.files[path] = file
	return file, nil
}
func (f *fakeFS) Rename(oldPath, newPath string) error {
	f.events = append(f.events, "rename")
	if err := f.failed("rename"); err != nil {
		return err
	}
	if data, ok := f.contents[oldPath]; ok {
		f.contents[newPath] = data
		delete(f.contents, oldPath)
	}
	return nil
}
func (f *fakeFS) Link(oldPath, newPath string) error {
	f.events = append(f.events, "link")
	if err := f.failed("link"); err != nil {
		return err
	}
	if _, exists := f.contents[newPath]; exists {
		return fs.ErrExist
	}
	data, exists := f.contents[oldPath]
	if !exists {
		return fs.ErrNotExist
	}
	f.contents[newPath] = append([]byte(nil), data...)
	return nil
}
func (f *fakeFS) Chmod(path string, _ fs.FileMode) error {
	f.events = append(f.events, "chmod:"+path)
	return f.failed("chmod")
}
func (f *fakeFS) Remove(path string) error {
	f.events = append(f.events, "remove")
	delete(f.contents, path)
	return f.failed("remove")
}
func (f *fakeFS) SyncDir(string) error {
	f.events = append(f.events, "syncdir")
	return f.failed("syncdir")
}
func (f *fakeFS) failed(stage string) error { return f.fail[stage] }

type fakeFile struct {
	path   string
	reader *bytes.Reader
	buffer bytes.Buffer
	owner  *fakeFS
	closed bool
}

func (f *fakeFile) Read(p []byte) (int, error) {
	if f.reader == nil {
		return 0, io.EOF
	}
	return f.reader.Read(p)
}
func (f *fakeFile) Write(p []byte) (int, error) {
	f.owner.events = append(f.owner.events, "write:"+f.path)
	if err := f.owner.failed("write"); err != nil {
		return 0, err
	}
	return f.buffer.Write(p)
}
func (f *fakeFile) Sync() error {
	f.owner.events = append(f.owner.events, "sync:"+f.path)
	return f.owner.failed("sync")
}
func (f *fakeFile) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true
	f.owner.events = append(f.owner.events, "close:"+f.path)
	if f.reader == nil {
		f.owner.contents[f.path] = append([]byte(nil), f.buffer.Bytes()...)
	}
	return f.owner.failed("close")
}
func (f *fakeFile) Chmod(fs.FileMode) error {
	f.owner.events = append(f.owner.events, "file.chmod:"+f.path)
	return f.owner.failed("file.chmod")
}
