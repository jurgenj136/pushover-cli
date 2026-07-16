# bash completion for pushover
_pushover() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  local commands="send completion"
  local global_flags="--profile --json --plain --no-color --timeout"
  local send_flags="--to --title --device --priority --sound --timestamp --ttl --url --url-title --html --monospace --attachment --attachment-url --encrypted --retry --expire --validate --validate-network --dry-run --yes --stdin --file --credentials-stdin"
  if [[ ${COMP_CWORD} -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "${commands} ${global_flags}" -- "${cur}") )
    return 0
  fi
  case "${COMP_WORDS[1]}" in
    send) COMPREPLY=( $(compgen -W "${send_flags} ${global_flags}" -- "${cur}") ) ;;
    completion) COMPREPLY=( $(compgen -W "bash zsh fish" -- "${cur}") ) ;;
  esac
}
complete -F _pushover pushover
