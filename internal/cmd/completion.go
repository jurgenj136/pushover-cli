package cmd

import (
	"bytes"
	"strings"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/output"
)

func executeCompletion(streams IO, deps Dependencies, mode output.Mode, command completionCommand, noColor bool) int {
	if mode == output.Plain {
		return renderError(streams, deps, mode, "completion", apperror.New("plain_unsupported", apperror.CategoryUsage, "plain output is unsupported for this command"), noColor)
	}
	script, err := completionScript(command.Shell)
	if err != nil {
		return renderError(streams, deps, mode, "completion", asAppError(err), noColor)
	}
	renderer, err := newRenderer(streams, deps, mode, noColor, streams.Out)
	if err != nil {
		return renderError(streams, deps, mode, "completion", asAppError(err), noColor)
	}
	result := output.CompletionResult{Meta: output.Meta{Command: "completion", Status: "success"}, Shell: command.Shell, Script: script}
	var encoded bytes.Buffer
	if err := renderer.Result(&encoded, result); err != nil {
		return renderError(streams, deps, mode, "completion", asAppError(err), noColor)
	}
	if err := commitOutput(streams.Out, encoded.Bytes()); err != nil {
		return renderError(streams, deps, mode, "completion", asAppError(err), noColor)
	}
	return 0
}

