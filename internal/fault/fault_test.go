package fault

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
)

func TestCodeExit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code Code
		exit int
	}{
		{InputRequired, 2},
		{InvalidArgument, 2},
		{InvalidColor, 2},
		{InvalidFields, 2},
		{AgentUnknown, 2},
		{NoSkillsConcept, 2},
		{NoSkillsRoot, 2},
		{NoAgents, 2},
		{NotFound, 3},
		{ManifestMissing, 3},
		{PermissionDenied, 4},
		{Conflict, 5},
		{UpstreamError, 75},
		{RateLimited, 75},
		{Timeout, 75},
		{CatalogUnavailable, 75},
		{ConfigInvalid, 78},
		{GoMissing, 78},
		{GoTooOld, 78},
		{Shadowed, 78},
		{NotOnPath, 78},
		{PlatformUnsupported, 1},
		{ManifestInvalid, 1},
		{RecordInvalid, 1},
		{BuildFailed, 1},
		{SkillCheckFailed, 1},
		{ServiceCheckFailed, 1},
		{ProblemsFound, 1},
		{PartialFailure, 1},
		{CatalogInvalid, 1},
		{Unknown, 1},
		{Code("NOT_A_CODE"), 1},
	}
	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			t.Parallel()
			if got := tt.code.Exit(); got != tt.exit {
				t.Errorf("Exit() = %d, want %d", got, tt.exit)
			}
			if got, want := tt.code.Transient(), tt.exit == ExitTransient; got != want {
				t.Errorf("Transient() = %v, want %v", got, want)
			}
		})
	}
}

func TestErrorJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "transient",
			err:  New(UpstreamError, "proxy timeout", "retry"),
			want: `{"code":"UPSTREAM_ERROR","message":"proxy timeout","fix":"retry","transient":true}`,
		},
		{
			name: "not transient",
			err:  New(Conflict, "modified", "p3bot doctor"),
			want: `{"code":"CONFLICT","message":"modified","fix":"p3bot doctor","transient":false}`,
		},
		{
			name: "valid values",
			err:  New(InvalidColor, "bad", "f").WithValidValues("auto", "never"),
			want: `{"code":"INVALID_COLOR","message":"bad","fix":"f","transient":false,"valid_values":["auto","never"]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := json.Marshal(tt.err)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestErrorJSONInsideData(t *testing.T) {
	t.Parallel()
	data := []struct {
		Tool  string `json:"tool"`
		Error *Error `json:"error"`
	}{{"snag", New(RateLimited, "429", "retry")}}
	got, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"tool":"snag","error":{"code":"RATE_LIMITED","message":"429","fix":"retry","transient":true}}]`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestFrom(t *testing.T) {
	t.Parallel()
	if From(nil) != nil {
		t.Error("From(nil) != nil")
	}

	coded := New(Conflict, "taken", "p3bot doctor").WithValidValues("a")
	if got := From(coded); got != coded {
		t.Errorf("From(coded) = %+v, want the original error unchanged", got)
	}
	got := From(fmt.Errorf("installing tk: %w", coded))
	if got.Message() != "installing tk: taken" {
		t.Errorf("From(wrapped coded) message = %q, want the wrapped context kept", got.Message())
	}
	if got.Code() != Conflict || got.Fix() != coded.Fix() || !slices.Equal(got.ValidValues(), coded.ValidValues()) {
		t.Errorf("From(wrapped coded) changed code, fix, or valid values: %+v", got)
	}
	if coded.Message() != "taken" {
		t.Error("From mutated the original error")
	}

	cause := errors.New("boom")
	got = From(fmt.Errorf("ctx: %w", cause))
	if got.Code() != Unknown || got.Exit() != ExitFailure || got.Transient() {
		t.Errorf("From(uncoded) = %+v, want UNKNOWN exit 1 not transient", got)
	}
	if got.Message() != "ctx: boom" || got.Fix() == "" {
		t.Errorf("From(uncoded) message %q fix %q", got.Message(), got.Fix())
	}
	if !errors.Is(got, cause) {
		t.Error("From(uncoded) lost the cause chain")
	}
}

func TestFromEmptyMessage(t *testing.T) {
	t.Parallel()
	got := From(errors.New(""))
	if got.Code() != Unknown || got.Message() == "" {
		t.Errorf("From(empty) = %q, want UNKNOWN with a message", got.Message())
	}
}

func TestNewRequiresMessageAndFix(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ message, fix string }{{"", "f"}, {"m", ""}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("New(%q, %q) did not panic", tt.message, tt.fix)
				}
			}()
			_ = New(Conflict, tt.message, tt.fix)
		}()
	}
}

func TestWithValidValuesCopies(t *testing.T) {
	t.Parallel()
	values := []string{"a", "b"}
	e := New(InvalidArgument, "m", "f").WithValidValues(values...)
	values[0] = "changed"
	if e.ValidValues()[0] != "a" {
		t.Error("WithValidValues aliased the caller's slice")
	}
	e.ValidValues()[0] = "changed"
	if e.ValidValues()[0] != "a" {
		t.Error("ValidValues exposed the error's own slice")
	}
	if New(InvalidArgument, "m", "f").ValidValues() != nil {
		t.Error("an open set must report nil")
	}
}
