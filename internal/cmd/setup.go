package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/operation"
	"github.com/jurgenj136/pushover-cli/internal/pushover"
	"github.com/jurgenj136/pushover-cli/internal/settings"
)

type setupCommand struct{}

const (
	setupBackendLabel        = "Secret backend (native/file)"
	setupProfileLabel        = "Profile name"
	setupTokenLabel          = "Application token"
	setupPriorityLabel       = "Default priority (-2, -1, 0)"
	setupAliasLabel          = "Recipient alias"
	setupKindLabel           = "Recipient kind (optional)"
	setupDevicesLabel        = "Recipient devices (comma-separated, optional)"
	setupUserKeyLabel        = "Recipient user key"
	setupE2EEKeyLabel        = "E2EE key (optional)"
	setupValidationModeLabel = "Validation mode (online/offline)"
)

func executeSetup(ctx context.Context, streams IO, deps Dependencies, root rootCommand, parsed *kong.Context) int {
	const command = "setup"
	mode := selectedMode(root.JSON, root.Plain)
	if exit, rejected := rejectPlainMutation(streams, deps, root, command); rejected {
		return exit
	}
	runtime, admin, err := beginSetup(ctx, deps, root, parsed)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	defer runtime.Close()
	prompter, err := adminPrompter(deps)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	input, err := collectSetupInput(runtime, admin, prompter)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	defer wipeSetupSecrets(input)
	if err := validateSetupRecipients(runtime, admin.API, input); err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	if err := writeSetupSummary(streams, input); err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	if err := prompter.Confirm(runtime.Context(), PromptSummary{Action: "Initialize configuration"}); err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	service, err := setupSettings(runtime.Context(), admin, input.Backend)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	if _, err := service.Bootstrap(runtime.Context(), input); err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	return renderAdminResult(streams, deps, mode, root.NoColor, command, mutation(command, "configuration", input.Profile.Name, "initialized", nil))
}

func beginSetup(ctx context.Context, deps Dependencies, root rootCommand, parsed *kong.Context) (operation.Scope, AdminDependencies, error) {
	if err := deps.validateAdminEnvironment(); err != nil {
		return operation.Scope{}, AdminDependencies{}, err
	}
	timeout, err := parseTimeout(root.Timeout, flagSeen(parsed, "timeout"))
	if err != nil {
		return operation.Scope{}, AdminDependencies{}, err
	}
	scope, err := startScope(ctx, timeout, deps)
	if err != nil {
		return operation.Scope{}, AdminDependencies{}, err
	}
	fail := func(err error) (operation.Scope, AdminDependencies, error) {
		scope.Close()
		return operation.Scope{}, AdminDependencies{}, err
	}
	if err := scope.Context().Err(); err != nil {
		return fail(err)
	}
	admin, err := deps.openAdmin(scope.Context())
	if err != nil {
		return fail(err)
	}
	if admin.Config == nil {
		return scope, admin, nil
	}
	_, err = admin.Config.Load(scope.Context())
	if err == nil {
		return fail(apperror.New("configuration_already_exists", apperror.CategoryConfig, "configuration already exists"))
	}
	if !errors.Is(err, config.ErrNotFound) {
		return fail(err)
	}
	return scope, admin, nil
}

