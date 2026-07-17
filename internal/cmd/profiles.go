package cmd

import (
	"context"
	"time"

	"github.com/alecthomas/kong"
	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/config"
	"github.com/jurgenj136/pushover-cli/internal/domain"
	"github.com/jurgenj136/pushover-cli/internal/output"
	"github.com/jurgenj136/pushover-cli/internal/settings"
)

type profilesCommand struct {
	Add    profileAddCommand    `cmd:"" help:"Add an application profile."`
	List   profileListCommand   `cmd:"" help:"List application profiles."`
	Show   profileShowCommand   `cmd:"" help:"Show an application profile."`
	Use    profileUseCommand    `cmd:"" help:"Select the default application profile."`
	Remove profileRemoveCommand `cmd:"" help:"Remove an application profile."`
}

type profileAddCommand struct {
	Name     string   `arg:"" help:"Profile name."`
	Title    string   `name:"title" help:"Default message title."`
	URL      string   `name:"url" help:"Default HTTPS action URL."`
	URLTitle string   `name:"url-title" help:"Default action URL title."`
	Sound    string   `name:"sound" help:"Default notification sound."`
	Devices  []string `name:"device" help:"Default device (repeatable)."`
	Priority int      `name:"priority" help:"Default priority (-2 through 0)."`
	TTL      string   `name:"ttl" help:"Default time-to-live duration."`
}
type profileListCommand struct{}
type profileShowCommand struct {
	Name string `arg:"" help:"Profile name."`
}
type profileUseCommand struct {
	Name string `arg:"" help:"Profile name."`
}
type profileRemoveCommand struct {
	Name        string `arg:"" help:"Profile name."`
	ReplaceWith string `name:"replace-with" help:"Replacement default profile."`
	Force       bool   `name:"force" help:"Remove even when this is the default profile."`
	Yes         bool   `name:"yes" help:"Confirm non-interactively."`
}

func executeProfiles(ctx context.Context, streams IO, deps Dependencies, root rootCommand, parsed *kong.Context, command string) int {
	mode := selectedMode(root.JSON, root.Plain)
	if command == "profiles add" || command == "profiles use" || command == "profiles remove" {
		if exit, rejected := rejectPlainMutation(streams, deps, root, command); rejected {
			return exit
		}
	}
	if command == "profiles remove" && flagSeen(parsed, "replace-with") && flagSeen(parsed, "force") {
		return renderErrorWithColor(streams, deps, mode, command, apperror.New("profile_removal_options_conflict", apperror.CategoryUsage, "profile removal options cannot be combined"), root.NoColor)
	}
	if command == "profiles add" {
		return executeProfileAdd(ctx, streams, deps, root, parsed)
	}
	runtime, err := beginAdmin(ctx, deps, root, parsed)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	defer runtime.scope.Close()
	switch command {
	case "profiles list":
		views, err := runtime.settings.Profiles(runtime.scope.Context())
		if err != nil {
			return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
		}
		return renderAdminResult(streams, deps, mode, root.NoColor, command, output.ProfilesResult{Meta: output.Meta{Command: command, Status: "success"}, Rows: profileRows(views)})
	case "profiles show":
		view, err := runtime.settings.Profile(runtime.scope.Context(), root.Profiles.Show.Name)
		if err != nil {
			return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
		}
		return renderAdminResult(streams, deps, mode, root.NoColor, command, output.ProfilesResult{Meta: output.Meta{Command: command, Status: "success"}, Rows: profileRows([]settings.ProfileView{view})})
	case "profiles use":
		if err := runtime.settings.UseProfile(runtime.scope.Context(), root.Profiles.Use.Name); err != nil {
			return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
		}
		return renderAdminResult(streams, deps, mode, root.NoColor, command, mutation(command, "profile", root.Profiles.Use.Name, "selected", nil))
	case "profiles remove":
		return executeProfileRemove(streams, deps, root, runtime)
	default:
		return renderErrorWithColor(streams, deps, mode, command, apperror.New("invalid_command", apperror.CategoryUsage, "invalid command line"), root.NoColor)
	}
}

