package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/p3bot/p3bot/internal/fault"
)

const testVersion = "v1.2.3"

type result struct {
	stdout string
	stderr string
	err    error
	exit   int
}

// testEnv isolates HOME and every XDG tier under a temp dir and returns the
// environment a run sees.
func testEnv(t *testing.T, extra map[string]string) map[string]string {
	t.Helper()
	root := t.TempDir()
	env := map[string]string{
		"HOME":            filepath.Join(root, "home"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
		"XDG_STATE_HOME":  filepath.Join(root, "state"),
		"XDG_CACHE_HOME":  filepath.Join(root, "cache"),
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

func newTestApp(env map[string]string, stdout, stderr *bytes.Buffer) *app {
	return newApp(system{
		stdout:  stdout,
		stderr:  stderr,
		getenv:  func(k string) string { return env[k] },
		goos:    "linux",
		version: testVersion,
	})
}

func runWith(t *testing.T, env map[string]string, extend func(a *app, root *cobra.Command), args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	a := newTestApp(env, &stdout, &stderr)
	root := a.newRoot()
	if extend != nil {
		extend(a, root)
	}
	err := a.execute(context.Background(), root, args)
	return result{stdout.String(), stderr.String(), err, ExitCode(err)}
}

func runCLI(t *testing.T, args ...string) result {
	t.Helper()
	return runWith(t, testEnv(t, nil), nil, args...)
}

type wireEnvelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
	Error  *struct {
		Code        string   `json:"code"`
		Message     string   `json:"message"`
		Fix         string   `json:"fix"`
		Transient   *bool    `json:"transient"`
		ValidValues []string `json:"valid_values"`
	} `json:"error"`
	Warnings []fault.Warning `json:"warnings"`
}

// decodeOne requires stdout to hold exactly one JSON document.
func decodeOne(t *testing.T, stdout string) wireEnvelope {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var env wireEnvelope
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("stdout is not an envelope: %v\n%s", err, stdout)
	}
	if dec.More() {
		t.Fatalf("stdout holds more than one document:\n%s", stdout)
	}
	return env
}

func requireErrorEnvelope(t *testing.T, r result, code fault.Code) wireEnvelope {
	t.Helper()
	env := decodeOne(t, r.stdout)
	if env.Status != statusError || env.Error == nil {
		t.Fatalf("envelope = %s, want an error", r.stdout)
	}
	if env.Error.Code != string(code) {
		t.Fatalf("code = %s, want %s: %s", env.Error.Code, code, env.Error.Message)
	}
	if env.Error.Fix == "" || env.Error.Message == "" {
		t.Errorf("error lacks message or fix: %s", r.stdout)
	}
	if env.Error.Transient == nil || *env.Error.Transient != code.Transient() {
		t.Errorf("transient = %v, want %v", env.Error.Transient, code.Transient())
	}
	if r.exit != code.Exit() {
		t.Errorf("exit = %d, want %d", r.exit, code.Exit())
	}
	if r.stderr != "" {
		t.Errorf("stderr under --json = %q, want empty", r.stderr)
	}
	return env
}

func writeConfigFile(t *testing.T, env map[string]string, src string) {
	t.Helper()
	dir := filepath.Join(env["XDG_CONFIG_HOME"], "p3bot")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.cue"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()
	r := runCLI(t, "--version")
	if r.err != nil || r.stdout != "p3bot version "+testVersion+"\n" {
		t.Errorf("--version = %q, %v", r.stdout, r.err)
	}

	r = runCLI(t, "--version", "--json")
	env := decodeOne(t, r.stdout)
	var data map[string]string
	if err := json.Unmarshal(env.Data, &data); err != nil || env.Status != statusOK || data["version"] != testVersion {
		t.Errorf("--version --json = %s", r.stdout)
	}
}

func TestBuildVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{"tagged", &debug.BuildInfo{Main: debug.Module{Version: "v0.4.0"}}, true, "v0.4.0"},
		{"local", &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261116000000-abcdef123456+dirty"}}, true, "v0.0.0-20261116000000-abcdef123456+dirty"},
		{"devel", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true, "(devel)"},
		{"empty", &debug.BuildInfo{}, true, "(unknown)"},
		{"missing", nil, false, "(unknown)"},
	}
	for _, tt := range tests {
		got := buildVersion(func() (*debug.BuildInfo, bool) { return tt.info, tt.ok })
		if got != tt.want {
			t.Errorf("%s: buildVersion = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestHelp(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{}, {"--help"}, {"help"}} {
		r := runCLI(t, args...)
		if r.err != nil || !strings.Contains(r.stdout, "Usage:") || r.stderr != "" {
			t.Errorf("%v: stdout %q stderr %q err %v", args, r.stdout, r.stderr, r.err)
		}
		r = runCLI(t, append(args, "--json")...)
		env := decodeOne(t, r.stdout)
		var data map[string]string
		if err := json.Unmarshal(env.Data, &data); err != nil || env.Status != statusOK || !strings.Contains(data["help"], "Usage:") {
			t.Errorf("%v --json: %s", args, r.stdout)
		}
	}
}

func TestForcedErrorsUnderJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		args  []string
		code  fault.Code
		valid []string
	}{
		{"invalid color", []string{"--color=bogus", "--json"}, fault.InvalidColor, []string{"auto", "always", "never"}},
		{"unknown command", []string{"bogus", "--json"}, fault.InvalidArgument, nil},
		{"json before unknown flag", []string{"--json", "--bogus"}, fault.InvalidArgument, nil},
		{"json after unknown flag", []string{"--bogus", "--json"}, fault.InvalidArgument, nil},
		{"completion missing shell", []string{"completion", "--json"}, fault.InvalidArgument, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := requireErrorEnvelope(t, runCLI(t, tt.args...), tt.code)
			if !slices.Equal(env.Error.ValidValues, tt.valid) {
				t.Errorf("valid_values = %v, want %v", env.Error.ValidValues, tt.valid)
			}
		})
	}
}