func collectSetupInput(scope operation.Scope, admin AdminDependencies, prompter Prompter) (input settings.SetupInput, err error) {
	var secretsToWipe [][]byte
	defer func() {
		if err != nil {
			for _, value := range secretsToWipe {
				wipePromptBytes(value)
			}
		}
	}()
	read := func(label string, secret bool) ([]byte, error) {
		value, readErr := prompter.Read(scope.Context(), Prompt{Label: label, Secret: secret})
		if secret {
			secretsToWipe = append(secretsToWipe, value)
		}
		return value, readErr
	}

	backendText, err := read(setupBackendLabel, false)
	if err != nil {
		return input, err
	}
	backend, err := parseSetupBackend(string(backendText))
	if err != nil {
		return input, err
	}
	profileText, err := read(setupProfileLabel, false)
	if err != nil {
		return input, err
	}
	profile := strings.TrimSpace(string(profileText))
	if !domain.IsSafeName(profile) {
		return input, setupValidationError("profile name is invalid")
	}
	token, err := read(setupTokenLabel, true)
	if err != nil {
		return input, err
	}
	if domain.ValidateIdentifier(string(token)) != nil {
		return input, setupValidationError("application token is invalid")
	}
	priorityText, err := read(setupPriorityLabel, false)
	if err != nil {
		return input, err
	}
	priority, err := parseSetupPriority(string(priorityText))
	if err != nil {
		return input, err
	}
	input = settings.SetupInput{Backend: backend, Profile: settings.ProfileInput{Name: profile, Token: token, Defaults: config.MessageDefaults{Priority: priority}}}

	aliases := make(map[string]bool)
	for {
		aliasText, readErr := read(setupAliasLabel, false)
		if readErr != nil {
			return input, readErr
		}
		alias := strings.TrimSpace(string(aliasText))
		if alias == "" {
			if len(input.Recipients) == 0 {
				return input, setupValidationError("at least one recipient is required")
			}
			return input, nil
		}
		if !domain.IsSafeName(alias) {
			return input, setupValidationError("recipient alias is invalid")
		}
		if aliases[alias] {
			return input, setupValidationError("recipient aliases must be unique")
		}
		aliases[alias] = true
		kindText, readErr := read(setupKindLabel, false)
		if readErr != nil {
			return input, readErr
		}
		kind, err := parseSetupKind(string(kindText))
		if err != nil {
			return input, err
		}
		devicesText, readErr := read(setupDevicesLabel, false)
		if readErr != nil {
			return input, readErr
		}
		devices, err := parseSetupDevices(string(devicesText))
		if err != nil {
			return input, err
		}
		if kind == domain.KindGroup && len(devices) != 0 {
			return input, setupValidationError("group recipients cannot specify devices")
		}
		userKey, readErr := read(setupUserKeyLabel, true)
		if readErr != nil {
			return input, readErr
		}
		if domain.ValidateIdentifier(string(userKey)) != nil {
			return input, setupValidationError("recipient key is invalid")
		}
		e2eeKey, readErr := read(setupE2EEKeyLabel, true)
		if readErr != nil {
			return input, readErr
		}
		if len(e2eeKey) != 0 && domain.ValidateE2EEKey(e2eeKey) != nil {
			return input, setupValidationError("recipient encryption key is invalid")
		}
		validationText, readErr := read(setupValidationModeLabel, false)
		if readErr != nil {
			return input, readErr
		}
		online, err := parseSetupValidationMode(string(validationText))
		if err != nil {
			return input, err
		}
		input.Recipients = append(input.Recipients, settings.RecipientInput{Alias: alias, Kind: kind, Devices: devices, UserKey: userKey, E2EEKey: e2eeKey, Validated: false})
		if !online {
			continue
		}
		// An empty value is the secure default. Online validation happens only
		// after every answer is staged, so a failed setup remains uncommitted.
		input.Recipients[len(input.Recipients)-1].Validated = true
	}
}

func parseSetupBackend(raw string) (config.Backend, error) {
	switch strings.TrimSpace(raw) {
	case "native":
		return config.Backend{Type: "native"}, nil
	case "file":
		return config.Backend{Type: "file", Metadata: map[string]string{"mode": "passphrase"}}, nil
	default:
		return config.Backend{}, setupValidationError("secret backend must be native or file")
	}
}

func parseSetupPriority(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}
	priority, err := strconv.Atoi(value)
	if err != nil || domain.ValidateSavedPriority(priority) != nil {
		return 0, setupValidationError("default priority must be -2, -1, or 0")
	}
	return priority, nil
}

