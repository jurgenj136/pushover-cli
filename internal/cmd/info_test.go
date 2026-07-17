package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/buildinfo"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/operation"
	"github.com/jurgenj136/pushover-cli/internal/pushover"
	"github.com/jurgenj136/pushover-cli/internal/secrets"
)

func TestIntrospectionSoundsAreFreshSortedAndWipeSelectedToken(t *testing.T) {
	token := []byte(strings.Repeat("a", domain.IdentifierLength))
	service := &introspectionSettingsFake{adminSettingsFake: adminSettingsFake{token: token}}
	api := &introspectionAPIFake{sounds: []pushover.SoundsResponse{
		{Sounds: []pushover.Sound{{Name: "zeta", Description: "Z"}, {Name: "alpha", Description: "A"}}},
		{Sounds: []pushover.Sound{{Name: "zeta", Description: "Z2"}, {Name: "alpha", Description: "A2"}}},
	}}
	contexts := &recordingContexts{}
	deps := Dependencies{Admin: AdminDependencies{Settings: service, API: api}, Operations: operation.Factory{Contexts: contexts}}

	for call, want := range []string{"alpha\tA\nzeta\tZ\n", "alpha\tA2\nzeta\tZ2\n"} {
		var stdout, stderr bytes.Buffer
		if got := executeIntrospection(context.Background(), testIO(&stdout, &stderr), deps, rootCommand{Plain: true}, nil, "sounds"); got != 0 {
			t.Fatalf("call %d exit=%d stderr=%q", call, got, stderr.String())
		}
		if got := stdout.String(); got != want {
			t.Fatalf("call %d output=%q want=%q", call, got, want)
		}
	}

	if got, want := service.selected, []string{"", ""}; !sameStrings(got, want) {
		t.Fatalf("selected profiles=%q want=%q", got, want)
	}
	if got, want := api.soundTokens, []string{string(token), string(token)}; !sameStrings(got, want) {
		t.Fatalf("API tokens=%q want=%q", got, want)
	}
	if service.selectedTokenCalls != 2 || len(service.returnedTokens) != 2 {
		t.Fatalf("selected token calls=%d returned=%d", service.selectedTokenCalls, len(service.returnedTokens))
	}
	for index, value := range service.returnedTokens {
		if !bytesZero(value) {
			t.Fatalf("returned token %d was not wiped: %q", index, value)
		}
	}
	if len(contexts.created) != 4 {
		t.Fatalf("scope/phase contexts=%d, want 4", len(contexts.created))
	}
	if len(api.soundContexts) != 2 || api.soundContexts[0] != contexts.created[1] || api.soundContexts[1] != contexts.created[3] {
		t.Fatalf("sound contexts did not use validation phases: %#v", api.soundContexts)
	}
}

func TestIntrospectionLimitsUsesExplicitProfileAndExactJSON(t *testing.T) {
	service := &introspectionSettingsFake{adminSettingsFake: adminSettingsFake{token: []byte(strings.Repeat("b", domain.IdentifierLength))}}
	api := &introspectionAPIFake{limitResponses: []pushover.LimitsResponse{{Limit: 10000, Remaining: 9988, Reset: 1_800_000_000, RequestID: "must-not-render"}}}
	contexts := &recordingContexts{}
	var stdout, stderr bytes.Buffer

	got := executeIntrospection(context.Background(), testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}, Operations: operation.Factory{Contexts: contexts}}, rootCommand{JSON: true, Profile: "other"}, nil, "limits")
	if got != 0 {
		t.Fatalf("exit=%d stderr=%q", got, stderr.String())
	}
	if want := "{\"schema_version\":1,\"command\":\"limits\",\"status\":\"success\",\"result\":{\"limit\":10000,\"remaining\":9988,\"reset_unix\":1800000000}}\n"; stdout.String() != want {
		t.Fatalf("JSON=%q want=%q", stdout.String(), want)
	}
	if got, want := service.selected, []string{"other"}; !sameStrings(got, want) {
		t.Fatalf("selected profiles=%q want=%q", got, want)
	}
	if got, want := api.limitTokens, []string{strings.Repeat("b", domain.IdentifierLength)}; !sameStrings(got, want) {
		t.Fatalf("API tokens=%q want=%q", got, want)
	}
	if len(service.returnedTokens) != 1 || !bytesZero(service.returnedTokens[0]) {
		t.Fatalf("returned token was not wiped: %#v", service.returnedTokens)
	}
	if len(contexts.created) != 2 {
		t.Fatalf("scope/phase contexts=%d, want 2", len(contexts.created))
	}
	if len(api.limitContexts) != 1 || api.limitContexts[0] != contexts.created[1] {
		t.Fatalf("limits context did not use validation phase: %#v", api.limitContexts)
	}
}