// Every command, at every flag position, answers a bad flag with the error envelope.
func TestErrorEnvelopeEveryCommand(t *testing.T) {
	t.Parallel()
	var paths [][]string
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if !c.Hidden && c.Name() != "help" {
			paths = append(paths, strings.Fields(c.CommandPath())[1:])
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newApp(system{}).newRoot())
	for _, path := range paths {
		for _, args := range [][]string{
			append(slices.Clone(path), "--bogus", "--json"),
			append(slices.Clone(path), "--json", "--bogus"),
		} {
			requireErrorEnvelope(t, runCLI(t, args...), fault.InvalidArgument)
		}
	}
}

func TestTextErrorGoesToStderr(t *testing.T) {
	t.Parallel()
	r := runCLI(t, "completion", "powershell")
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
	want := "Error: unsupported shell \"powershell\"\n" +
		"  Code:     INVALID_ARGUMENT\n" +
		"  Accepted: bash, zsh, fish\n" +
		"  Fix:      p3bot completion bash\n"
	if r.stderr != want {
		t.Errorf("stderr =\n%s\nwant\n%s", r.stderr, want)
	}
	if r.exit != 2 {
		t.Errorf("exit = %d, want 2", r.exit)
	}
}

func TestErrorDecoration(t *testing.T) {
	t.Parallel()
	plain := runCLI(t, "completion")
	if strings.Contains(plain.stderr, "\x1b[") {
		t.Errorf("ANSI on a non-terminal stderr: %q", plain.stderr)
	}
	colored := runCLI(t, "completion", "--color=always")
	if !strings.Contains(colored.stderr, "\x1b[31mError:") {
		t.Errorf("--color=always did not colour the error: %q", colored.stderr)
	}
	jsonOut := runCLI(t, "completion", "x", "--color=always", "--json")
	if strings.Contains(jsonOut.stdout, "\x1b[") {
		t.Errorf("ANSI in the envelope: %q", jsonOut.stdout)
	}
}