func parseSetupKind(raw string) (domain.TargetKind, error) {
	value := strings.TrimSpace(raw)
	switch domain.TargetKind(value) {
	case domain.KindUnknown, domain.KindUser, domain.KindGroup, domain.KindTeamGroup:
		return domain.TargetKind(value), nil
	default:
		return "", setupValidationError("recipient kind is invalid")
	}
}

func parseSetupDevices(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	devices := make([]string, 0, len(parts))
	for _, part := range parts {
		device := strings.TrimSpace(part)
		if device == "" || domain.ValidateDevice(device) != nil {
			return nil, setupValidationError("recipient device is invalid")
		}
		devices = append(devices, device)
	}
	return devices, nil
}

func parseSetupValidationMode(raw string) (bool, error) {
	switch strings.TrimSpace(raw) {
	case "", "online":
		return true, nil
	case "offline":
		return false, nil
	default:
		return false, setupValidationError("validation mode must be online or offline")
	}
}

func validateSetupRecipients(scope operation.Scope, api pushover.Client, input settings.SetupInput) error {
	needsValidation := false
	for _, recipient := range input.Recipients {
		if recipient.Validated {
			needsValidation = true
			break
		}
	}
	if !needsValidation {
		return nil
	}
	if api == nil {
		return apperror.New("validation_unavailable", apperror.CategoryInternal, apperror.MessageInternal)
	}
	for index := range input.Recipients {
		recipient := &input.Recipients[index]
		if !recipient.Validated {
			continue
		}
		phase, cancel, err := scope.Phase(operation.PhaseValidation)
		if err != nil {
			return err
		}
		validation, err := api.ValidateRecipient(phase, string(input.Profile.Token), string(recipient.UserKey), append([]string(nil), recipient.Devices...))
		cancel()
		if err != nil {
			return err
		}
		if !validation.Valid {
			return apperror.New("recipient_validation_failed", apperror.CategoryValidation, "recipient validation failed")
		}
	}
	return nil
}

func setupSettings(ctx context.Context, admin AdminDependencies, backend config.Backend) (settings.Service, error) {
	if admin.Settings != nil {
		return admin.Settings, nil
	}
	if admin.Config == nil || admin.SecretFactory == nil {
		return nil, apperror.New("configuration_unavailable", apperror.CategoryConfig, "configuration is unavailable")
	}
	store, err := admin.SecretFactory.Open(ctx, backend)
	if err != nil {
		return nil, err
	}
	return &settings.Manager{Config: admin.Config, Secrets: store}, nil
}

func writeSetupSummary(streams IO, input settings.SetupInput) error {
	lines := []string{
		"backend=" + apperror.Sanitize(input.Backend.Type),
		"profile=" + apperror.Sanitize(input.Profile.Name),
		"default_priority=" + strconv.Itoa(input.Profile.Defaults.Priority),
		"recipients=" + strconv.Itoa(len(input.Recipients)),
	}
	for index, recipient := range input.Recipients {
		kind := string(recipient.Kind)
		if kind == "" {
			kind = "unknown"
		}
		devices := make([]string, 0, len(recipient.Devices))
		for _, device := range recipient.Devices {
			devices = append(devices, apperror.Sanitize(device))
		}
		prefix := "recipient[" + strconv.Itoa(index) + "]."
		lines = append(lines,
			prefix+"alias="+apperror.Sanitize(recipient.Alias),
			prefix+"kind="+apperror.Sanitize(kind),
			prefix+"devices="+strings.Join(devices, ","),
			prefix+"validated="+strconv.FormatBool(recipient.Validated),
			prefix+"e2ee_configured="+strconv.FormatBool(len(recipient.E2EEKey) != 0),
		)
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(streams.Err, line); err != nil {
			return err
		}
	}
	return nil
}

func wipeSetupSecrets(input settings.SetupInput) {
	wipePromptBytes(input.Profile.Token)
	for _, recipient := range input.Recipients {
		wipePromptBytes(recipient.UserKey)
		wipePromptBytes(recipient.E2EEKey)
	}
}

func setupValidationError(message string) error {
	return apperror.New("setup_invalid", apperror.CategoryValidation, message)
}