func TestIntrospectionReadHumanAndPlainContracts(t *testing.T) {
	for _, test := range []struct {
		name    string
		command string
		root    rootCommand
		api     *introspectionAPIFake
		want    string
	}{
		{
			name:    "sounds human",
			command: "sounds",
			api:     &introspectionAPIFake{sounds: []pushover.SoundsResponse{{Sounds: []pushover.Sound{{Name: "alpha", Description: "A"}}}}},
			want:    "alpha A\n",
		},
		{
			name:    "limits plain",
			command: "limits",
			root:    rootCommand{Plain: true},
			api:     &introspectionAPIFake{limitResponses: []pushover.LimitsResponse{{Limit: 10000, Remaining: 9999, Reset: 1_800_000_000}}},
			want:    "10000\t9999\t1800000000\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &introspectionSettingsFake{adminSettingsFake: adminSettingsFake{token: []byte(strings.Repeat("a", domain.IdentifierLength))}}
			var stdout, stderr bytes.Buffer
			got := executeIntrospection(context.Background(), testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: test.api}}, test.root, nil, test.command)
			if got != 0 || stdout.String() != test.want || stderr.Len() != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
			}
		})
	}
	t.Run("plain grammar error", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		got := Execute(context.Background(), []string{"--plain", "limits", "extra"}, testIO(&stdout, &stderr), Dependencies{})
		if got != apperror.ExitUsage || stdout.Len() != 0 || stderr.String() != "error: invalid command line\n" {
			t.Fatalf("exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
		}
	})
}

func TestIntrospectionUsesOneLazyAdminFactory(t *testing.T) {
	service := &introspectionSettingsFake{adminSettingsFake: adminSettingsFake{token: []byte(strings.Repeat("a", domain.IdentifierLength))}}
	api := &introspectionAPIFake{sounds: []pushover.SoundsResponse{{Sounds: []pushover.Sound{{Name: "pushover", Description: "Default"}}}}}
	factoryCalls := 0
	deps := Dependencies{admin: func(context.Context) (AdminDependencies, error) {
		factoryCalls++
		return AdminDependencies{Settings: service, API: api}, nil
	}}
	var stdout, stderr bytes.Buffer
	if got := executeIntrospection(context.Background(), testIO(&stdout, &stderr), deps, rootCommand{}, nil, "sounds"); got != 0 || factoryCalls != 1 {
		t.Fatalf("exit=%d factory calls=%d stdout=%q stderr=%q", got, factoryCalls, stdout.String(), stderr.String())
	}
}

func TestIntrospectionIgnoresCompleteEnvironmentCredentials(t *testing.T) {
	storedToken := strings.Repeat("a", domain.IdentifierLength)
	service := &introspectionSettingsFake{adminSettingsFake: adminSettingsFake{token: []byte(storedToken)}}
	api := &introspectionAPIFake{sounds: []pushover.SoundsResponse{{Sounds: []pushover.Sound{{Name: "pushover", Description: "Default"}}}}}
	deps := Dependencies{
		Admin: AdminDependencies{Settings: service, API: api},
		lookupEnv: func(name string) (string, bool) {
			if name == "PUSHOVER_TOKEN" {
				return "environment-token", true
			}
			if name == "PUSHOVER_USER_KEY" {
				return "environment-user-key", true
			}
			return "", false
		},
	}
	var stdout, stderr bytes.Buffer
	if got := executeIntrospection(context.Background(), testIO(&stdout, &stderr), deps, rootCommand{}, nil, "sounds"); got != 0 || len(api.soundTokens) != 1 || api.soundTokens[0] != storedToken {
		t.Fatalf("exit=%d tokens=%q stderr=%q", got, api.soundTokens, stderr.String())
	}
}