func TestDecorates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		color string
		env   map[string]string
		tty   bool
		want  bool
	}{
		{"auto tty", "auto", nil, true, true},
		{"auto pipe", "auto", nil, false, false},
		{"always beats NO_COLOR", "always", map[string]string{"NO_COLOR": "1"}, false, true},
		{"never beats FORCE_COLOR", "never", map[string]string{"FORCE_COLOR": "1"}, true, false},
		{"NO_COLOR", "auto", map[string]string{"NO_COLOR": "1"}, true, false},
		{"NO_COLOR beats FORCE_COLOR", "auto", map[string]string{"NO_COLOR": "1", "FORCE_COLOR": "1"}, true, false},
		{"FORCE_COLOR", "auto", map[string]string{"FORCE_COLOR": "1"}, false, true},
		{"CLICOLOR_FORCE", "auto", map[string]string{"CLICOLOR_FORCE": "1"}, false, true},
		{"FORCE_COLOR beats dumb", "auto", map[string]string{"FORCE_COLOR": "1", "TERM": "dumb"}, false, true},
		{"dumb", "auto", map[string]string{"TERM": "dumb"}, true, false},
		{"invalid acts as auto", "bogus", nil, true, true},
	}
	for _, tt := range tests {
		got := decorates(tt.color, func(k string) string { return tt.env[k] }, tt.tty)
		if got != tt.want {
			t.Errorf("%s: decorates = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestCompletion(t *testing.T) {
	t.Parallel()
	markers := map[string]string{
		"bash": "bash completion V2 for p3bot",
		"zsh":  "#compdef p3bot",
		"fish": "fish completion for p3bot",
	}
	for shell, marker := range markers {
		r := runCLI(t, "completion", shell)
		if r.err != nil || !strings.Contains(r.stdout, marker) || r.stderr != "" {
			t.Errorf("completion %s: err %v stderr %q stdout starts %q", shell, r.err, r.stderr, r.stdout[:min(len(r.stdout), 60)])
		}
	}
}

func TestCompletionArguments(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		args []string
		code fault.Code
	}{
		{[]string{"completion"}, fault.InputRequired},
		{[]string{"completion", "bash", "zsh"}, fault.InvalidArgument},
		{[]string{"completion", "powershell"}, fault.InvalidArgument},
	} {
		r := runCLI(t, tt.args...)
		if r.exit != 2 || !strings.Contains(r.stderr, string(tt.code)) || !strings.Contains(r.stderr, "bash, zsh, fish") {
			t.Errorf("%v: exit %d stderr %q, want %s listing the shells", tt.args, r.exit, r.stderr, tt.code)
		}
	}
}

// Completion is static, so a broken config must not stop it.
func TestCompletionIgnoresConfig(t *testing.T) {
	t.Parallel()
	env := testEnv(t, map[string]string{"P3BOT_BIN_DIR": "relative"})
	writeConfigFile(t, env, "bin_dir: 3")
	for _, args := range [][]string{{"completion", "bash"}, {"help"}, {"__complete", ""}} {
		if r := runWith(t, env, nil, args...); r.err != nil {
			t.Errorf("%v with broken config: %v", args, r.err)
		}
	}
}

func TestConfigErrors(t *testing.T) {
	t.Parallel()
	t.Run("malformed config.cue", func(t *testing.T) {
		t.Parallel()
		env := testEnv(t, nil)
		writeConfigFile(t, env, "bin_dir: \"")
		requireErrorEnvelope(t, runWith(t, env, nil, "--json"), fault.ConfigInvalid)
	})
	t.Run("relative P3BOT_BIN_DIR", func(t *testing.T) {
		t.Parallel()
		env := testEnv(t, map[string]string{"P3BOT_BIN_DIR": "rel/bin"})
		got := requireErrorEnvelope(t, runWith(t, env, nil, "--json"), fault.ConfigInvalid)
		if !strings.Contains(got.Error.Message, `"rel/bin"`) || !strings.Contains(got.Error.Message, "P3BOT_BIN_DIR") {
			t.Errorf("message %q does not name the value and its source", got.Error.Message)
		}
	})
}

func TestConfigResolvedIntoApp(t *testing.T) {
	t.Parallel()
	env := testEnv(t, nil)
	writeConfigFile(t, env, "bin_dir: \"~/x\"\nversion_cache_ttl: \"1h\"\n")
	var stdout, stderr bytes.Buffer
	a := newTestApp(env, &stdout, &stderr)
	if err := a.execute(context.Background(), a.newRoot(), []string{"--debug"}); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(env["HOME"], "x"); a.cfg.Dirs.Bin != want {
		t.Errorf("bin dir = %q, want %q", a.cfg.Dirs.Bin, want)
	}
	if !strings.Contains(stderr.String(), "msg=\"config resolved\"") || strings.Contains(stdout.String(), "level=DEBUG") {
		t.Errorf("--debug must log to stderr only; stderr %q", stderr.String())
	}
}

// A coded error from an operational package reaches the envelope with its
// code, fix, and valid values intact and any wrapped context kept; an
// uncoded one becomes UNKNOWN, exit 1, never retryable.
func TestCommandErrorsReachEnvelope(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err     error
		code    fault.Code
		message string
	}{
		{fault.New(fault.UpstreamError, "proxy.golang.org: timeout", "p3bot install snag"), fault.UpstreamError, "proxy.golang.org: timeout"},
		{fmt.Errorf("installing tk: %w", fault.New(fault.Conflict, "modified", "p3bot doctor")), fault.Conflict, "installing tk: modified"},
		{fault.New(fault.InputRequired, "tool required", "p3bot install tk").WithValidValues("tk", "tkv"), fault.InputRequired, "tool required"},
		{errors.New("something unexpected"), fault.Unknown, "something unexpected"},
	}
	for _, tt := range tests {
		extend := func(_ *app, root *cobra.Command) {
			root.AddCommand(&cobra.Command{Use: "fail", RunE: func(*cobra.Command, []string) error { return tt.err }})
		}
		r := runWith(t, testEnv(t, nil), extend, "fail", "--json")
		env := requireErrorEnvelope(t, r, tt.code)
		if env.Error.Message != tt.message {
			t.Errorf("message = %q, want %q", env.Error.Message, tt.message)
		}
		if fe, ok := errors.AsType[*fault.Error](tt.err); ok && (env.Error.Fix != fe.Fix() || !slices.Equal(env.Error.ValidValues, fe.ValidValues())) {
			t.Errorf("error changed on its way to the envelope: %s", r.stdout)
		}
	}
}

