# fish completion for pushover
complete -c pushover -n '__fish_use_subcommand' -a 'send completion'
complete -c pushover -l profile
complete -c pushover -l json
complete -c pushover -l plain
complete -c pushover -l no-color
complete -c pushover -l timeout
complete -c pushover -n '__fish_seen_subcommand_from send' -l to
complete -c pushover -n '__fish_seen_subcommand_from send' -l title
complete -c pushover -n '__fish_seen_subcommand_from send' -l device
complete -c pushover -n '__fish_seen_subcommand_from send' -l priority
complete -c pushover -n '__fish_seen_subcommand_from send' -l sound
complete -c pushover -n '__fish_seen_subcommand_from send' -l timestamp
complete -c pushover -n '__fish_seen_subcommand_from send' -l ttl
complete -c pushover -n '__fish_seen_subcommand_from send' -l url
complete -c pushover -n '__fish_seen_subcommand_from send' -l url-title
complete -c pushover -n '__fish_seen_subcommand_from send' -l html
complete -c pushover -n '__fish_seen_subcommand_from send' -l monospace
complete -c pushover -n '__fish_seen_subcommand_from send' -l attachment
complete -c pushover -n '__fish_seen_subcommand_from send' -l attachment-url
complete -c pushover -n '__fish_seen_subcommand_from send' -l encrypted
complete -c pushover -n '__fish_seen_subcommand_from send' -l retry
complete -c pushover -n '__fish_seen_subcommand_from send' -l expire
complete -c pushover -n '__fish_seen_subcommand_from send' -l validate
complete -c pushover -n '__fish_seen_subcommand_from send' -l validate-network
complete -c pushover -n '__fish_seen_subcommand_from send' -l dry-run
complete -c pushover -n '__fish_seen_subcommand_from send' -l yes
complete -c pushover -n '__fish_seen_subcommand_from send' -l stdin
complete -c pushover -n '__fish_seen_subcommand_from send' -l file
complete -c pushover -n '__fish_seen_subcommand_from send' -l credentials-stdin
complete -c pushover -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'
