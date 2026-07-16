package domain

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPushoverContractFixture(t *testing.T) {
	contents, err := os.ReadFile("testdata/pushover-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]json.RawMessage
	if err := json.Unmarshal(contents, &fixture); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"retrieved_at": true, "sources": true, "identifiers": true, "fields": true,
		"priority": true, "attachment": true, "quota_headers": true, "e2ee_fields": true,
	}
	if len(fixture) != len(want) {
		t.Fatalf("fixture keys = %d, want %d", len(fixture), len(want))
	}
	for key := range want {
		if _, ok := fixture[key]; !ok {
			t.Fatalf("fixture missing %q", key)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var typed map[string]any
	if err := decoder.Decode(&typed); err != nil {
		t.Fatal(err)
	}
	assertIntegerJSONNumbers(t, typed)
	retrievedAt, ok := typed["retrieved_at"].(string)
	if !ok {
		t.Fatalf("retrieved_at has type %T, want string", typed["retrieved_at"])
	}
	if _, err := time.Parse("2006-01-02", retrievedAt); err != nil {
		t.Fatalf("retrieved_at is not an ISO date: %q", retrievedAt)
	}
	sources, ok := typed["sources"].([]any)
	if !ok || len(sources) == 0 {
		t.Fatal("fixture sources must be a non-empty array")
	}
	for _, raw := range sources {
		source, ok := raw.(string)
		if !ok {
			t.Fatalf("fixture source has type %T, want string", raw)
		}
		parsed, err := url.Parse(source)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "pushover.net" || parsed.User != nil || parsed.String() != source {
			t.Fatalf("fixture source is not a canonical Pushover HTTPS URL: %q", source)
		}
	}
	var numbers struct {
		Identifiers struct {
			Length int `json:"length"`
		} `json:"identifiers"`
		Fields struct {
			Message  int `json:"message_max_runes"`
			Title    int `json:"title_max_runes"`
			URL      int `json:"url_max_runes"`
			URLTitle int `json:"url_title_max_runes"`
		} `json:"fields"`
		Attachment struct {
			Max int `json:"max_bytes"`
		} `json:"attachment"`
	}
	if err := json.Unmarshal(contents, &numbers); err != nil {
		t.Fatal(err)
	}
	if numbers.Identifiers.Length != IdentifierLength || numbers.Fields.Message != MaxBodyRunes || numbers.Fields.Title != MaxTitleRunes || numbers.Fields.URL != MaxURLRunes || numbers.Fields.URLTitle != MaxURLTitleRunes || numbers.Attachment.Max != MaxAttachmentBytes {
		t.Fatal("fixture constants differ from centralized domain constants")
	}
	assertFixtureValue(t, typed, []string{"fields", "device_max_runes"}, MaxDeviceRunes)
	assertFixtureValue(t, typed, []string{"priority", "minimum"}, PriorityLowest)
	assertFixtureValue(t, typed, []string{"priority", "maximum"}, PriorityEmergency)
	assertFixtureValue(t, typed, []string{"priority", "emergency_retry_min_seconds"}, int(MinEmergencyRetry/time.Second))
	assertFixtureValue(t, typed, []string{"priority", "emergency_expire_max_seconds"}, int(MaxEmergencyExpire/time.Second))
	assertFixtureValue(t, typed, []string{"priority", "emergency_delivery_cap"}, MaxEmergencyRetries)
	if got := typed["e2ee_fields"]; !reflect.DeepEqual(got, []any{"message", "title", "url", "url_title"}) {
		t.Fatalf("e2ee_fields = %#v", got)
	}
	wantQuota := map[string]any{
		"limit": "X-Limit-App-Limit", "remaining": "X-Limit-App-Remaining", "reset": "X-Limit-App-Reset",
	}
	if got := typed["quota_headers"]; !reflect.DeepEqual(got, wantQuota) {
		t.Fatalf("quota_headers = %#v, want %#v", got, wantQuota)
	}
}

func assertIntegerJSONNumbers(t *testing.T, value any) {
	t.Helper()
	switch value := value.(type) {
	case map[string]any:
		for _, child := range value {
			assertIntegerJSONNumbers(t, child)
		}
	case []any:
		for _, child := range value {
			assertIntegerJSONNumbers(t, child)
		}
	case json.Number:
		if _, err := strconv.ParseInt(string(value), 10, 64); err != nil {
			t.Fatalf("fixture number %q is not a JSON integer", value)
		}
	}
}

func assertFixtureValue(t *testing.T, fixture map[string]any, path []string, want int) {
	t.Helper()
	var current any = fixture
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("fixture path %v is not an object", path)
		}
		current = object[part]
	}
	number, ok := current.(json.Number)
	if !ok || number.String() != strconv.Itoa(want) {
		t.Fatalf("fixture path %v = %#v, want integer %d", path, current, want)
	}
}

