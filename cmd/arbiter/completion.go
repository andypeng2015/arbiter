package main

import (
	"fmt"
	"sort"
	"strings"
)

// commandNames returns the subcommands (from commandList), sorted. It reads the
// static list rather than the handler map to avoid an initialization cycle.
func commandNames() []string {
	parts := strings.Split(commandList, ",")
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			names = append(names, s)
		}
	}
	sort.Strings(names)
	return names
}

// runCompletion prints a shell completion script for arbiter. The first
// argument selects the shell: bash, zsh, or fish.
func runCompletion(args []string) error {
	shell := "bash"
	if len(args) > 0 && args[0] != "" {
		shell = args[0]
	}
	cmds := strings.Join(commandNames(), " ")

	switch shell {
	case "bash":
		fmt.Printf(`# arbiter bash completion — eval "$(arbiter completion bash)"
_arbiter() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=( $(compgen -W "%s" -- "$cur") )
  else
    COMPREPLY=( $(compgen -f -- "$cur") )
  fi
}
complete -F _arbiter arbiter
`, cmds)
	case "zsh":
		fmt.Printf(`#compdef arbiter
# arbiter zsh completion — eval "$(arbiter completion zsh)"
_arbiter() {
  if (( CURRENT == 2 )); then
    compadd %s
  else
    _files
  fi
}
compdef _arbiter arbiter
`, cmds)
	case "fish":
		fmt.Printf(`# arbiter fish completion — arbiter completion fish > ~/.config/fish/completions/arbiter.fish
complete -c arbiter -n '__fish_use_subcommand' -a '%s'
complete -c arbiter -n 'not __fish_use_subcommand' -F
`, cmds)
	default:
		return usageError("Usage: arbiter completion <bash|zsh|fish>")
	}
	return nil
}
