package resolver

import (
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

func effectiveMessage(input Input, defaults config.MessageDefaults, recipient config.Recipient, body string) domain.Message {
	message := input.Message
	message.Body = body
	fields := input.MessageFields
	if !fields.Title {
		message.Title = defaults.Title
	}
	if !fields.URL {
		message.URL = defaults.URL
	}
	if !fields.URLTitle {
		message.URLTitle = defaults.URLTitle
	}
	if !fields.Sound {
		message.Sound = defaults.Sound
	}
	if !fields.Priority {
		message.Priority = defaults.Priority
	}
	if !fields.TTL {
		message.TTL = defaults.TTL
	}
	if !fields.Devices {
		message.Devices = append([]string(nil), defaults.Devices...)
		if len(recipient.Devices) > 0 {
			message.Devices = append([]string(nil), recipient.Devices...)
		}
	} else {
		message.Devices = append([]string(nil), message.Devices...)
	}
	return message
}
