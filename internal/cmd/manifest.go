package cmd

// manifest is intentionally static. Completion must never inspect profiles,
// aliases, configuration, or a secret store merely to suggest command words.
var manifest = struct {
	Commands, GlobalFlags, SendFlags                 []string
	ProfileCommands, RecipientCommands, AuthCommands []string
	ProfileAddFlags, ProfileRemoveFlags              []string
	RecipientAddFlags, RecipientRemoveFlags          []string
	CompletionShells, BackendDestinations            []string
}{
	Commands:    []string{"setup", "send", "profiles", "recipients", "sounds", "limits", "auth", "info", "version", "completion"},
	GlobalFlags: []string{"--profile", "--json", "--plain", "--no-color", "--timeout"},
	SendFlags: []string{
		"--to", "--title", "--device", "--priority", "--sound", "--timestamp", "--ttl", "--url", "--url-title",
		"--html", "--monospace", "--attachment", "--attachment-url", "--encrypted", "--retry", "--expire",
		"--validate", "--validate-network", "--dry-run", "--yes", "--stdin", "--file", "--credentials-stdin",
	},
	ProfileCommands:      []string{"add", "list", "show", "use", "remove"},
	RecipientCommands:    []string{"add", "list", "show", "validate", "remove"},
	AuthCommands:         []string{"status", "backend"},
	ProfileAddFlags:      []string{"--title", "--url", "--url-title", "--sound", "--device", "--priority", "--ttl"},
	ProfileRemoveFlags:   []string{"--replace-with", "--force", "--yes"},
	RecipientAddFlags:    []string{"--kind", "--device", "--offline"},
	RecipientRemoveFlags: []string{"--yes"},
	CompletionShells:     []string{"bash", "zsh", "fish"},
	BackendDestinations:  []string{"native", "file"},
}
