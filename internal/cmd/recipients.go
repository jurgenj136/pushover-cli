package cmd

import (
	"context"
	"errors"

	"github.com/alecthomas/kong"
	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/operation"
	"github.com/jurgenj136/pushover-cli/internal/output"
	"github.com/jurgenj136/pushover-cli/internal/pushover"
	"github.com/jurgenj136/pushover-cli/internal/settings"
)

type recipientsCommand struct {
	Add      recipientAddCommand      `cmd:"" help:"Add a recipient alias."`
	List     recipientListCommand     `cmd:"" help:"List recipient aliases."`
	Show     recipientShowCommand     `cmd:"" help:"Show a recipient alias."`
	Validate recipientValidateCommand `cmd:"" help:"Validate recipient aliases remotely."`
	Remove   recipientRemoveCommand   `cmd:"" help:"Remove a recipient alias."`
}

type recipientAddCommand struct {
	Alias   string   `arg:"" help:"Recipient alias."`
	Kind    string   `name:"kind" help:"Optional recipient kind (user, group, or team-group)."`
	Devices []string `name:"device" help:"Device (repeatable)."`
	Offline bool     `name:"offline" help:"Save without remote validation."`
}
type recipientListCommand struct{}
type recipientShowCommand struct {
	Alias string `arg:"" help:"Recipient alias."`
}
type recipientValidateCommand struct {
	Aliases []string `arg:"" required:"" help:"Recipient aliases."`
}
type recipientRemoveCommand struct {
	Alias string `arg:"" help:"Recipient alias."`
	Yes   bool   `name:"yes" help:"Confirm non-interactively."`
}

func executeRecipients(ctx context.Context, streams IO, deps Dependencies, root rootCommand, parsed *kong.Context, command string) int {
	mode := selectedMode(root.JSON, root.Plain)
	if command == "recipients add" || command == "recipients remove" {
		if exit, rejected := rejectPlainMutation(streams, deps, root, command); rejected {
			return exit
		}
	}
	if command == "recipients validate" && duplicateAliases(root.Recipients.Validate.Aliases) {
		return renderErrorWithColor(streams, deps, mode, command, apperror.New("recipient_alias_duplicate", apperror.CategoryUsage, "recipient aliases must be unique"), root.NoColor)
	}
	if command == "recipients add" {
		return executeRecipientAdd(ctx, streams, deps, root, parsed)
	}
	runtime, err := beginAdmin(ctx, deps, root, parsed)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	defer runtime.scope.Close()
	switch command {
	case "recipients list":
		views, err := runtime.settings.Recipients(runtime.scope.Context())
		if err != nil {
			return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
		}
		return renderAdminResult(streams, deps, mode, root.NoColor, command, output.RecipientsResult{Meta: output.Meta{Command: command, Status: "success"}, Rows: recipientRows(views)})
	case "recipients show":
		view, err := runtime.settings.Recipient(runtime.scope.Context(), root.Recipients.Show.Alias)
		if err != nil {
			return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
		}
		return renderAdminResult(streams, deps, mode, root.NoColor, command, output.RecipientsResult{Meta: output.Meta{Command: command, Status: "success"}, Rows: recipientRows([]settings.RecipientView{view})})
	case "recipients validate":
		return executeRecipientValidate(streams, deps, root, runtime)
	case "recipients remove":
		return executeRecipientRemove(streams, deps, root, runtime)
	default:
		return renderErrorWithColor(streams, deps, mode, command, apperror.New("invalid_command", apperror.CategoryUsage, "invalid command line"), root.NoColor)
	}
}

func executeRecipientAdd(ctx context.Context, streams IO, deps Dependencies, root rootCommand, parsed *kong.Context) int {
	const command = "recipients add"
	mode := selectedMode(root.JSON, root.Plain)
	inputCommand := root.Recipients.Add
	if !domain.IsSafeName(inputCommand.Alias) {
		return renderErrorWithColor(streams, deps, mode, command, apperror.New("recipient_invalid", apperror.CategoryValidation, "recipient alias is invalid"), root.NoColor)
	}
	if kind := inputCommand.Kind; kind != "" && kind != string(domain.KindUser) && kind != string(domain.KindGroup) && kind != string(domain.KindTeamGroup) {
		return renderErrorWithColor(streams, deps, mode, command, apperror.New("recipient_invalid", apperror.CategoryValidation, "recipient kind is invalid"), root.NoColor)
	}
	if inputCommand.Kind == string(domain.KindGroup) && len(inputCommand.Devices) != 0 {
		return renderErrorWithColor(streams, deps, mode, command, apperror.New("recipient_invalid", apperror.CategoryValidation, "group recipients cannot specify devices"), root.NoColor)
	}
	runtime, err := beginAdmin(ctx, deps, root, parsed)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	defer runtime.scope.Close()
	prompter, err := adminPrompter(deps)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	userKey, err := prompter.Read(runtime.scope.Context(), Prompt{Label: "Recipient user key", Secret: true})
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	defer wipePromptBytes(userKey)
	e2eeKey, err := prompter.Read(runtime.scope.Context(), Prompt{Label: "E2EE key (optional)", Secret: true})
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	defer wipePromptBytes(e2eeKey)
	input := settings.RecipientInput{Alias: root.Recipients.Add.Alias, Kind: domain.TargetKind(root.Recipients.Add.Kind), Devices: append([]string(nil), root.Recipients.Add.Devices...), UserKey: userKey, E2EEKey: e2eeKey}
	if !root.Recipients.Add.Offline {
		validation, err := validateNewRecipient(runtime, deps, root.Profile, userKey, input.Devices)
		if err != nil {
			return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
		}
		if !validation.Valid {
			return renderErrorWithColor(streams, deps, mode, command, apperror.New("recipient_validation_failed", apperror.CategoryValidation, "recipient validation failed"), root.NoColor)
		}
		input.Validated = validation.Valid
	}
	if _, err := runtime.settings.AddRecipient(runtime.scope.Context(), input); err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	return renderAdminResult(streams, deps, mode, root.NoColor, command, mutation(command, "recipient", input.Alias, "added", nil))
}

