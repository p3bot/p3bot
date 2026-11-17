package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/p3bot/p3bot/internal/fault"
)

// Envelope statuses.
const (
	statusOK    = "ok"
	statusError = "error"
)

// envelope is the single stdout document under --json. p3bot has no
// pagination, so the guide's next cursor field is never set and is omitted.
type envelope struct {
	Status   string          `json:"status"`
	Data     any             `json:"data,omitempty"`
	Error    *fault.Error    `json:"error,omitempty"`
	Warnings []fault.Warning `json:"warnings,omitempty"`
}

func okEnvelope(data any, warnings []fault.Warning) envelope {
	if data == nil {
		// omitempty would drop data, and a success always carries it.
		data = json.RawMessage("null")
	}
	return envelope{Status: statusOK, Data: data, Warnings: warnings}
}

// textRenderer is implemented by every value ok renders in text mode.
type textRenderer interface {
	RenderText(w io.Writer) error
}

func (a *app) writeEnvelope(env envelope) error {
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding envelope: %w", err)
	}
	_, err = fmt.Fprintln(a.sys.stdout, string(data))
	return err
}

// ok emits a successful result: one envelope under --json, otherwise the
// rendered text on stdout and each warning on stderr.
func (a *app) ok(data any, warnings ...fault.Warning) error {
	if a.flags.json {
		return a.writeEnvelope(okEnvelope(data, warnings))
	}
	if data != nil {
		r, isRenderer := data.(textRenderer)
		if !isRenderer {
			return fmt.Errorf("%T does not implement textRenderer", data)
		}
		if err := r.RenderText(a.sys.stdout); err != nil {
			return fmt.Errorf("rendering result: %w", err)
		}
	}
	for _, w := range warnings {
		a.warn(w)
	}
	return nil
}

// warn writes a warning and its fix to stderr unless --quiet is set. Under
// --json, warnings travel in the envelope instead.
func (a *app) warn(w fault.Warning) {
	if a.flags.quiet || a.flags.json {
		return
	}
	on := a.stderrDecorated()
	fmt.Fprintln(a.sys.stderr, style(yellow, "Warning: "+w.Message, on))
	if w.Fix != "" {
		fmt.Fprintln(a.sys.stderr, style(dim, "  Fix: "+w.Fix, on))
	}
}

// renderError emits e as one envelope on stdout under --json, otherwise as
// labelled lines on stderr. When stdout has failed, the text form also goes
// to stderr, the only channel left.
func (a *app) renderError(e *fault.Error) {
	if a.flags.json {
		// The envelope holds only strings, bools, and string slices, so encoding cannot fail.
		_ = a.writeEnvelope(envelope{Status: statusError, Error: e})
		if a.stdout.err == nil {
			return
		}
	}
	labels, values := []string{"Code"}, []string{string(e.Code())}
	if len(e.ValidValues()) > 0 {
		labels, values = append(labels, "Accepted"), append(values, strings.Join(e.ValidValues(), ", "))
	}
	labels, values = append(labels, "Fix"), append(values, e.Fix())
	width := 0
	for _, l := range labels {
		width = max(width, len(l))
	}
	on := a.stderrDecorated()
	fmt.Fprintln(a.sys.stderr, style(red, "Error: "+e.Message(), on))
	for i, l := range labels {
		fmt.Fprintln(a.sys.stderr, style(dim, fmt.Sprintf("  %-*s %s", width+1, l+":", values[i]), on))
	}
}

// stderrDecorated reports whether ANSI escapes may reach stderr. An invalid
// --color value decorates as auto, so its own error renders normally.
func (a *app) stderrDecorated() bool {
	return decorates(a.flags.color, a.sys.getenv, a.sys.stderrTTY)
}

// decorates applies the CLI guide's decoration rules, first match winning.
func decorates(color string, getenv func(string) string, tty bool) bool {
	switch {
	case color == "always":
		return true
	case color == "never", getenv("NO_COLOR") != "":
		return false
	case getenv("FORCE_COLOR") != "", getenv("CLICOLOR_FORCE") != "":
		return true
	case getenv("TERM") == "dumb":
		return false
	default:
		return tty
	}
}

const (
	red    = "31"
	yellow = "33"
	dim    = "2"
)

func style(sgr, s string, on bool) string {
	if !on {
		return s
	}
	return "\x1b[" + sgr + "m" + s + "\x1b[0m"
}

// stickyWriter keeps the first write error, because Cobra writes help and
// --version without checking and the run must not exit 0 after lost output.
type stickyWriter struct {
	w   io.Writer
	err error
}

func (s *stickyWriter) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	n, err := s.w.Write(p)
	if err != nil {
		s.err = err
	}
	return n, err
}