func TestOK(t *testing.T) {
	t.Parallel()
	warning := fault.Warning{Code: fault.AlreadyDeleted, Message: "tk was already removed", Fix: "p3bot list"}
	cmd := func(data any) func(*app, *cobra.Command) {
		return func(a *app, root *cobra.Command) {
			root.AddCommand(&cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error {
				return a.ok(data, warning)
			}})
		}
	}

	r := runWith(t, testEnv(t, nil), cmd(lines{"removed tk"}), "probe", "--json")
	env := decodeOne(t, r.stdout)
	var data []string
	if err := json.Unmarshal(env.Data, &data); err != nil || r.exit != 0 || env.Status != statusOK || !slices.Equal(data, []string{"removed tk"}) {
		t.Errorf("ok --json: exit %d %s", r.exit, r.stdout)
	}
	if len(env.Warnings) != 1 || env.Warnings[0] != warning {
		t.Errorf("warnings = %+v", env.Warnings)
	}

	r = runWith(t, testEnv(t, nil), cmd(nil), "probe", "--json")
	if !strings.Contains(r.stdout, `"data": null`) {
		t.Errorf("nil data dropped from envelope: %s", r.stdout)
	}

	r = runWith(t, testEnv(t, nil), cmd(lines{"removed tk"}), "probe")
	if r.exit != 0 || r.stdout != "removed tk\n" || r.stderr != "Warning: tk was already removed\n  Fix: p3bot list\n" {
		t.Errorf("ok text: exit %d stdout %q stderr %q", r.exit, r.stdout, r.stderr)
	}

	r = runWith(t, testEnv(t, nil), cmd(lines{"removed tk"}), "probe", "--quiet")
	if r.stdout != "removed tk\n" || r.stderr != "" {
		t.Errorf("ok --quiet: stdout %q stderr %q", r.stdout, r.stderr)
	}

	r = runWith(t, testEnv(t, nil), cmd(struct{}{}), "probe")
	if r.exit != 1 || r.stdout != "" {
		t.Errorf("non-renderer in text mode must fail: exit %d stdout %q", r.exit, r.stdout)
	}
}

type lines []string

func (l lines) RenderText(w io.Writer) error {
	for _, s := range l {
		if _, err := fmt.Fprintln(w, s); err != nil {
			return err
		}
	}
	return nil
}