func TestIntrospectionReadFailuresAreTypedAndDoNotLeakTokens(t *testing.T) {
	secret := strings.Repeat("s", domain.IdentifierLength)
	for _, test := range []struct {
		name string
		api  *introspectionAPIFake
		want int
		code string
	}{
		{name: "API false", api: &introspectionAPIFake{soundErrs: []error{apperror.New("api_rejected", apperror.CategoryValidation, "Pushover rejected the request", apperror.WithSecrets(secret))}}, want: apperror.ExitValidation, code: "api_rejected"},
		{name: "deadline", api: &introspectionAPIFake{soundErrs: []error{context.DeadlineExceeded}}, want: apperror.ExitNetwork, code: "operation_timeout"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &introspectionSettingsFake{adminSettingsFake: adminSettingsFake{token: []byte(secret)}}
			var stdout, stderr bytes.Buffer
			got := executeIntrospection(context.Background(), testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: test.api}}, rootCommand{JSON: true}, nil, "sounds")
			if got != test.want || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"code":"`+test.code+`"`) || strings.Contains(stderr.String(), secret) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
			}
			if len(service.returnedTokens) != 1 || !bytesZero(service.returnedTokens[0]) {
				t.Fatalf("returned token was not wiped: %#v", service.returnedTokens)
			}
		})
	}

	t.Run("API unavailable before secret lookup", func(t *testing.T) {
		service := &introspectionSettingsFake{adminSettingsFake: adminSettingsFake{token: []byte(secret)}}
		var stdout, stderr bytes.Buffer
		got := executeIntrospection(context.Background(), testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service}}, rootCommand{}, nil, "sounds")
		if got != apperror.ExitInternal || service.selectedTokenCalls != 0 || strings.Contains(stderr.String(), secret) {
			t.Fatalf("exit=%d token calls=%d stderr=%q", got, service.selectedTokenCalls, stderr.String())
		}
	})
}

func TestIntrospectionMissingConfigurationIsTypedBeforeOpeningSecrets(t *testing.T) {
	for _, command := range []string{"sounds", "limits"} {
		t.Run(command, func(t *testing.T) {
			store := &introspectionConfigStore{path: "/safe/config.yaml", err: config.ErrNotFound}
			factory := &introspectionSecretFactory{}
			api := &introspectionAPIFake{}
			var stdout, stderr bytes.Buffer
			exit := Execute(context.Background(), []string{"--json", command}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Config: store, SecretFactory: factory, API: api}})
			if exit != apperror.ExitConfiguration || stdout.Len() != 0 || factory.opens != 0 || api.soundCalls != 0 || api.limitCalls != 0 {
				t.Fatalf("exit=%d stdout=%q opens=%d sound calls=%d limit calls=%d", exit, stdout.String(), factory.opens, api.soundCalls, api.limitCalls)
			}
			if !strings.Contains(stderr.String(), `"command":"`+command+`"`) || !strings.Contains(stderr.String(), `"code":"configuration_unavailable"`) || !strings.Contains(stderr.String(), `"category":"configuration"`) {
				t.Fatalf("stderr=%q", stderr.String())
			}
		})
	}
}

func TestIntrospectionReadsRejectPartialEnvironmentAndTimeoutBeforeRuntime(t *testing.T) {
	for _, test := range []struct {
		name   string
		root   rootCommand
		parsed *kong.Context
		deps   func(*introspectionSettingsFake, *introspectionAPIFake, *contextSpy) Dependencies
	}{
		{
			name: "partial environment",
			deps: func(service *introspectionSettingsFake, api *introspectionAPIFake, spy *contextSpy) Dependencies {
				return Dependencies{Admin: AdminDependencies{Settings: service, API: api}, Operations: testOperations(spy), lookupEnv: func(name string) (string, bool) { return "secret", name == "PUSHOVER_TOKEN" }}
			},
		},
		{
			name:   "invalid timeout",
			root:   rootCommand{Timeout: "not-a-duration"},
			parsed: timeoutContext(t, "not-a-duration"),
			deps: func(service *introspectionSettingsFake, api *introspectionAPIFake, spy *contextSpy) Dependencies {
				return Dependencies{Admin: AdminDependencies{Settings: service, API: api}, Operations: testOperations(spy)}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &introspectionSettingsFake{adminSettingsFake: adminSettingsFake{token: []byte(strings.Repeat("t", domain.IdentifierLength))}}
			api := &introspectionAPIFake{}
			contexts := &contextSpy{}
			var stdout, stderr bytes.Buffer
			got := executeIntrospection(context.Background(), testIO(&stdout, &stderr), test.deps(service, api, contexts), test.root, test.parsed, "limits")
			if got != apperror.ExitUsage || stdout.Len() != 0 || service.selectedTokenCalls != 0 || api.limitCalls != 0 || contexts.calls != 0 {
				t.Fatalf("exit=%d stdout=%q token calls=%d API calls=%d contexts=%d", got, stdout.String(), service.selectedTokenCalls, api.limitCalls, contexts.calls)
			}
		})
	}
}

func TestInfoReportsMissingAndSelectedConfigurationWithoutSecrets(t *testing.T) {
	build := buildinfo.Info{Version: "1.2.3", Commit: "abc123", Date: "2026-07-17"}
	for _, test := range []struct {
		name  string
		root  rootCommand
		store *introspectionConfigStore
		want  string
	}{
		{
			name:  "missing configuration JSON",
			root:  rootCommand{JSON: true},
			store: &introspectionConfigStore{path: "/safe/config.yaml", err: config.ErrNotFound},
			want:  "{\"schema_version\":1,\"command\":\"info\",\"status\":\"success\",\"result\":{\"config_path\":\"/safe/config.yaml\",\"profile\":\"\",\"backend\":\"\",\"version\":\"1.2.3\",\"commit\":\"abc123\",\"build_date\":\"2026-07-17\",\"features\":[\"age-file\",\"e2ee\",\"native-secrets\"]}}\n",
		},
		{
			name:  "explicit profile plain",
			root:  rootCommand{Plain: true, Profile: "other"},
			store: &introspectionConfigStore{path: "/safe/config.yaml", value: config.Config{SchemaVersion: config.CurrentSchemaVersion, Backend: config.Backend{Type: "native"}, DefaultProfile: "default"}},
			want:  "/safe/config.yaml\tother\tnative\t1.2.3\tabc123\t2026-07-17\tage-file,e2ee,native-secrets\n",
		},
		{
			name:  "default profile human",
			root:  rootCommand{},
			store: &introspectionConfigStore{path: "/safe/config.yaml", value: config.Config{SchemaVersion: config.CurrentSchemaVersion, Backend: config.Backend{Type: "native"}, DefaultProfile: "default"}},
			want:  "config_path=/safe/config.yaml profile=default backend=native version=1.2.3 commit=abc123 build_date=2026-07-17 features=age-file,e2ee,native-secrets\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			factory := &introspectionSecretFactory{}
			api := &introspectionAPIFake{}
			var stdout, stderr bytes.Buffer
			got := executeIntrospection(context.Background(), testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Config: test.store, SecretFactory: factory, API: api, Build: build}}, test.root, nil, "info")
			if got != 0 || stdout.String() != test.want || factory.opens != 0 || api.soundCalls != 0 || api.limitCalls != 0 {
				t.Fatalf("exit=%d output=%q opens=%d API sounds=%d limits=%d stderr=%q", got, stdout.String(), factory.opens, api.soundCalls, api.limitCalls, stderr.String())
			}
		})
	}
}

func TestInfoHandlesControlSafePathsAndTypedFailures(t *testing.T) {
	build := buildinfo.Info{Version: "1.2.3", Commit: "abc", Date: "2026-07-17"}
	t.Run("control characters are safe", func(t *testing.T) {
		store := &introspectionConfigStore{path: "/safe\nconfig.yaml", value: config.Config{SchemaVersion: config.CurrentSchemaVersion, Backend: config.Backend{Type: "native"}, DefaultProfile: "default"}}
		var stdout, stderr bytes.Buffer
		got := executeIntrospection(context.Background(), testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Config: store, Build: build}}, rootCommand{}, nil, "info")
		if got != 0 || strings.Count(stdout.String(), "\n") != 1 || strings.Contains(stdout.String(), "/safe\n") || !strings.Contains(stdout.String(), "config_path=/safe config.yaml") {
			t.Fatalf("exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
		}
	})
	t.Run("configuration error remains typed", func(t *testing.T) {
		store := &introspectionConfigStore{path: "/safe/config.yaml", err: apperror.New("configuration_unavailable", apperror.CategoryConfig, "configuration is unavailable")}
		var stdout, stderr bytes.Buffer
		got := executeIntrospection(context.Background(), testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Config: store, Build: build}}, rootCommand{JSON: true}, nil, "info")
		if got != apperror.ExitConfiguration || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"code":"configuration_unavailable"`) {
			t.Fatalf("exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
		}
	})
}

