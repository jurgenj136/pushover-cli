# fish completion for pushover
function __fish_pushover_path
    set -l words (commandline -opc)
    set -e words[1]
    set -l index 1
    set -l root ''
    set -l child ''
    set -l nested ''
    while test $index -le (count $words)
        set -l word $words[$index]
        switch $word
            case --profile --timeout
                set index (math $index + 2)
                continue
            case '--profile=*' '--timeout=*' --json --plain --no-color '--json=*' '--plain=*' '--no-color=*'
                set index (math $index + 1)
                continue
        end
        if test -z "$root"
            set root $word
        else if test -z "$child"; and contains -- $root profiles recipients auth
            set child $word
        else if test "$root" = auth; and test "$child" = backend; and test -z "$nested"
            set nested $word
        end
        set index (math $index + 1)
    end
    switch "$root:$child:$nested"
        case '::'
            printf '%s\n' root
        case 'send::'
            printf '%s\n' send
        case 'profiles::'
            printf '%s\n' profiles
        case 'profiles:add:'
            printf '%s\n' profiles-add
        case 'profiles:remove:'
            printf '%s\n' profiles-remove
        case 'profiles:*'
            printf '%s\n' profiles-leaf
        case 'recipients::'
            printf '%s\n' recipients
        case 'recipients:add:'
            printf '%s\n' recipients-add
        case 'recipients:remove:'
            printf '%s\n' recipients-remove
        case 'recipients:*'
            printf '%s\n' recipients-leaf
        case 'auth::'
            printf '%s\n' auth
        case 'auth:backend:'
            printf '%s\n' auth-backend
        case 'auth:backend:set'
            printf '%s\n' auth-backend-set
        case 'auth:*'
            printf '%s\n' auth-leaf
        case 'completion::'
            printf '%s\n' completion
        case '*'
            printf '%s\n' leaf
    end
end

function __fish_pushover_path_is
    test (__fish_pushover_path) = "$argv[1]"
end

function __fish_pushover_global_value_pending
    set -l words (commandline -opc)
    contains -- "$words[-1]" --profile --timeout
end

complete -c pushover -n '__fish_pushover_path_is root; and not __fish_pushover_global_value_pending' -a 'setup send profiles recipients sounds limits auth info version completion'
complete -c pushover -n 'not __fish_pushover_global_value_pending' -l profile
complete -c pushover -n 'not __fish_pushover_global_value_pending' -l json
complete -c pushover -n 'not __fish_pushover_global_value_pending' -l plain
complete -c pushover -n 'not __fish_pushover_global_value_pending' -l no-color
complete -c pushover -n 'not __fish_pushover_global_value_pending' -l timeout
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l to
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l title
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l device
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l priority
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l sound
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l timestamp
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l ttl
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l url
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l url-title
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l html
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l monospace
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l attachment
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l attachment-url
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l encrypted
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l retry
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l expire
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l validate
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l validate-network
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l dry-run
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l yes
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l stdin
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l file
complete -c pushover -n '__fish_pushover_path_is send; and not __fish_pushover_global_value_pending' -l credentials-stdin
complete -c pushover -n '__fish_pushover_path_is profiles; and not __fish_pushover_global_value_pending' -a 'add list show use remove'
complete -c pushover -n '__fish_pushover_path_is profiles-add; and not __fish_pushover_global_value_pending' -l title
complete -c pushover -n '__fish_pushover_path_is profiles-add; and not __fish_pushover_global_value_pending' -l url
complete -c pushover -n '__fish_pushover_path_is profiles-add; and not __fish_pushover_global_value_pending' -l url-title
complete -c pushover -n '__fish_pushover_path_is profiles-add; and not __fish_pushover_global_value_pending' -l sound
complete -c pushover -n '__fish_pushover_path_is profiles-add; and not __fish_pushover_global_value_pending' -l device
complete -c pushover -n '__fish_pushover_path_is profiles-add; and not __fish_pushover_global_value_pending' -l priority
complete -c pushover -n '__fish_pushover_path_is profiles-add; and not __fish_pushover_global_value_pending' -l ttl
complete -c pushover -n '__fish_pushover_path_is profiles-remove; and not __fish_pushover_global_value_pending' -l replace-with
complete -c pushover -n '__fish_pushover_path_is profiles-remove; and not __fish_pushover_global_value_pending' -l force
complete -c pushover -n '__fish_pushover_path_is profiles-remove; and not __fish_pushover_global_value_pending' -l yes
complete -c pushover -n '__fish_pushover_path_is recipients; and not __fish_pushover_global_value_pending' -a 'add list show validate remove'
complete -c pushover -n '__fish_pushover_path_is recipients-add; and not __fish_pushover_global_value_pending' -l kind
complete -c pushover -n '__fish_pushover_path_is recipients-add; and not __fish_pushover_global_value_pending' -l device
complete -c pushover -n '__fish_pushover_path_is recipients-add; and not __fish_pushover_global_value_pending' -l offline
complete -c pushover -n '__fish_pushover_path_is recipients-remove; and not __fish_pushover_global_value_pending' -l yes
complete -c pushover -n '__fish_pushover_path_is auth; and not __fish_pushover_global_value_pending' -a 'status backend'
complete -c pushover -n '__fish_pushover_path_is auth-backend; and not __fish_pushover_global_value_pending' -a 'set'
complete -c pushover -n '__fish_pushover_path_is auth-backend-set; and not __fish_pushover_global_value_pending' -a 'native file'
complete -c pushover -n '__fish_pushover_path_is completion; and not __fish_pushover_global_value_pending' -a 'bash zsh fish'
