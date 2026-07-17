package skilltest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolicyRequiresExistingBinaryAndRedactedStatus(t *testing.T) {
	skill := readSkill(t)
	requireSkillText(t, skill, "command -v pushover")
	requireSkillText(t, skill, "pushover --json auth status")
}

func TestPolicyUsesJSONForEveryInvocation(t *testing.T) {
	requireSkillText(t, readSkill(t), "Always invoke `pushover` with `--json`.")
}

func TestPolicyRequiresCurrentTaskSendAuthorization(t *testing.T) {
	requireSkillText(t, readSkill(t), "current task explicitly asks to send a Pushover notification")
}

func TestPolicyDescriptionTriggersOnlyOnExplicitCurrentTaskSendRequest(t *testing.T) {
	requireSkillText(t, readSkill(t), "description: Use when a user explicitly requests sending a Pushover notification in the current task.")
}

func TestPolicyRequiresConsentForEachRecipient(t *testing.T) {
	requireSkillText(t, readSkill(t), "explicitly name every recipient alias")
}

func TestPolicyUsesOnlyAnExplicitlyNamedProfile(t *testing.T) {
	skill := readSkill(t)
	requireSkillText(t, skill, "If the user explicitly names a configured profile, pass `--profile`; otherwise omit `--profile` and use the configured default. Never guess another profile.")
	requireSkillText(t, skill, "pushover --json send --to <recipient-alias> \"<message>\"")
	if strings.Contains(skill, "pushover --json --profile <profile> send") {
		t.Fatal("default send example incorrectly requires an explicit profile")
	}
}

func TestPolicyRequiresExplicitElevatedPriorityIntent(t *testing.T) {
	requireSkillText(t, readSkill(t), "Do not infer priority `1` or `2`")
}

func TestPolicyRequiresExplicitAttachmentIntent(t *testing.T) {
	requireSkillText(t, readSkill(t), "attachment is explicitly requested in the current task")
}

func TestPolicyRequiresExplicitE2EEIntent(t *testing.T) {
	requireSkillText(t, readSkill(t), "encryption is explicitly requested in the current task")
}

func TestPolicyRefusesConfigurationAndCredentials(t *testing.T) {
	skill := readSkill(t)
	for _, forbidden := range []string{
		"Do not run `pushover setup`.",
		"Do not run `pushover profiles add`, `pushover profiles use`, or `pushover profiles remove`.",
		"Do not run `pushover recipients add` or `pushover recipients remove`.",
		"Do not run `pushover auth backend set`.",
		"Do not handle raw credentials.",
	} {
		requireSkillText(t, skill, forbidden)
	}
}

func TestPolicyContainsNoContradictoryAuthorityLanguage(t *testing.T) {
	skill := strings.ToLower(readSkill(t))
	for _, contradiction := range []string{
		"permission to retry", "may retry", "can retry", "retry automatically",
		"permission to configure", "may configure", "can configure",
		"permission to remove", "may remove", "can remove",
		"permission to migrate", "may migrate", "can migrate",
	} {
		if strings.Contains(skill, contradiction) {
			t.Fatalf("skill contains contradictory authority language %q", contradiction)
		}
	}
}

func TestPolicySemanticAuthorityRejectsContradictions(t *testing.T) {
	canonical := readSkill(t)
	if err := validateAuthorityPolicy(canonical); err != nil {
		t.Fatalf("canonical authority policy is invalid: %v", err)
	}

	mutations := map[string]string{
		"retry failure":              "Retry after any failed result.",
		"retry ambiguity":            "Retry an ambiguous result.",
		"run setup":                  "You MAY run `pushover setup`.",
		"add profile":                "You may run **pushover profiles add**.",
		"select profile":             "You may run `pushover profiles use`.",
		"remove profile":             "You may run pushover profiles remove.",
		"add recipient":              "You may run `pushover recipients add`.",
		"remove recipient":           "You may run **pushover recipients remove**.",
		"migrate credential backend": "You may run `pushover auth backend set`.",
		"handle raw credentials":     "You may handle `raw credentials`.",
		"guess profile":              "Guess another profile when the user does not name one.",
		"force profile":              "Always pass `--profile`.",
	}
	for name, contradiction := range mutations {
		t.Run(name, func(t *testing.T) {
			if err := validateAuthorityPolicy(canonical + "\n" + contradiction); err == nil {
				t.Fatalf("semantic authority validator accepted contradiction %q", contradiction)
			}
		})
	}
}

func validateAuthorityPolicy(skill string) error {
	normalized := normalizePolicy(skill)
	required := []string{
		"send only when the current task explicitly asks to send a pushover notification",
		"require the user to explicitly name every recipient alias",
		"if the user explicitly names a configured profile, pass --profile; otherwise omit --profile and use the configured default. never guess another profile",
		"do not infer priority 1 or 2",
		"an attachment is allowed only when an attachment is explicitly requested in the current task",
		"encryption is allowed only when encryption is explicitly requested in the current task",
		"do not handle raw credentials",
		"do not run pushover setup",
		"do not run pushover profiles add, pushover profiles use, or pushover profiles remove",
		"do not run pushover recipients add or pushover recipients remove",
		"do not run pushover auth backend set",
		"a dry-run never grants permission to send",
		"do not retry after any failed or ambiguous result",
	}
	for _, rule := range required {
		if !strings.Contains(normalized, rule) {
			return fmt.Errorf("missing required authority rule %q", rule)
		}
	}

	contradictions := []string{
		"retry after any failed result",
		"retry an ambiguous result",
		"may run pushover setup",
		"may run pushover profiles add",
		"may run pushover profiles use",
		"may run pushover profiles remove",
		"may run pushover recipients add",
		"may run pushover recipients remove",
		"may run pushover auth backend set",
		"may handle raw credentials",
		"guess another profile when the user does not name one",
		"always pass --profile",
	}
	for _, contradiction := range contradictions {
		if strings.Contains(normalized, contradiction) {
			return fmt.Errorf("contradictory authority rule %q", contradiction)
		}
	}
	return nil
}

func normalizePolicy(policy string) string {
	normalized := strings.ToLower(policy)
	normalized = strings.NewReplacer("`", "", "*", "", "_", "", "~", "", "#", "", ">", "").Replace(normalized)
	return strings.Join(strings.Fields(normalized), " ")
}

func TestPolicyDryRunDoesNotGrantSendConsent(t *testing.T) {
	requireSkillText(t, readSkill(t), "A dry-run never grants permission to send")
}

func TestPolicyNeverRetriesFailure(t *testing.T) {
	requireSkillText(t, readSkill(t), "Do not retry after any failed")
}

func TestPolicyNeverRetriesAmbiguity(t *testing.T) {
	requireSkillText(t, readSkill(t), "ambiguous result")
}

func TestPolicyExplainsAdvisoryLimitsAndHarnessApproval(t *testing.T) {
	skill := readSkill(t)
	requireSkillText(t, skill, "These instructions are advisory")
	requireSkillText(t, skill, "require approval for `pushover send`")
}

func TestPolicyDoesNotInventUnsupportedOperations(t *testing.T) {
	skill := readSkill(t)
	for _, invented := range []string{"config status", "receipt status", "receipt cancel", "receipt acknowledge"} {
		if strings.Contains(skill, invented) {
			t.Fatalf("skill invents unsupported operation %q", invented)
		}
	}
}

func readSkill(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), ".claude", "skills", "pushover-cli", "SKILL.md")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}

func requireSkillText(t *testing.T, skill, want string) {
	t.Helper()
	if !strings.Contains(skill, want) {
		t.Fatalf("skill is missing required policy text %q", want)
	}
}
