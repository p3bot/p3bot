package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/p3bot/p3bot/internal/fault"
)

var completionShells = []string{"bash", "zsh", "fish"}

func (a *app) newCompletionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "completion bash|zsh|fish",
		Short: "Print a shell completion script",
		Long: `Print a completion script for bash, zsh, or fish to stdout. The script is
static, so generating it reads no configuration and no network.`,
		Example: `  p3bot completion bash > ~/.local/share/bash-completion/completions/p3bot
  p3bot completion zsh > "${fpath[1]}/_p3bot"
  p3bot completion fish > ~/.config/fish/completions/p3bot.fish`,
		ValidArgs:   completionShells,
		Args:        cobra.ArbitraryArgs,
		Annotations: map[string]string{skipConfig: ""},
		RunE: func(cmd *cobra.Command, args []string) error {
			shell, err := completionShell(args, a.flags.json)
			if err != nil {
				return err
			}
			root, w := cmd.Root(), cmd.OutOrStdout()
			switch shell {
			case "bash":
				return root.GenBashCompletionV2(w, true)
			case "zsh":
				return root.GenZshCompletion(w)
			default:
				return root.GenFishCompletion(w, true)
			}
		},
	}
}

// completionShell validates the arguments so each mistake carries the
// accepted shells. A script is not data, so --json has nothing to wrap.
func completionShell(args []string, jsonMode bool) (string, error) {
	fix := "p3bot completion bash"
	switch {
	case jsonMode:
		return "", fault.New(fault.InvalidArgument, "completion prints a shell script and has no JSON output", fix)
	case len(args) == 0:
		return "", fault.New(fault.InputRequired, "shell required: "+strings.Join(completionShells, ", "), fix).
			WithValidValues(completionShells...)
	case len(args) > 1:
		return "", fault.New(fault.InvalidArgument, fmt.Sprintf("completion takes one shell, got %d arguments", len(args)), fix).
			WithValidValues(completionShells...)
	case !slices.Contains(completionShells, args[0]):
		return "", fault.New(fault.InvalidArgument, fmt.Sprintf("unsupported shell %q", args[0]), fix).
			WithValidValues(completionShells...)
	}
	return args[0], nil
}
