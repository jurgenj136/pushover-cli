package config

import "context"

// Clone returns a configuration that can be changed without mutating the
// value returned by Store.Load. Stores and callers may retain that value.
func Clone(value Config) Config {
	copy := value
	copy.Backend.Metadata = cloneStrings(value.Backend.Metadata)
	copy.Profiles = make(map[string]Profile, len(value.Profiles))
	for name, profile := range value.Profiles {
		profile.Defaults.Devices = append([]string(nil), profile.Defaults.Devices...)
		copy.Profiles[name] = profile
	}
	copy.Recipients = make(map[string]Recipient, len(value.Recipients))
	for alias, recipient := range value.Recipients {
		recipient.Devices = append([]string(nil), recipient.Devices...)
		copy.Recipients[alias] = recipient
	}
	return copy
}

func cloneStrings(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

// Transaction loads a private copy, applies mutate, then commits the result.
// It intentionally does not hide a CommitError: callers coordinating staged
// secrets must inspect OutcomeOf to decide whether rollback is safe.
func Transaction(ctx context.Context, store Store, mutate func(*Config) error) (Config, error) {
	before, err := store.Load(ctx)
	if err != nil {
		return Config{}, err
	}
	after := Clone(before)
	if err := mutate(&after); err != nil {
		return Config{}, err
	}
	if err := store.Commit(ctx, after); err != nil {
		return Config{}, err
	}
	return after, nil
}