func TestInfoRejectsEnvironmentTimeoutAndDeadlineBeforeConfig(t *testing.T) {
	for _, test := range []struct {
		name   string
		ctx    context.Context
		root   rootCommand
		parsed *kong.Context
		deps   func(*introspectionConfigStore, *contextSpy) Dependencies
		want   int
	}{
		{
			name: "partial environment",
			ctx:  context.Background(),
			deps: func(store *introspectionConfigStore, spy *contextSpy) Dependencies {
				return Dependencies{Admin: AdminDependencies{Config: store}, Operations: testOperations(spy), lookupEnv: func(name string) (string, bool) { return "secret", name == "PUSHOVER_USER_KEY" }}
			},
			want: apperror.ExitUsage,
		},
		{
			name:   "invalid timeout",
			ctx:    context.Background(),
			root:   rootCommand{Timeout: "not-a-duration"},
			parsed: timeoutContext(t, "not-a-duration"),
			deps: func(store *introspectionConfigStore, spy *contextSpy) Dependencies {
				return Dependencies{Admin: AdminDependencies{Config: store}, Operations: testOperations(spy)}
			},
			want: apperror.ExitUsage,
		},
		{
			name: "expired deadline",
			ctx:  canceledContext(),
			deps: func(store *introspectionConfigStore, spy *contextSpy) Dependencies {
				return Dependencies{Admin: AdminDependencies{Config: store}, Operations: testOperations(spy)}
			},
			want: apperror.ExitNetwork,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &introspectionConfigStore{path: "/safe/config.yaml"}
			contexts := &contextSpy{}
			var stdout, stderr bytes.Buffer
			got := executeIntrospection(test.ctx, testIO(&stdout, &stderr), test.deps(store, contexts), test.root, test.parsed, "info")
			if got != test.want || stdout.Len() != 0 || store.loads != 0 {
				t.Fatalf("exit=%d stdout=%q loads=%d stderr=%q", got, stdout.String(), store.loads, stderr.String())
			}
			if test.name == "expired deadline" && contexts.calls != 1 {
				t.Fatalf("deadline context starts=%d, want 1", contexts.calls)
			}
			if test.name != "expired deadline" && contexts.calls != 0 {
				t.Fatalf("contexts=%d, want 0", contexts.calls)
			}
		})
	}
}

