// Package fault defines p3bot's coded errors, warnings, and process exit codes.
//
// It imports only the standard library, so every operational package can
// return an *Error and the CLI renders it unchanged.
package fault

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Process exit codes, following the CLI guide's taxonomy.
const (
	ExitOK         = 0
	ExitFailure    = 1
	ExitUsage      = 2
	ExitNotFound   = 3
	ExitPermission = 4
	ExitConflict   = 5
	ExitTransient  = 75
	ExitConfig     = 78
)

// Code identifies a failure class a caller can branch on.
type Code string

// Error codes. Each maps to exactly one exit code in Code.Exit.
const (
	InputRequired       Code = "INPUT_REQUIRED"
	InvalidArgument     Code = "INVALID_ARGUMENT"
	InvalidColor        Code = "INVALID_COLOR"
	InvalidFields       Code = "INVALID_FIELDS"
	AgentUnknown        Code = "AGENT_UNKNOWN"
	NoSkillsConcept     Code = "NO_SKILLS_CONCEPT"
	NoSkillsRoot        Code = "NO_SKILLS_ROOT"
	NoAgents            Code = "NO_AGENTS"
	NotFound            Code = "NOT_FOUND"
	ManifestMissing     Code = "MANIFEST_MISSING"
	PermissionDenied    Code = "PERMISSION_DENIED"
	Conflict            Code = "CONFLICT"
	UpstreamError       Code = "UPSTREAM_ERROR"
	RateLimited         Code = "RATE_LIMITED"
	Timeout             Code = "TIMEOUT"
	CatalogUnavailable  Code = "CATALOG_UNAVAILABLE"
	ConfigInvalid       Code = "CONFIG_INVALID"
	GoMissing           Code = "GO_MISSING"
	GoTooOld            Code = "GO_TOO_OLD"
	Shadowed            Code = "SHADOWED"
	NotOnPath           Code = "NOT_ON_PATH"
	PlatformUnsupported Code = "PLATFORM_UNSUPPORTED"
	ManifestInvalid     Code = "MANIFEST_INVALID"
	RecordInvalid       Code = "RECORD_INVALID"
	BuildFailed         Code = "BUILD_FAILED"
	SkillCheckFailed    Code = "SKILL_CHECK_FAILED"
	ServiceCheckFailed  Code = "SERVICE_CHECK_FAILED"
	ProblemsFound       Code = "PROBLEMS_FOUND"
	PartialFailure      Code = "PARTIAL_FAILURE"
	CatalogInvalid      Code = "CATALOG_INVALID"
	Unknown             Code = "UNKNOWN"
)

// Exit returns the process exit code for c. A code outside the declared set
// exits as Unknown does.
func (c Code) Exit() int {
	switch c {
	case InputRequired, InvalidArgument, InvalidColor, InvalidFields,
		AgentUnknown, NoSkillsConcept, NoSkillsRoot, NoAgents:
		return ExitUsage
	case NotFound, ManifestMissing:
		return ExitNotFound
	case PermissionDenied:
		return ExitPermission
	case Conflict:
		return ExitConflict
	case UpstreamError, RateLimited, Timeout, CatalogUnavailable:
		return ExitTransient
	case ConfigInvalid, GoMissing, GoTooOld, Shadowed, NotOnPath:
		return ExitConfig
	case PlatformUnsupported, ManifestInvalid, RecordInvalid, BuildFailed,
		SkillCheckFailed, ServiceCheckFailed, ProblemsFound, PartialFailure,
		CatalogInvalid, Unknown:
		return ExitFailure
	default:
		return ExitFailure
	}
}

// Transient reports whether retrying can help, which is true exactly when c
// exits 75.
func (c Code) Transient() bool { return c.Exit() == ExitTransient }

// Error is a failure a caller can act on: what happened, what to do next,
// and, for a closed set of accepted values, what those values are. Build one
// with New, so every error carries a message and a fix.
type Error struct {
	code        Code
	message     string
	fix         string
	validValues []string
	cause       error
}

// New returns an error with code, message, and fix. It panics when message or
// fix is empty, because an error without either leaves the caller no way on.
func New(code Code, message, fix string) *Error {
	if message == "" || fix == "" {
		panic(fmt.Sprintf("fault.New(%s): message and fix are required", code))
	}
	return &Error{code: code, message: message, fix: fix}
}

// WithValidValues sets the accepted values and returns e.
func (e *Error) WithValidValues(values ...string) *Error {
	e.validValues = slices.Clone(values)
	return e
}

// WithCause sets the underlying error that Unwrap returns, and returns e.
func (e *Error) WithCause(err error) *Error {
	e.cause = err
	return e
}

func (e *Error) Error() string { return e.message }
func (e *Error) Unwrap() error { return e.cause }

// Code returns the failure class.
func (e *Error) Code() Code { return e.code }

// Message returns the one-line summary.
func (e *Error) Message() string { return e.message }

// Fix returns the next command or action.
func (e *Error) Fix() string { return e.fix }

// ValidValues returns a copy of the accepted values, or nil when the set is
// open.
func (e *Error) ValidValues() []string { return slices.Clone(e.validValues) }

// Exit returns the process exit code for e.
func (e *Error) Exit() int { return e.code.Exit() }

// Transient reports whether retrying can help.
func (e *Error) Transient() bool { return e.code.Transient() }

// MarshalJSON renders e with transient derived from its code, so the two
// cannot disagree wherever an error is rendered.
func (e *Error) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Code        Code     `json:"code"`
		Message     string   `json:"message"`
		Fix         string   `json:"fix"`
		Transient   bool     `json:"transient"`
		ValidValues []string `json:"valid_values,omitempty"`
	}{e.code, e.message, e.fix, e.Transient(), e.validValues})
}

// From returns the *Error in err's chain, or wraps err as Unknown when it
// carries no code. When the coded error was wrapped, the result carries the
// whole chain's text as its message, so context added with %w is kept. It
// returns nil for a nil err.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	if e, ok := errors.AsType[*Error](err); ok {
		if err.Error() == e.message {
			return e
		}
		wrapped := *e
		wrapped.message = err.Error()
		return &wrapped
	}
	msg := err.Error()
	if msg == "" {
		msg = fmt.Sprintf("unclassified failure (%T)", err)
	}
	return New(Unknown, msg, "rerun with --debug for details").WithCause(err)
}

// WarningCode identifies a non-fatal condition reported alongside a success.
type WarningCode string

// Warning codes. A warning never changes the exit code of the success it
// accompanies.
const (
	AlreadyDeleted WarningCode = "ALREADY_DELETED"
)

// Warning is a non-fatal condition a caller can act on.
type Warning struct {
	Code    WarningCode `json:"code"`
	Message string      `json:"message"`
	Fix     string      `json:"fix,omitempty"`
}