func TestValidateIdentifier(t *testing.T) {
	valid := strings.Repeat("a", IdentifierLength)
	for _, test := range []struct {
		name, value string
		want        bool
	}{
		{"valid", valid, true},
		{"short", valid[:IdentifierLength-1], false},
		{"upper allowed", strings.Repeat("A", IdentifierLength), true},
		{"punctuation", strings.Repeat("a", IdentifierLength-1) + "-", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidateIdentifier(test.value) == nil; got != test.want {
				t.Fatalf("ValidateIdentifier(%q) valid = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func TestValidateCredentials(t *testing.T) {
	valid := strings.Repeat("A", IdentifierLength)
	if err := ValidateCredentials(Credentials{Token: valid, UserKey: valid}); err != nil {
		t.Fatalf("valid credentials rejected: %v", err)
	}
	for _, credentials := range []Credentials{{Token: "short", UserKey: valid}, {Token: valid, UserKey: "short"}} {
		if err := ValidateCredentials(credentials); err == nil {
			t.Fatal("invalid credentials accepted")
		}
	}
}

func TestValidateE2EEKey(t *testing.T) {
	for _, test := range []struct {
		name string
		key  []byte
		ok   bool
	}{
		{"lower hex", []byte(strings.Repeat("ab", 32)), true},
		{"upper hex", []byte(strings.Repeat("AF", 32)), true},
		{"missing", nil, false},
		{"short", []byte(strings.Repeat("a", 63)), false},
		{"non hex", []byte(strings.Repeat("z", 64)), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidateE2EEKey(test.key) == nil; got != test.ok {
				t.Fatalf("valid = %v, want %v", got, test.ok)
			}
		})
	}
}

func TestValidateDevice(t *testing.T) {
	for _, test := range []struct {
		name, device string
		want         bool
	}{
		{"empty allowed", "", true},
		{"letters numbers punctuation", "phone_1-home", true},
		{"limit", strings.Repeat("a", MaxDeviceRunes), true},
		{"too long", strings.Repeat("a", MaxDeviceRunes+1), false},
		{"space", "my phone", false},
		{"control", "phone\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidateDevice(test.device) == nil; got != test.want {
				t.Fatalf("ValidateDevice(%q) valid = %v, want %v", test.device, got, test.want)
			}
		})
	}
}

func TestValidateMessageRules(t *testing.T) {
	valid := Message{Body: "body", Title: "title", URL: "https://example.com", URLTitle: "open", Priority: 0}
	for _, test := range []struct {
		name string
		msg  Message
		want bool
	}{
		{"valid", valid, true},
		{"body rune limit", Message{Body: strings.Repeat("界", MaxBodyRunes+1)}, false},
		{"body required", Message{}, false},
		{"title rune limit", Message{Body: "body", Title: strings.Repeat("界", MaxTitleRunes+1)}, false},
		{"url rune limit", Message{Body: "body", URL: "https://" + strings.Repeat("a", MaxURLRunes)}, false},
		{"url title rune limit", Message{Body: "body", URL: "https://example.com", URLTitle: strings.Repeat("界", MaxURLTitleRunes+1)}, false},
		{"priority range", Message{Body: "body", Priority: 3}, false},
		{"ttl positive", Message{Body: "body", TTL: -time.Second}, false},
		{"ttl and emergency", Message{Body: "body", Priority: PriorityEmergency, TTL: time.Second, Retry: 30 * time.Second, Expire: time.Hour}, false},
		{"http url", Message{Body: "body", URL: "http://example.com"}, false},
		{"url title requires url", Message{Body: "body", URLTitle: "open"}, false},
		{"html and monospace", Message{Body: "body", HTML: true, Monospace: true}, false},
		{"encrypted attachment", Message{Body: "body", Encrypted: true, Attachment: &Attachment{}}, false},
		{"encrypted html", Message{Body: "body", Encrypted: true, HTML: true}, false},
		{"encrypted monospace", Message{Body: "body", Encrypted: true, Monospace: true}, false},
		{"attachment maximum", Message{Body: "body", Attachment: &Attachment{Bytes: make([]byte, MaxAttachmentBytes+1)}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidateMessage(test.msg) == nil; got != test.want {
				t.Fatalf("ValidateMessage valid = %v, want %v: %+v", got, test.want, test.msg)
			}
		})
	}
}

func TestValidateEmergencyAndDefaults(t *testing.T) {
	for _, test := range []struct {
		name string
		msg  Message
		want bool
	}{
		{"minimum retry and maximum expire", Message{Body: "body", Priority: PriorityEmergency, Retry: 30 * time.Second, Expire: 3 * time.Hour}, true},
		{"retry required", Message{Body: "body", Priority: PriorityEmergency, Expire: time.Hour}, false},
		{"expire required", Message{Body: "body", Priority: PriorityEmergency, Retry: 30 * time.Second}, false},
		{"retry too short", Message{Body: "body", Priority: PriorityEmergency, Retry: 29 * time.Second, Expire: time.Hour}, false},
		{"expire too long", Message{Body: "body", Priority: PriorityEmergency, Retry: 30 * time.Second, Expire: 3*time.Hour + time.Second}, false},
		{"retry is ignored outside emergency", Message{Body: "body", Priority: 0, Retry: time.Second}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidateMessage(test.msg) == nil; got != test.want {
				t.Fatalf("ValidateMessage valid = %v, want %v", got, test.want)
			}
		})
	}
	if err := ValidateSavedPriority(PriorityHigh); err == nil {
		t.Fatal("high priority should not be saveable")
	}
	if err := ValidateSavedPriority(PriorityNormal); err != nil {
		t.Fatalf("normal priority should be saveable: %v", err)
	}
}