func TestCancelledRunRendersNothing(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	a := newTestApp(testEnv(t, nil), &stdout, &stderr)
	root := a.newRoot()
	ctx, cancel := context.WithCancel(context.Background())
	root.AddCommand(&cobra.Command{Use: "wait", RunE: func(cmd *cobra.Command, _ []string) error {
		cancel()
		return cmd.Context().Err()
	}})
	if err := a.execute(ctx, root, []string{"wait", "--json"}); err == nil {
		t.Fatal("want the cancellation error")
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("cancelled run rendered output: stdout %q stderr %q", stdout.String(), stderr.String())
	}
}

func TestExitCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{&InterruptedError{Signal: syscall.SIGINT}, 130},
		{&InterruptedError{Signal: syscall.SIGTERM}, 143},
		{fault.New(fault.ConfigInvalid, "m", "f"), 78},
		{fault.New(fault.UpstreamError, "m", "f"), 75},
		{errors.New("plain"), 1},
	}
	for _, tt := range tests {
		if got := ExitCode(tt.err); got != tt.want {
			t.Errorf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
		}
	}
}

func TestFlagRequested(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"--json"}, true},
		{[]string{"--json=true"}, true},
		{[]string{"--json", "--json=false"}, false},
		{[]string{"--json=bogus"}, false},
		{[]string{"--", "--json"}, false},
		{[]string{"x", "--bogus", "--json"}, true},
		{[]string{"--jsonx"}, false},
		{[]string{"--debug"}, false},
	}
	for _, tt := range tests {
		if got := flagRequested(tt.args, "json"); got != tt.want {
			t.Errorf("flagRequested(%v, json) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestDebugCoversParseFailures(t *testing.T) {
	t.Parallel()
	r := runCLI(t, "--debug", "--bogus")
	if !strings.Contains(r.stderr, `msg="command failed"`) || !strings.Contains(r.stderr, "code=INVALID_ARGUMENT") {
		t.Errorf("--debug logged nothing for a parse failure: %q", r.stderr)
	}
	r = runCLI(t, "--debug", "--debug=false", "--bogus")
	if strings.Contains(r.stderr, "level=DEBUG") {
		t.Errorf("--debug=false still logged: %q", r.stderr)
	}
}

func wordSet(words string) map[string]bool {
	set := make(map[string]bool)
	for _, w := range strings.Fields(words) {
		set[w] = true
	}
	return set
}

// No command or flag takes a name from the CLI guide's "Never as canonical" columns.
func TestCanonicalVocabulary(t *testing.T) {
	t.Parallel()
	bannedCommands := wordSet("view info show describe ls all find add new make edit modify rm remove destroy exec do health state")
	bannedFlags := wordSet("skip-confirmations no-confirm yes-to-all yes non-interactive plan preview simulate silent " +
		"max-results count max page offset cursor columns select where query from start-time after to end-time before " +
		"config-file conf trace diagnostics colour ansi recurse tree tail observe subscribe " +
		"out destination write-to save-to target config-name env-name block sync time-limit max-duration deadline")
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if bannedCommands[c.Name()] {
			t.Errorf("command %q is not canonical", c.CommandPath())
		}
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if bannedFlags[f.Name] {
				t.Errorf("%s: flag --%s is not canonical", c.CommandPath(), f.Name)
			}
		})
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newApp(system{}).newRoot())
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// Output Cobra writes without checking must still fail the run when it is lost.
func TestLostStdoutFailsRun(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--help"}, {"--help", "--json"}, {}, {"--version"}, {"--version", "--json"}, {"completion", "bash"}} {
		var stderr bytes.Buffer
		env := testEnv(t, nil)
		a := newApp(system{stdout: failingWriter{}, stderr: &stderr, getenv: func(k string) string { return env[k] }, goos: "linux", version: testVersion})
		err := a.execute(context.Background(), a.newRoot(), args)
		if ExitCode(err) != 1 {
			t.Errorf("%v: exit %d (%v), want 1", args, ExitCode(err), err)
		}
		if !strings.Contains(stderr.String(), "disk full") {
			t.Errorf("%v: stderr %q does not explain the failure", args, stderr.String())
		}
	}
}