func TestVersionIsRuntimeIndependentAndRendersExactContracts(t *testing.T) {
	build := buildinfo.Info{Version: "v1.2.3", Commit: "abc123", Date: "2026-07-17"}
	for _, test := range []struct {
		name string
		root rootCommand
		want string
	}{
		{name: "JSON", root: rootCommand{JSON: true, Timeout: "not-a-duration"}, want: "{\"schema_version\":1,\"command\":\"version\",\"status\":\"success\",\"result\":{\"version\":\"v1.2.3\",\"commit\":\"abc123\",\"build_date\":\"2026-07-17\"}}\n"},
		{name: "plain", root: rootCommand{Plain: true, Timeout: "not-a-duration"}, want: "v1.2.3\tabc123\t2026-07-17\n"},
		{name: "human", root: rootCommand{Timeout: "not-a-duration"}, want: "version=v1.2.3 commit=abc123 build_date=2026-07-17\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			deps := Dependencies{
				Admin:      AdminDependencies{Build: build, Config: &introspectionConfigStore{panicOnLoad: true}, API: &introspectionAPIFake{panicOnRead: true}},
				Operations: operation.Factory{Contexts: panicContexts{}},
				admin: func(context.Context) (AdminDependencies, error) {
					t.Fatal("version opened admin runtime")
					return AdminDependencies{}, errors.New("unreachable")
				},
				lookupEnv: func(string) (string, bool) {
					t.Fatal("version inspected environment")
					return "", false
				},
			}
			if got := executeVersion(context.Background(), testIO(&stdout, &stderr), deps, test.root); got != 0 || stdout.String() != test.want || stderr.Len() != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
			}
		})
	}
}