func completionScript(shell string) (string, error) {
	commands := strings.Join(manifest.Commands, " ")
	globalFlags := strings.Join(manifest.GlobalFlags, " ")
	sendFlags := strings.Join(manifest.SendFlags, " ")
	profileCommands := strings.Join(manifest.ProfileCommands, " ")
	recipientCommands := strings.Join(manifest.RecipientCommands, " ")
	authCommands := strings.Join(manifest.AuthCommands, " ")
	profileAddFlags := strings.Join(manifest.ProfileAddFlags, " ")
	profileRemoveFlags := strings.Join(manifest.ProfileRemoveFlags, " ")
	recipientAddFlags := strings.Join(manifest.RecipientAddFlags, " ")
	recipientRemoveFlags := strings.Join(manifest.RecipientRemoveFlags, " ")
	completionShells := strings.Join(manifest.CompletionShells, " ")
	backendDestinations := strings.Join(manifest.BackendDestinations, " ")
	switch shell {
	case "bash":
		return "# bash completion for pushover\n" +
			"_pushover_path() {\n" +
			"  local index=1 word root=\"\" child=\"\" nested=\"\"\n" +
			"  while (( index < COMP_CWORD )); do\n" +
			"    word=\"${COMP_WORDS[index]}\"\n" +
			"    case \"${word}\" in\n" +
			"      --profile|--timeout) (( index += 2 )); continue ;;\n" +
			"      --profile=*|--timeout=*|--json|--plain|--no-color|--json=*|--plain=*|--no-color=*) (( index += 1 )); continue ;;\n" +
			"    esac\n" +
			"    if [[ -z \"${root}\" ]]; then\n" +
			"      root=\"${word}\"\n" +
			"    elif [[ ( \"${root}\" == profiles || \"${root}\" == recipients || \"${root}\" == auth ) && -z \"${child}\" ]]; then\n" +
			"      child=\"${word}\"\n" +
			"    elif [[ \"${root}\" == auth && \"${child}\" == backend && -z \"${nested}\" ]]; then\n" +
			"      nested=\"${word}\"\n" +
			"    fi\n" +
			"    (( index += 1 ))\n" +
			"  done\n" +
			"  case \"${root}:${child}:${nested}\" in\n" +
			"    ::) printf '%s\\n' root ;;\n" +
			"    send::) printf '%s\\n' send ;;\n" +
			"    profiles::) printf '%s\\n' profiles ;;\n" +
			"    profiles:add:) printf '%s\\n' profiles-add ;;\n" +
			"    profiles:remove:) printf '%s\\n' profiles-remove ;;\n" +
			"    profiles:*) printf '%s\\n' profiles-leaf ;;\n" +
			"    recipients::) printf '%s\\n' recipients ;;\n" +
			"    recipients:add:) printf '%s\\n' recipients-add ;;\n" +
			"    recipients:remove:) printf '%s\\n' recipients-remove ;;\n" +
			"    recipients:*) printf '%s\\n' recipients-leaf ;;\n" +
			"    auth::) printf '%s\\n' auth ;;\n" +
			"    auth:backend:) printf '%s\\n' auth-backend ;;\n" +
			"    auth:backend:set) printf '%s\\n' auth-backend-set ;;\n" +
			"    auth:*) printf '%s\\n' auth-leaf ;;\n" +
			"    completion::) printf '%s\\n' completion ;;\n" +
			"    *) printf '%s\\n' leaf ;;\n" +
			"  esac\n" +
			"}\n\n" +
			"_pushover() {\n" +
			"  local cur=\"${COMP_WORDS[COMP_CWORD]}\"\n" +
			"  local previous=\"\"\n" +
			"  local commands=\"" + commands + "\"\n" +
			"  local global_flags=\"" + globalFlags + "\"\n" +
			"  local send_flags=\"" + sendFlags + "\"\n" +
			"  local profile_commands=\"" + profileCommands + "\"\n" +
			"  local recipient_commands=\"" + recipientCommands + "\"\n" +
			"  local auth_commands=\"" + authCommands + "\"\n" +
			"  local profile_add_flags=\"" + profileAddFlags + "\"\n" +
			"  local profile_remove_flags=\"" + profileRemoveFlags + "\"\n" +
			"  local recipient_add_flags=\"" + recipientAddFlags + "\"\n" +
			"  local recipient_remove_flags=\"" + recipientRemoveFlags + "\"\n" +
			"  if (( COMP_CWORD > 1 )); then previous=\"${COMP_WORDS[COMP_CWORD - 1]}\"; fi\n" +
			"  case \"${previous}\" in --profile|--timeout) COMPREPLY=(); return 0 ;; esac\n" +
			"  case \"$(_pushover_path)\" in\n" +
			"    root) COMPREPLY=( $(compgen -W \"${commands} ${global_flags}\" -- \"${cur}\") ) ;;\n" +
			"    send) COMPREPLY=( $(compgen -W \"${send_flags} ${global_flags}\" -- \"${cur}\") ) ;;\n" +
			"    profiles) COMPREPLY=( $(compgen -W \"${profile_commands} ${global_flags}\" -- \"${cur}\") ) ;;\n" +
			"    profiles-add) COMPREPLY=( $(compgen -W \"${profile_add_flags} ${global_flags}\" -- \"${cur}\") ) ;;\n" +
			"    profiles-remove) COMPREPLY=( $(compgen -W \"${profile_remove_flags} ${global_flags}\" -- \"${cur}\") ) ;;\n" +
			"    recipients) COMPREPLY=( $(compgen -W \"${recipient_commands} ${global_flags}\" -- \"${cur}\") ) ;;\n" +
			"    recipients-add) COMPREPLY=( $(compgen -W \"${recipient_add_flags} ${global_flags}\" -- \"${cur}\") ) ;;\n" +
			"    recipients-remove) COMPREPLY=( $(compgen -W \"${recipient_remove_flags} ${global_flags}\" -- \"${cur}\") ) ;;\n" +
			"    auth) COMPREPLY=( $(compgen -W \"${auth_commands} ${global_flags}\" -- \"${cur}\") ) ;;\n" +
			"    auth-backend) COMPREPLY=( $(compgen -W \"set ${global_flags}\" -- \"${cur}\") ) ;;\n" +
			"    auth-backend-set) COMPREPLY=( $(compgen -W \"" + backendDestinations + " ${global_flags}\" -- \"${cur}\") ) ;;\n" +
			"    completion) COMPREPLY=( $(compgen -W \"" + completionShells + " ${global_flags}\" -- \"${cur}\") ) ;;\n" +
			"    *) COMPREPLY=( $(compgen -W \"${global_flags}\" -- \"${cur}\") ) ;;\n" +
			"  esac\n" +
			"}\n" +
			"complete -F _pushover pushover\n", nil
	case "zsh":
		return "#compdef pushover\n\n" +
			"_pushover_path() {\n" +
			"  local index=2 word root=\"\" child=\"\" nested=\"\"\n" +
			"  while (( index < CURRENT )); do\n" +
			"    word=\"${words[index]}\"\n" +
			"    case \"${word}\" in\n" +
			"      --profile|--timeout) (( index += 2 )); continue ;;\n" +
			"      --profile=*|--timeout=*|--json|--plain|--no-color|--json=*|--plain=*|--no-color=*) (( index += 1 )); continue ;;\n" +
			"    esac\n" +
			"    if [[ -z \"${root}\" ]]; then\n" +
			"      root=\"${word}\"\n" +
			"    elif [[ ( \"${root}\" == profiles || \"${root}\" == recipients || \"${root}\" == auth ) && -z \"${child}\" ]]; then\n" +
			"      child=\"${word}\"\n" +
			"    elif [[ \"${root}\" == auth && \"${child}\" == backend && -z \"${nested}\" ]]; then\n" +
			"      nested=\"${word}\"\n" +
			"    fi\n" +
			"    (( index += 1 ))\n" +
			"  done\n" +
			"  case \"${root}:${child}:${nested}\" in\n" +
			"    ::) print -r -- root ;;\n" +
			"    send::) print -r -- send ;;\n" +
			"    profiles::) print -r -- profiles ;;\n" +
			"    profiles:add:) print -r -- profiles-add ;;\n" +
			"    profiles:remove:) print -r -- profiles-remove ;;\n" +
			"    profiles:*) print -r -- profiles-leaf ;;\n" +
			"    recipients::) print -r -- recipients ;;\n" +
			"    recipients:add:) print -r -- recipients-add ;;\n" +
			"    recipients:remove:) print -r -- recipients-remove ;;\n" +
			"    recipients:*) print -r -- recipients-leaf ;;\n" +
			"    auth::) print -r -- auth ;;\n" +
			"    auth:backend:) print -r -- auth-backend ;;\n" +
			"    auth:backend:set) print -r -- auth-backend-set ;;\n" +
			"    auth:*) print -r -- auth-leaf ;;\n" +
			"    completion::) print -r -- completion ;;\n" +
			"    *) print -r -- leaf ;;\n" +
			"  esac\n" +
			"}\n\n" +
			"_pushover() {\n" +
			"  local -a commands global_flags send_flags profile_commands recipient_commands auth_commands\n" +
			"  local previous=\"${words[CURRENT - 1]}\" path\n" +
			"  commands=(" + commands + ")\n" +
			"  global_flags=(" + globalFlags + ")\n" +
			"  send_flags=(" + sendFlags + ")\n" +
			"  profile_commands=(" + profileCommands + ")\n" +
			"  recipient_commands=(" + recipientCommands + ")\n" +
			"  auth_commands=(" + authCommands + ")\n" +
			"  case \"${previous}\" in --profile|--timeout) return ;; esac\n" +
			"  path=\"$(_pushover_path)\"\n" +
			"  case \"${path}\" in\n" +
			"    root) _values 'pushover command' ${commands[@]} ${global_flags[@]} ;;\n" +
			"    send) _values 'send flag' ${send_flags[@]} ${global_flags[@]} ;;\n" +
			"    profiles) _values 'profiles command' ${profile_commands[@]} ${global_flags[@]} ;;\n" +
			"    profiles-add) _values 'profile add flag' " + profileAddFlags + " ${global_flags[@]} ;;\n" +
			"    profiles-remove) _values 'profile remove flag' " + profileRemoveFlags + " ${global_flags[@]} ;;\n" +
			"    recipients) _values 'recipients command' ${recipient_commands[@]} ${global_flags[@]} ;;\n" +
			"    recipients-add) _values 'recipient add flag' " + recipientAddFlags + " ${global_flags[@]} ;;\n" +
			"    recipients-remove) _values 'recipient remove flag' " + recipientRemoveFlags + " ${global_flags[@]} ;;\n" +
			"    auth) _values 'auth command' ${auth_commands[@]} ${global_flags[@]} ;;\n" +
			"    auth-backend) _values 'backend command' set ${global_flags[@]} ;;\n" +
			"    auth-backend-set) _values 'backend destination' " + backendDestinations + " ${global_flags[@]} ;;\n" +
			"    completion) _values 'shell' " + completionShells + " ${global_flags[@]} ;;\n" +
			"    *) _values 'global flag' ${global_flags[@]} ;;\n" +
			"  esac\n" +
			"}\n\n" +
			"_pushover \"$@\"\n", nil
	case "fish":
		return "# fish completion for pushover\n" +
			"function __fish_pushover_path\n" +
			"    set -l words (commandline -opc)\n" +
			"    set -e words[1]\n" +
			"    set -l index 1 root '' child '' nested ''\n" +
			"    while test $index -le (count $words)\n" +
			"        set -l word $words[$index]\n" +
			"        switch $word\n" +
			"            case --profile --timeout\n" +
			"                set index (math $index + 2)\n" +
			"                continue\n" +
			"            case '--profile=*' '--timeout=*' --json --plain --no-color '--json=*' '--plain=*' '--no-color=*'\n" +
			"                set index (math $index + 1)\n" +
			"                continue\n" +
			"        end\n" +
			"        if test -z \"$root\"\n" +
			"            set root $word\n" +
			"        else if test -z \"$child\"; and contains -- $root profiles recipients auth\n" +
			"            set child $word\n" +
			"        else if test \"$root\" = auth; and test \"$child\" = backend; and test -z \"$nested\"\n" +
			"            set nested $word\n" +
			"        end\n" +
			"        set index (math $index + 1)\n" +
			"    end\n" +
			"    switch \"$root:$child:$nested\"\n" +
			"        case '::'\n" +
			"            printf '%s\\n' root\n" +
			"        case 'send::'\n" +
			"            printf '%s\\n' send\n" +
			"        case 'profiles::'\n" +
			"            printf '%s\\n' profiles\n" +
			"        case 'profiles:add:'\n" +
			"            printf '%s\\n' profiles-add\n" +
			"        case 'profiles:remove:'\n" +
			"            printf '%s\\n' profiles-remove\n" +
			"        case 'profiles:*'\n" +
			"            printf '%s\\n' profiles-leaf\n" +
			"        case 'recipients::'\n" +
			"            printf '%s\\n' recipients\n" +
			"        case 'recipients:add:'\n" +
			"            printf '%s\\n' recipients-add\n" +
			"        case 'recipients:remove:'\n" +
			"            printf '%s\\n' recipients-remove\n" +
			"        case 'recipients:*'\n" +
			"            printf '%s\\n' recipients-leaf\n" +
			"        case 'auth::'\n" +
			"            printf '%s\\n' auth\n" +
			"        case 'auth:backend:'\n" +
			"            printf '%s\\n' auth-backend\n" +
			"        case 'auth:backend:set'\n" +
			"            printf '%s\\n' auth-backend-set\n" +
			"        case 'auth:*'\n" +
			"            printf '%s\\n' auth-leaf\n" +
			"        case 'completion::'\n" +
			"            printf '%s\\n' completion\n" +
			"        case '*'\n" +
			"            printf '%s\\n' leaf\n" +
			"    end\n" +
			"end\n\n" +
			"function __fish_pushover_path_is\n" +
			"    test (__fish_pushover_path) = \"$argv[1]\"\n" +
			"end\n\n" +
			"function __fish_pushover_global_value_pending\n" +
			"    set -l words (commandline -opc)\n" +
			"    contains -- \"$words[-1]\" --profile --timeout\n" +
			"end\n\n" +
			"complete -c pushover -n '__fish_pushover_path_is root; and not __fish_pushover_global_value_pending' -a '" + commands + "'\n" +
			fishFlags(manifest.GlobalFlags, "not __fish_pushover_global_value_pending") +
			fishFlags(manifest.SendFlags, "__fish_pushover_path_is send; and not __fish_pushover_global_value_pending") +
			"complete -c pushover -n '__fish_pushover_path_is profiles; and not __fish_pushover_global_value_pending' -a '" + profileCommands + "'\n" +
			fishFlags(manifest.ProfileAddFlags, "__fish_pushover_path_is profiles-add; and not __fish_pushover_global_value_pending") +
			fishFlags(manifest.ProfileRemoveFlags, "__fish_pushover_path_is profiles-remove; and not __fish_pushover_global_value_pending") +
			"complete -c pushover -n '__fish_pushover_path_is recipients; and not __fish_pushover_global_value_pending' -a '" + recipientCommands + "'\n" +
			fishFlags(manifest.RecipientAddFlags, "__fish_pushover_path_is recipients-add; and not __fish_pushover_global_value_pending") +
			fishFlags(manifest.RecipientRemoveFlags, "__fish_pushover_path_is recipients-remove; and not __fish_pushover_global_value_pending") +
			"complete -c pushover -n '__fish_pushover_path_is auth; and not __fish_pushover_global_value_pending' -a '" + authCommands + "'\n" +
			"complete -c pushover -n '__fish_pushover_path_is auth-backend; and not __fish_pushover_global_value_pending' -a 'set'\n" +
			"complete -c pushover -n '__fish_pushover_path_is auth-backend-set; and not __fish_pushover_global_value_pending' -a '" + backendDestinations + "'\n" +
			"complete -c pushover -n '__fish_pushover_path_is completion; and not __fish_pushover_global_value_pending' -a '" + completionShells + "'\n", nil
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