func TestEffectiveRetryCount(t *testing.T) {
	for _, test := range []struct {
		retry, expire time.Duration
		want          int
	}{
		{30 * time.Second, 30 * time.Second, 1},
		{30 * time.Second, 31 * time.Second, 2},
		{60 * time.Second, 61 * time.Second, 2},
		{30 * time.Second, 3 * time.Hour, 50},
		{0, time.Hour, 0},
	} {
		if got := EffectiveRetryCount(test.retry, test.expire); got != test.want {
			t.Fatalf("EffectiveRetryCount(%s, %s) = %d, want %d", test.retry, test.expire, got, test.want)
		}
	}
}

func TestValidateTargetGroupDeviceRule(t *testing.T) {
	credentials := validCredentials()
	for _, test := range []struct {
		name string
		kind TargetKind
		ok   bool
	}{
		{"unknown defers", KindUnknown, true},
		{"user allows", KindUser, true},
		{"ordinary group rejects", KindGroup, false},
		{"team group allows", KindTeamGroup, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateTargetPayload(TargetPayload{Target: Target{Credentials: credentials, Kind: test.kind, Validated: true}, Message: Message{Body: "body", Devices: []string{"phone"}}})
			if got := err == nil; got != test.ok {
				t.Fatalf("valid = %v, want %v (%v)", got, test.ok, err)
			}
		})
	}
}

func TestValidateTargetOnlyUnknownKindDefersDevices(t *testing.T) {
	credentials := validCredentials()
	if err := ValidateTargetPayload(TargetPayload{Target: Target{Credentials: credentials, Kind: KindGroup}, Message: Message{Body: "body", Devices: []string{"phone"}}}); err == nil {
		t.Fatal("explicit group kind must reject devices even if not remotely validated")
	}
	if err := ValidateTargetPayload(TargetPayload{Target: Target{Credentials: credentials, Kind: KindUnknown}, Message: Message{Body: "body", Devices: []string{"phone"}}}); err != nil {
		t.Fatalf("unknown kind should defer device behavior: %v", err)
	}
}

func TestValidateEncryptedTargetRequiresValidKey(t *testing.T) {
	credentials := validCredentials()
	message := Message{Body: "body", Encrypted: true}
	for _, test := range []struct {
		name string
		key  []byte
		ok   bool
	}{
		{"valid", []byte(strings.Repeat("a1", 32)), true},
		{"missing", nil, false},
		{"malformed", []byte(strings.Repeat("x", 64)), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateTargetPayload(TargetPayload{Target: Target{Credentials: credentials, E2EEKey: test.key}, Message: message})
			if got := err == nil; got != test.ok {
				t.Fatalf("valid = %v, want %v (%v)", got, test.ok, err)
			}
		})
	}
}

func validCredentials() Credentials {
	value := strings.Repeat("A", IdentifierLength)
	return Credentials{Token: value, UserKey: value}
}