func TestExecuteIntrospectionGrammarAndOutputContracts(t *testing.T) {
	t.Run("sounds JSON", func(t *testing.T) {
		service := &introspectionSettingsFake{adminSettingsFake: adminSettingsFake{token: []byte(strings.Repeat("a", domain.IdentifierLength))}}
		api := &introspectionAPIFake{sounds: []pushover.SoundsResponse{{Sounds: []pushover.Sound{{Name: "zeta", Description: "Z"}, {Name: "alpha", Description: "A"}}}}}
		var stdout, stderr bytes.Buffer
		got := Execute(context.Background(), []string{"--json", "sounds"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}})
		want := "{\"schema_version\":1,\"command\":\"sounds\",\"status\":\"success\",\"result\":{\"rows\":[{\"name\":\"alpha\",\"description\":\"A\"},{\"name\":\"zeta\",\"description\":\"Z\"}]}}\n"
		if got != 0 || stdout.String() != want || stderr.Len() != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
		}
	})
	t.Run("limits plain", func(t *testing.T) {
		service := &introspectionSettingsFake{adminSettingsFake: adminSettingsFake{token: []byte(strings.Repeat("a", domain.IdentifierLength))}}
		api := &introspectionAPIFake{limitResponses: []pushover.LimitsResponse{{Limit: 10000, Remaining: 9999, Reset: 1_800_000_000}}}
		var stdout, stderr bytes.Buffer
		got := Execute(context.Background(), []string{"--plain", "limits"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}})
		if got != 0 || stdout.String() != "10000\t9999\t1800000000\n" || stderr.Len() != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
		}
	})
	t.Run("typed API error", func(t *testing.T) {
		service := &introspectionSettingsFake{adminSettingsFake: adminSettingsFake{token: []byte(strings.Repeat("a", domain.IdentifierLength))}}
		api := &introspectionAPIFake{soundErrs: []error{apperror.New("api_rejected", apperror.CategoryValidation, "Pushover rejected the request")}}
		var stdout, stderr bytes.Buffer
		got := Execute(context.Background(), []string{"--json", "sounds"}, testIO(&stdout, &stderr), Dependencies{Admin: AdminDependencies{Settings: service, API: api}})
		if got != apperror.ExitValidation || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"command":"sounds"`) || !strings.Contains(stderr.String(), `"code":"api_rejected"`) {
			t.Fatalf("exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
		}
	})
	for _, test := range []struct {
		name string
		args []string
		path string
	}{
		{name: "unexpected sounds argument", args: []string{"--json", "sounds", "extra"}, path: "sounds"},
		{name: "unexpected limits argument", args: []string{"--json", "limits", "extra"}, path: "limits"},
		{name: "mode conflict", args: []string{"--json", "--plain", "info"}, path: "info"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := Execute(context.Background(), test.args, testIO(&stdout, &stderr), Dependencies{})
			if got != apperror.ExitUsage || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"command":"`+test.path+`"`) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
			}
			if test.name == "mode conflict" && !strings.Contains(stderr.String(), `"code":"output_mode_conflict"`) {
				t.Fatalf("mode conflict stderr=%q", stderr.String())
			}
		})
	}
}

func TestExecuteVersionHasNoRuntimeDependencies(t *testing.T) {
	build := buildinfo.Info{Version: "v1.2.3", Commit: "abc123", Date: "2026-07-17"}
	var stdout, stderr bytes.Buffer
	deps := Dependencies{
		Admin:      AdminDependencies{Build: build, Config: &introspectionConfigStore{panicOnLoad: true}, API: &introspectionAPIFake{panicOnRead: true}},
		Operations: operation.Factory{Contexts: panicContexts{}},
		admin: func(context.Context) (AdminDependencies, error) {
			t.Fatal("version opened admin runtime")
			return AdminDependencies{}, errors.New("unreachable")
		},
		lookupEnv: func(string) (string, bool) {
			t.Fatal("version inspected environment")
			return "", false
		},
	}
	got := Execute(context.Background(), []string{"--timeout", "not-a-duration", "--json", "version"}, testIO(&stdout, &stderr), deps)
	want := "{\"schema_version\":1,\"command\":\"version\",\"status\":\"success\",\"result\":{\"version\":\"v1.2.3\",\"commit\":\"abc123\",\"build_date\":\"2026-07-17\"}}\n"
	if got != 0 || stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
}

