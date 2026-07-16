package config

import (
	"fmt"
	"strings"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

func Validate(value Config) error {
	if value.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported schema version %d", value.SchemaVersion)
	}
	if value.Backend.Type != "native" && value.Backend.Type != "file" {
		return fmt.Errorf("backend type is unsupported")
	}
	for key, metadata := range value.Backend.Metadata {
		if !domain.IsSafeName(key) || strings.ContainsFunc(metadata, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return fmt.Errorf("backend metadata is invalid")
		}
	}
	if value.DefaultProfile != "" {
		if _, ok := value.Profiles[value.DefaultProfile]; !ok {
			return fmt.Errorf("default profile does not exist")
		}
	}
	for name, profile := range value.Profiles {
		if !domain.IsSafeName(name) || profile.Name != name {
			return fmt.Errorf("profile name is invalid")
		}
		if !validRef(profile.TokenRef) {
			return fmt.Errorf("profile token reference is invalid")
		}
		if err := validateDefaults(profile.Defaults); err != nil {
			return err
		}
	}
	for alias, recipient := range value.Recipients {
		if !domain.IsSafeName(alias) || recipient.Alias != alias {
			return fmt.Errorf("recipient alias is invalid")
		}
		if !validRef(recipient.UserKeyRef) || (recipient.E2EEKeyRef != "" && !validRef(recipient.E2EEKeyRef)) {
			return fmt.Errorf("recipient secret reference is invalid")
		}
		if recipient.Kind != domain.KindUnknown && recipient.Kind != domain.KindUser && recipient.Kind != domain.KindGroup && recipient.Kind != domain.KindTeamGroup {
			return fmt.Errorf("recipient kind is invalid")
		}
		if recipient.Kind == domain.KindGroup && len(recipient.Devices) > 0 {
			return fmt.Errorf("group recipient cannot have devices")
		}
		for _, device := range recipient.Devices {
			if err := domain.ValidateDevice(device); err != nil {
				return err
			}
		}
	}
	return nil
}

func validRef(ref domain.SecretRef) bool { return domain.IsSafeName(string(ref)) }

func validateDefaults(defaults MessageDefaults) error {
	if err := domain.ValidateSavedPriority(defaults.Priority); err != nil {
		return err
	}
	return domain.ValidateMessage(domain.Message{Body: "x", Title: defaults.Title, URL: defaults.URL, URLTitle: defaults.URLTitle, Sound: defaults.Sound, Devices: defaults.Devices, Priority: defaults.Priority, TTL: defaults.TTL})
}
