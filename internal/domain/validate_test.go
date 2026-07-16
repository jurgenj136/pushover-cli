package domain

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPushoverContractFixture(t *testing.T) {
	bytes, err := os.ReadFile("testdata/pushover-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]json.RawMessage
	if err := json.Unmarshal(bytes, &fixture); err != nil {
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
	if err := json.Unmarshal(bytes, &numbers); err != nil {
		t.Fatal(err)
	}
	if numbers.Identifiers.Length != IdentifierLength || numbers.Fields.Message != MaxBodyRunes || numbers.Fields.Title != MaxTitleRunes || numbers.Fields.URL != MaxURLRunes || numbers.Fields.URLTitle != MaxURLTitleRunes || numbers.Attachment.Max != MaxAttachmentBytes {
		t.Fatal("fixture constants differ from centralized domain constants")
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
			err := ValidateTargetPayload(TargetPayload{Target: Target{Kind: test.kind, Validated: true}, Message: Message{Body: "body", Devices: []string{"phone"}}})
			if got := err == nil; got != test.ok {
				t.Fatalf("valid = %v, want %v (%v)", got, test.ok, err)
			}
		})
	}
}

func TestValidateTargetUnvalidatedGroupDefersDevices(t *testing.T) {
	err := ValidateTargetPayload(TargetPayload{Target: Target{Kind: KindGroup}, Message: Message{Body: "body", Devices: []string{"phone"}}})
	if err != nil {
		t.Fatalf("unvalidated group should defer device behavior: %v", err)
	}
}