func validateNewRecipient(runtime adminRuntime, deps Dependencies, profile string, userKey []byte, devices []string) (pushover.RecipientValidation, error) {
	admin, err := deps.openAdmin(runtime.scope.Context())
	if err != nil {
		return pushover.RecipientValidation{}, err
	}
	if admin.API == nil {
		return pushover.RecipientValidation{}, apperror.New("validation_unavailable", apperror.CategoryInternal, apperror.MessageInternal)
	}
	token, err := runtime.settings.SelectedProfileToken(runtime.scope.Context(), profile)
	if err != nil {
		return pushover.RecipientValidation{}, err
	}
	defer wipePromptBytes(token)
	phase, cancel, err := runtime.scope.Phase(operation.PhaseValidation)
	if err != nil {
		return pushover.RecipientValidation{}, err
	}
	defer cancel()
	return admin.API.ValidateRecipient(phase, string(token), string(userKey), devices)
}

func executeRecipientValidate(streams IO, deps Dependencies, root rootCommand, runtime adminRuntime) int {
	const command = "recipients validate"
	mode := selectedMode(root.JSON, root.Plain)
	admin, err := deps.openAdmin(runtime.scope.Context())
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	if admin.API == nil {
		return renderErrorWithColor(streams, deps, mode, command, apperror.New("validation_unavailable", apperror.CategoryInternal, apperror.MessageInternal), root.NoColor)
	}
	token, err := runtime.settings.SelectedProfileToken(runtime.scope.Context(), root.Profile)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	defer wipePromptBytes(token)
	rows := make([]output.ValidationRow, 0, len(root.Recipients.Validate.Aliases))
	for _, alias := range root.Recipients.Validate.Aliases {
		row, err := validateRecipient(runtime, admin.API, string(token), alias)
		if err != nil {
			return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
		}
		rows = append(rows, row)
	}
	return renderAdminResult(streams, deps, mode, root.NoColor, command, output.ValidationsResult{Meta: output.Meta{Command: command, Status: "success"}, Rows: rows})
}

func validateRecipient(runtime adminRuntime, api pushover.Client, token, alias string) (output.ValidationRow, error) {
	view, err := runtime.settings.Recipient(runtime.scope.Context(), alias)
	if err != nil {
		return output.ValidationRow{}, err
	}
	key, err := runtime.settings.RecipientUserKey(runtime.scope.Context(), alias)
	if err != nil {
		return output.ValidationRow{}, err
	}
	defer wipePromptBytes(key)
	phase, cancel, err := runtime.scope.Phase(operation.PhaseValidation)
	if err != nil {
		return output.ValidationRow{}, err
	}
	defer cancel()
	response, err := api.ValidateRecipient(phase, token, string(key), append([]string(nil), view.Devices...))
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return output.ValidationRow{}, err
	}
	if err != nil {
		return output.ValidationRow{Alias: alias, ErrorCode: apperror.SafeDetail(err).Code}, nil
	}
	return output.ValidationRow{Alias: alias, Valid: response.Valid, Devices: append([]string(nil), response.Devices...), Licenses: append([]string(nil), response.Licenses...)}, nil
}

func executeRecipientRemove(streams IO, deps Dependencies, root rootCommand, runtime adminRuntime) int {
	const command = "recipients remove"
	mode := selectedMode(root.JSON, root.Plain)
	alias := root.Recipients.Remove.Alias
	impact, err := runtime.settings.PreviewRemoveRecipient(runtime.scope.Context(), alias)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	if err := writeRemovalSummary(streams, impact); err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	if !root.Recipients.Remove.Yes {
		prompter, err := adminPrompter(deps)
		if err != nil {
			return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
		}
		if err := prompter.Confirm(runtime.scope.Context(), PromptSummary{Action: "Remove recipient", Subjects: []string{alias}}); err != nil {
			return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
		}
	}
	warnings, err := runtime.settings.RemoveRecipient(runtime.scope.Context(), alias)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	writeWarnings(streams, warnings)
	return renderAdminResult(streams, deps, mode, root.NoColor, command, mutation(command, "recipient", alias, "removed", warnings))
}

func duplicateAliases(aliases []string) bool {
	seen := make(map[string]bool, len(aliases))
	for _, alias := range aliases {
		if seen[alias] {
			return true
		}
		seen[alias] = true
	}
	return false
}

func recipientRows(views []settings.RecipientView) []output.RecipientRow {
	rows := make([]output.RecipientRow, 0, len(views))
	for _, view := range views {
		rows = append(rows, output.RecipientRow{Alias: view.Alias, Kind: view.Kind, Validated: view.Validated, Devices: append([]string(nil), view.Devices...)})
	}
	return rows
}
