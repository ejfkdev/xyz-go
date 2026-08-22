package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (a *App) printCompletion(out, errOut io.Writer, shell string) int {
	bin := filepath.Base(os.Args[0])
	if bin == "" || bin == "." {
		bin = "app"
	}
	names := map[string]bool{}
	var list []string
	for _, e := range a.reg.All() {
		if e.CLI.Skip {
			continue
		}
		if top, _, _ := strings.Cut(e.Name, "."); top != "" && !names[top] {
			names[top] = true
			list = append(list, top)
		}
	}
	words := strings.Join(append(list, "completion", "help", "serve", "mcp", "-h", "--help", "-v", "--version"), " ")
	switch shell {
	case "bash":
		fmt.Fprintf(out, `# %s completion for bash
_%s_completions() {
  local cur
  cur="${COMP_WORDS[COMP_CWORD]}"
  COMPREPLY=( $(compgen -W "%s" -- "$cur") )
}
complete -F _%s_completions %s
`, bin, bin, words, bin, bin)
	case "zsh":
		fmt.Fprintf(out, `#compdef %s
_%s_completions() {
  local -a cmds
  cmds=(%s)
  _describe '%s' cmds
}
compdef _%s_completions %s
`, bin, bin, words, bin, bin, bin)
	case "fish":
		for _, n := range list {
			fmt.Fprintf(out, "complete -c %s -f -a %s\n", bin, n)
		}
		fmt.Fprintf(out, "complete -c %s -f -a completion\n", bin)
	default:
		fmt.Fprintf(errOut, "unknown shell %q (want bash|zsh|fish)\n", shell)
		return 2
	}
	return 0
}