type introspectionSettingsFake struct {
	adminSettingsFake
	selected []string
	err      error
}

func (s *introspectionSettingsFake) SelectedProfileToken(ctx context.Context, name string) ([]byte, error) {
	s.selected = append(s.selected, name)
	if s.err != nil {
		return nil, s.err
	}
	return s.adminSettingsFake.SelectedProfileToken(ctx, name)
}

type introspectionAPIFake struct {
	sounds                       []pushover.SoundsResponse
	limitResponses               []pushover.LimitsResponse
	soundErrs, limitErrs         []error
	soundCalls, limitCalls       int
	soundTokens, limitTokens     []string
	soundContexts, limitContexts []context.Context
	panicOnRead                  bool
}

func (*introspectionAPIFake) Send(context.Context, domain.TargetPayload) (pushover.SendResponse, error) {
	return pushover.SendResponse{}, errors.New("unexpected send")
}
func (*introspectionAPIFake) ValidateRecipient(context.Context, string, string, []string) (pushover.RecipientValidation, error) {
	return pushover.RecipientValidation{}, errors.New("unexpected validation")
}
func (f *introspectionAPIFake) Sounds(ctx context.Context, token string) (pushover.SoundsResponse, error) {
	if f.panicOnRead {
		panic("version called sounds")
	}
	f.soundCalls++
	f.soundTokens = append(f.soundTokens, token)
	f.soundContexts = append(f.soundContexts, ctx)
	if len(f.soundErrs) > 0 {
		err := f.soundErrs[0]
		f.soundErrs = f.soundErrs[1:]
		return pushover.SoundsResponse{}, err
	}
	if len(f.sounds) == 0 {
		return pushover.SoundsResponse{}, nil
	}
	value := f.sounds[0]
	f.sounds = f.sounds[1:]
	return value, nil
}
func (f *introspectionAPIFake) Limits(ctx context.Context, token string) (pushover.LimitsResponse, error) {
	if f.panicOnRead {
		panic("version called limits")
	}
	f.limitCalls++
	f.limitTokens = append(f.limitTokens, token)
	f.limitContexts = append(f.limitContexts, ctx)
	if len(f.limitErrs) > 0 {
		err := f.limitErrs[0]
		f.limitErrs = f.limitErrs[1:]
		return pushover.LimitsResponse{}, err
	}
	if len(f.limitResponses) == 0 {
		return pushover.LimitsResponse{}, nil
	}
	value := f.limitResponses[0]
	f.limitResponses = f.limitResponses[1:]
	return value, nil
}

type introspectionConfigStore struct {
	path        string
	value       config.Config
	err         error
	loads       int
	panicOnLoad bool
}

func (s *introspectionConfigStore) Load(context.Context) (config.Config, error) {
	if s.panicOnLoad {
		panic("version loaded configuration")
	}
	s.loads++
	return s.value, s.err
}
func (*introspectionConfigStore) Commit(context.Context, config.Config) error {
	return errors.New("unexpected config commit")
}
func (s *introspectionConfigStore) Path() string { return s.path }

type introspectionSecretFactory struct{ opens int }

func (f *introspectionSecretFactory) Open(context.Context, config.Backend) (secrets.Store, error) {
	f.opens++
	return nil, errors.New("secret store must not open")
}

type panicContexts struct{}

func (panicContexts) WithDeadline(context.Context, time.Time) (context.Context, context.CancelFunc) {
	panic("version started an operation scope")
}

func timeoutContext(t *testing.T, value string) *kong.Context {
	t.Helper()
	var command struct {
		Timeout string `name:"timeout"`
	}
	parser, err := kong.New(&command)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.Parse([]string{"--timeout", value})
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func bytesZero(value []byte) bool {
	for _, b := range value {
		if b != 0 {
			return false
		}
	}
	return true
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
