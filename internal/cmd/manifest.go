package cmd

// manifest is intentionally static. Completion must never inspect profiles,
// aliases, configuration, or a secret store merely to suggest command words.
var manifest = struct {
	Commands    []string
	GlobalFlags []string
	SendFlags   []string
}{
	Commands:    []string{"send", "completion"},
	GlobalFlags: []string{"--profile", "--json", "--plain", "--no-color", "--timeout"},
	SendFlags: []string{
		"--to", "--title", "--device", "--priority", "--sound", "--timestamp", "--ttl", "--url", "--url-title",
		"--html", "--monospace", "--attachment", "--attachment-url", "--encrypted", "--retry", "--expire",
		"--validate", "--validate-network", "--dry-run", "--yes", "--stdin", "--file", "--credentials-stdin",
	},
}
