package cmd

import (
	"bytes"
	"strings"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/output"
)

func executeCompletion(streams IO, deps Dependencies, mode output.Mode, command completionCommand) int {
	if mode == output.Plain {
		return renderError(streams, deps, mode, "completion", apperror.New("plain_unsupported", apperror.CategoryUsage, "plain output is unsupported for this command"))
	}
	script, err := completionScript(command.Shell)
	if err != nil {
		return renderError(streams, deps, mode, "completion", asAppError(err))
	}
	renderer, err := newRenderer(streams, deps, mode, false, streams.Out)
	if err != nil {
		return renderError(streams, deps, mode, "completion", asAppError(err))
	}
	result := output.CompletionResult{Meta: output.Meta{Command: "completion", Status: "success"}, Shell: command.Shell, Script: script}
	var encoded bytes.Buffer
	if err := renderer.Result(&encoded, result); err != nil {
		return renderError(streams, deps, mode, "completion", asAppError(err))
	}
	if err := commitOutput(streams.Out, encoded.Bytes()); err != nil {
		return renderError(streams, deps, mode, "completion", asAppError(err))
	}
	return 0
}

func completionScript(shell string) (string, error) {
	commands := strings.Join(manifest.Commands, " ")
	globalFlags := strings.Join(manifest.GlobalFlags, " ")
	sendFlags := strings.Join(manifest.SendFlags, " ")
	switch shell {
	case "bash":
		return "# bash completion for pushover\n" +
			"_pushover() {\n" +
			"  local cur=\"${COMP_WORDS[COMP_CWORD]}\"\n" +
			"  local commands=\"" + commands + "\"\n" +
			"  local global_flags=\"" + globalFlags + "\"\n" +
			"  local send_flags=\"" + sendFlags + "\"\n" +
			"  if [[ ${COMP_CWORD} -eq 1 ]]; then\n" +
			"    COMPREPLY=( $(compgen -W \"${commands} ${global_flags}\" -- \"${cur}\") )\n" +
			"    return 0\n" +
			"  fi\n" +
			"  case \"${COMP_WORDS[1]}\" in\n" +
			"    send) COMPREPLY=( $(compgen -W \"${send_flags} ${global_flags}\" -- \"${cur}\") ) ;;\n" +
			"    completion) COMPREPLY=( $(compgen -W \"bash zsh fish\" -- \"${cur}\") ) ;;\n" +
			"  esac\n" +
			"}\n" +
			"complete -F _pushover pushover\n", nil
	case "zsh":
		return "#compdef pushover\n\n" +
			"_pushover() {\n" +
			"  local -a commands global_flags send_flags\n" +
			"  commands=(" + commands + ")\n" +
			"  global_flags=(" + globalFlags + ")\n" +
			"  send_flags=(" + sendFlags + ")\n" +
			"  if (( CURRENT == 2 )); then\n" +
			"    _describe -t commands 'pushover command' commands\n" +
			"    return\n" +
			"  fi\n" +
			"  case \"${words[2]}\" in\n" +
			"    send) _describe -t flags 'send flag' send_flags ;;\n" +
			"    completion) _values 'shell' bash zsh fish ;;\n" +
			"  esac\n" +
			"}\n\n" +
			"_pushover \"$@\"\n", nil
	case "fish":
		return "# fish completion for pushover\n" +
			"complete -c pushover -n '__fish_use_subcommand' -a '" + commands + "'\n" +
			fishFlags(manifest.GlobalFlags, "") +
			fishFlags(manifest.SendFlags, "__fish_seen_subcommand_from send") +
			"complete -c pushover -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'\n", nil
	default:
		return "", apperror.New("completion_shell", apperror.CategoryUsage, "shell must be bash, zsh, or fish")
	}
}

func fishFlags(flags []string, condition string) string {
	var output strings.Builder
	for _, flag := range flags {
		output.WriteString("complete -c pushover")
		if condition != "" {
			output.WriteString(" -n '")
			output.WriteString(condition)
			output.WriteString("'")
		}
		output.WriteString(" -l ")
		output.WriteString(strings.TrimPrefix(flag, "--"))
		output.WriteByte('\n')
	}
	return output.String()
}
