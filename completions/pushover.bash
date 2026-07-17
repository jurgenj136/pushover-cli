# bash completion for pushover
_pushover_path() {
  local index=1 word root="" child="" nested=""
  while (( index < COMP_CWORD )); do
    word="${COMP_WORDS[index]}"
    case "${word}" in
      --profile|--timeout) (( index += 2 )); continue ;;
      --profile=*|--timeout=*|--json|--plain|--no-color|--json=*|--plain=*|--no-color=*) (( index += 1 )); continue ;;
    esac
    if [[ -z "${root}" ]]; then
      root="${word}"
    elif [[ ( "${root}" == profiles || "${root}" == recipients || "${root}" == auth ) && -z "${child}" ]]; then
      child="${word}"
    elif [[ "${root}" == auth && "${child}" == backend && -z "${nested}" ]]; then
      nested="${word}"
    fi
    (( index += 1 ))
  done
  case "${root}:${child}:${nested}" in
    ::) printf '%s\n' root ;;
    send::) printf '%s\n' send ;;
    profiles::) printf '%s\n' profiles ;;
    profiles:add:) printf '%s\n' profiles-add ;;
    profiles:remove:) printf '%s\n' profiles-remove ;;
    profiles:*) printf '%s\n' profiles-leaf ;;
    recipients::) printf '%s\n' recipients ;;
    recipients:add:) printf '%s\n' recipients-add ;;
    recipients:remove:) printf '%s\n' recipients-remove ;;
    recipients:*) printf '%s\n' recipients-leaf ;;
    auth::) printf '%s\n' auth ;;
    auth:backend:) printf '%s\n' auth-backend ;;
    auth:backend:set) printf '%s\n' auth-backend-set ;;
    auth:*) printf '%s\n' auth-leaf ;;
    completion::) printf '%s\n' completion ;;
    *) printf '%s\n' leaf ;;
  esac
}

_pushover() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  local previous=""
  local commands="setup send profiles recipients sounds limits auth info version completion"
  local global_flags="--profile --json --plain --no-color --timeout"
  local send_flags="--to --title --device --priority --sound --timestamp --ttl --url --url-title --html --monospace --attachment --attachment-url --encrypted --retry --expire --validate --validate-network --dry-run --yes --stdin --file --credentials-stdin"
  local profile_commands="add list show use remove"
  local recipient_commands="add list show validate remove"
  local auth_commands="status backend"
  local profile_add_flags="--title --url --url-title --sound --device --priority --ttl"
  local profile_remove_flags="--replace-with --force --yes"
  local recipient_add_flags="--kind --device --offline"
  local recipient_remove_flags="--yes"
  if (( COMP_CWORD > 1 )); then previous="${COMP_WORDS[COMP_CWORD - 1]}"; fi
  case "${previous}" in --profile|--timeout) COMPREPLY=(); return 0 ;; esac
  case "$(_pushover_path)" in
    root) COMPREPLY=( $(compgen -W "${commands} ${global_flags}" -- "${cur}") ) ;;
    send) COMPREPLY=( $(compgen -W "${send_flags} ${global_flags}" -- "${cur}") ) ;;
    profiles) COMPREPLY=( $(compgen -W "${profile_commands} ${global_flags}" -- "${cur}") ) ;;
    profiles-add) COMPREPLY=( $(compgen -W "${profile_add_flags} ${global_flags}" -- "${cur}") ) ;;
    profiles-remove) COMPREPLY=( $(compgen -W "${profile_remove_flags} ${global_flags}" -- "${cur}") ) ;;
    recipients) COMPREPLY=( $(compgen -W "${recipient_commands} ${global_flags}" -- "${cur}") ) ;;
    recipients-add) COMPREPLY=( $(compgen -W "${recipient_add_flags} ${global_flags}" -- "${cur}") ) ;;
    recipients-remove) COMPREPLY=( $(compgen -W "${recipient_remove_flags} ${global_flags}" -- "${cur}") ) ;;
    auth) COMPREPLY=( $(compgen -W "${auth_commands} ${global_flags}" -- "${cur}") ) ;;
    auth-backend) COMPREPLY=( $(compgen -W "set ${global_flags}" -- "${cur}") ) ;;
    auth-backend-set) COMPREPLY=( $(compgen -W "native file ${global_flags}" -- "${cur}") ) ;;
    completion) COMPREPLY=( $(compgen -W "bash zsh fish ${global_flags}" -- "${cur}") ) ;;
    *) COMPREPLY=( $(compgen -W "${global_flags}" -- "${cur}") ) ;;
  esac
}
complete -F _pushover pushover
