package cli

import (
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/p3bot/p3bot/internal/config"
	"github.com/p3bot/p3bot/internal/duration"
	"github.com/p3bot/p3bot/internal/fault"
)

const rootLong = `p3bot manages the p3bot command-line tools and reports problems with their
installation, for people and AI agents. It never prompts.

Results go to stdout; warnings, hints, and debug output go to stderr. With
--json, stdout carries one envelope:
  {"status":"ok","data":...,"warnings":[{"code","message","fix"}]}
  {"status":"error","error":{"code","message","fix","transient","valid_values"}}

Exit codes: 0 ok, 1 failure, 2 usage, 3 not found, 4 permission, 5 conflict,
75 retry may help, 78 configuration, 130 interrupted.

Configuration: P3BOT_BIN_DIR sets the install directory (default ~/.local/bin)
ahead of bin_dir in config.cue. config.cue lives in ~/.config/p3bot on Linux
and ~/Library/Application Support/p3bot/config on macOS, or
$XDG_CONFIG_HOME/p3bot when that is set. It accepts bin_dir (absolute or ~/
path) and version_cache_ttl (such as 1h).`

// skipConfig marks a command that must work with a broken configuration.
const skipConfig = "p3bot.skip-config"

type globalFlags struct {
	json    bool
	quiet   bool
	noInput bool
	color   string
	verbose bool
	debug   bool
}

// app is the state of one invocation. A fresh app and command tree per run
// keeps flag values from leaking between runs.
type app struct {
	sys    system
	stdout *stickyWriter
	flags  globalFlags
	log    *slog.Logger
	cfg    config.Config
}

func newApp(sys system) *app {
	out := &stickyWriter{w: sys.stdout}
	sys.stdout = out
	return &app{sys: sys, stdout: out, log: slog.New(slog.DiscardHandler)}
}

func init() {
	cobra.AddTemplateFunc("p3botVersion", versionText)
}

func (a *app) newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:               "p3bot",
		Short:             "Manage the p3bot tools",
		Long:              rootLong,
		Version:           a.sys.version,
		SilenceUsage:      true,
		SilenceErrors:     true,
		Args:              usageArgs(cobra.NoArgs),
		PersistentPreRunE: a.setup,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true

	pf := root.PersistentFlags()
	pf.BoolVar(&a.flags.json, "json", false, "Emit a JSON envelope on stdout")
	pf.BoolVar(&a.flags.quiet, "quiet", false, "Drop warnings and hints from stderr")
	pf.BoolVar(&a.flags.noInput, "no-input", false, "Never prompt (p3bot never prompts; accepted for consistency)")
	pf.StringVar(&a.flags.color, "color", "auto", "Colour on stderr: auto, always, never")
	pf.BoolVar(&a.flags.verbose, "verbose", false, "Add detail to stdout")
	pf.BoolVar(&a.flags.debug, "debug", false, "Diagnostic logging on stderr")
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &usageError{cmd: cmd.CommandPath(), err: err}
	})

	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(c *cobra.Command, args []string) {
		if !a.flags.json {
			defaultHelp(c, args)
			return
		}
		var b strings.Builder
		prev := c.OutOrStdout()
		c.SetOut(&b)
		defaultHelp(c, args)
		c.SetOut(prev)
		// A failed write is kept by the sticky stdout and fails the run.
		_ = a.writeEnvelope(okEnvelope(map[string]string{"help": b.String()}, nil))
	})
	root.SetVersionTemplate(`{{p3botVersion .}}`)

	root.AddCommand(a.newCompletionCmd())
	return root
}

// versionText renders --version. Cobra prints it before any run hook, so it
// reads --json from the parsed flags itself.
func versionText(c *cobra.Command) string {
	if j, _ := c.Flags().GetBool("json"); j {
		data, _ := json.MarshalIndent(okEnvelope(map[string]string{"version": c.Version}, nil), "", "  ")
		return string(data) + "\n"
	}
	return c.Name() + " version " + c.Version + "\n"
}

// setup validates the global flags and resolves configuration before any
// command runs.
func (a *app) setup(cmd *cobra.Command, _ []string) error {
	a.setDebug(a.flags.debug)
	switch a.flags.color {
	case "auto", "always", "never":
	default:
		return fault.New(fault.InvalidColor,
			"--color must be one of: auto, always, never; got "+strconv.Quote(a.flags.color),
			cmd.CommandPath()+" --color=auto",
		).WithValidValues("auto", "always", "never")
	}
	a.log.Debug("run", "command", cmd.CommandPath(), "version", a.sys.version, "goos", a.sys.goos)
	if !needsConfig(cmd) {
		return nil
	}
	cfg, err := config.Resolve(config.Env{GOOS: a.sys.goos, Getenv: a.sys.getenv})
	if err != nil {
		return err
	}
	a.cfg = cfg
	a.log.Debug("config resolved",
		"file", cfg.File,
		"bin", cfg.Dirs.Bin,
		"bin_source", cfg.BinSource,
		"config", cfg.Dirs.Config,
		"data", cfg.Dirs.Data,
		"state", cfg.Dirs.State,
		"cache", cfg.Dirs.Cache,
		"version_cache_ttl", duration.Format(cfg.VersionCacheTTL),
	)
	return nil
}

func (a *app) setDebug(on bool) {
	if on {
		a.log = slog.New(slog.NewTextHandler(a.sys.stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	} else {
		a.log = slog.New(slog.DiscardHandler)
	}
}

func needsConfig(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case "help", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return false
	}
	for c := cmd; c != nil; c = c.Parent() {
		if _, ok := c.Annotations[skipConfig]; ok {
			return false
		}
	}
	return true
}

// usageError is a parser-level failure: unknown command or flag, bad flag
// value, or wrong argument count.
type usageError struct {
	cmd string
	err error
}

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// usageArgs marks a positional-argument validator's failure as a usage error.
func usageArgs(v cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := v(cmd, args); err != nil {
			return &usageError{cmd: cmd.CommandPath(), err: err}
		}
		return nil
	}
}

// flagRequested reports whether the boolean flag --name is set in raw
// arguments; the last occurrence wins, as in pflag.
func flagRequested(args []string, name string) bool {
	found := false
	for _, arg := range args {
		if arg == "--" {
			break
		}
		switch {
		case arg == "--"+name:
			found = true
		case strings.HasPrefix(arg, "--"+name+"="):
			v, err := strconv.ParseBool(strings.TrimPrefix(arg, "--"+name+"="))
			found = err == nil && v
		}
	}
	return found
}