func executeProfileAdd(ctx context.Context, streams IO, deps Dependencies, root rootCommand, parsed *kong.Context) int {
	const command = "profiles add"
	mode := selectedMode(root.JSON, root.Plain)
	if !domain.IsSafeName(root.Profiles.Add.Name) {
		return renderErrorWithColor(streams, deps, mode, command, apperror.New("profile_invalid", apperror.CategoryValidation, "profile name is invalid"), root.NoColor)
	}
	defaults, err := profileDefaults(root.Profiles.Add, parsed)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
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
	token, err := prompter.Read(runtime.scope.Context(), Prompt{Label: "Application token", Secret: true})
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	defer wipePromptBytes(token)
	_, err = runtime.settings.AddProfile(runtime.scope.Context(), settings.ProfileInput{Name: root.Profiles.Add.Name, Token: token, Defaults: defaults})
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	return renderAdminResult(streams, deps, mode, root.NoColor, command, mutation(command, "profile", root.Profiles.Add.Name, "added", nil))
}

func executeProfileRemove(streams IO, deps Dependencies, root rootCommand, runtime adminRuntime) int {
	const command = "profiles remove"
	mode := selectedMode(root.JSON, root.Plain)
	input := root.Profiles.Remove
	options := settings.RemoveProfileOptions{ReplaceWith: input.ReplaceWith, Force: input.Force}
	impact, err := runtime.settings.PreviewRemoveProfile(runtime.scope.Context(), input.Name, options)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	if err := writeRemovalSummary(streams, impact); err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	if !input.Yes {
		prompter, err := adminPrompter(deps)
		if err != nil {
			return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
		}
		if err := prompter.Confirm(runtime.scope.Context(), PromptSummary{Action: "Remove profile", Subjects: []string{input.Name}}); err != nil {
			return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
		}
	}
	warnings, err := runtime.settings.RemoveProfile(runtime.scope.Context(), input.Name, options)
	if err != nil {
		return renderErrorWithColor(streams, deps, mode, command, asAppError(err), root.NoColor)
	}
	writeWarnings(streams, warnings)
	return renderAdminResult(streams, deps, mode, root.NoColor, command, mutation(command, "profile", input.Name, "removed", warnings))
}

func profileDefaults(input profileAddCommand, parsed *kong.Context) (config.MessageDefaults, error) {
	defaults := config.MessageDefaults{Title: input.Title, URL: input.URL, URLTitle: input.URLTitle, Sound: input.Sound, Devices: append([]string(nil), input.Devices...), Priority: input.Priority}
	if flagSeen(parsed, "ttl") {
		value, err := time.ParseDuration(input.TTL)
		if err != nil {
			return config.MessageDefaults{}, apperror.New("invalid_ttl", apperror.CategoryUsage, "ttl must be a duration")
		}
		defaults.TTL = value
	}
	if domain.ValidateSavedPriority(defaults.Priority) != nil || domain.ValidateMessage(domain.Message{Body: "x", Title: defaults.Title, URL: defaults.URL, URLTitle: defaults.URLTitle, Sound: defaults.Sound, Devices: defaults.Devices, Priority: defaults.Priority, TTL: defaults.TTL}) != nil {
		return config.MessageDefaults{}, apperror.New("profile_invalid", apperror.CategoryValidation, "profile defaults are invalid")
	}
	return defaults, nil
}

func profileRows(views []settings.ProfileView) []output.ProfileRow {
	rows := make([]output.ProfileRow, 0, len(views))
	for _, view := range views {
		rows = append(rows, output.ProfileRow{Name: view.Name, Default: view.Default, Priority: view.Defaults.Priority, Title: view.Defaults.Title, Sound: view.Defaults.Sound})
	}
	return rows
}

func mutation(command, resource, name, action string, warnings []string) output.MutationResult {
	return output.MutationResult{Meta: output.Meta{Command: command, Status: "success"}, Resource: resource, Name: name, Action: action, Warnings: append([]string(nil), warnings...)}
}
