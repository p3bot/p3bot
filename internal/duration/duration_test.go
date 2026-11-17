package duration

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	t.Parallel()
	valid := []struct {
		in   string
		want time.Duration
	}{
		{"0", 0},
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"1h", time.Hour},
		{"1h30m", 90 * time.Minute},
		{"7d", 7 * 24 * time.Hour},
		{"2d12h", 60 * time.Hour},
		{"1d2h3m4s", 26*time.Hour + 3*time.Minute + 4*time.Second},
	}
	for _, tt := range valid {
		got, err := Parse(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("Parse(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
		}
	}
	invalid := []string{"", "1.5h", "30m1h", "1h2h", "300ms", "0s", "5", "h", "-1h", "1H", " 1h", "106752d"}
	for _, in := range invalid {
		if got, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %v, want an error", in, got)
		}
	}
}

func TestFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "0"},
		{-time.Second, "0"},
		{time.Millisecond, "1s"},
		{time.Hour, "1h"},
		{90 * time.Minute, "1h30m"},
		{60 * time.Hour, "2d12h"},
	}
	for _, tt := range tests {
		got := Format(tt.in)
		if got != tt.want {
			t.Errorf("Format(%v) = %q, want %q", tt.in, got, tt.want)
		}
		if back, err := Parse(got); err != nil || (tt.in >= time.Second && back != tt.in) {
			t.Errorf("Parse(Format(%v)) = %v, %v", tt.in, back, err)
		}
	}
}
