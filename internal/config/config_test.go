package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/p3bot/p3bot/internal/fault"
)

func envOf(goos string, vars map[string]string) Env {
	return Env{GOOS: goos, Getenv: func(k string) string { return vars[k] }}
}

func writeConfig(t *testing.T, configHome, src string) {
	t.Helper()
	dir := filepath.Join(configHome, "p3bot")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.cue")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}

func requireConfigInvalid(t *testing.T, err error, contains ...string) {
	t.Helper()
	fe, ok := errors.AsType[*fault.Error](err)
	if !ok {
		t.Fatalf("error = %v, want *fault.Error", err)
	}
	if fe.Code() != fault.ConfigInvalid || fe.Exit() != 78 {
		t.Fatalf("code %s exit %d, want CONFIG_INVALID 78", fe.Code(), fe.Exit())
	}
	if fe.Fix() == "" {
		t.Error("CONFIG_INVALID carries no fix")
	}
	for _, s := range contains {
		if !strings.Contains(fe.Message(), s) {
			t.Errorf("message %q does not contain %q", fe.Message(), s)
		}
	}
}

func TestResolveDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		goos string
		want Dirs
	}{
		{"linux", Dirs{
			Bin:    "/h/.local/bin",
			Config: "/h/.config/p3bot",
			Data:   "/h/.local/share/p3bot",
			State:  "/h/.local/state/p3bot",
			Cache:  "/h/.cache/p3bot",
		}},
		{"darwin", Dirs{
			Bin:    "/h/.local/bin",
			Config: "/h/Library/Application Support/p3bot/config",
			Data:   "/h/Library/Application Support/p3bot/data",
			State:  "/h/Library/Application Support/p3bot/state",
			Cache:  "/h/Library/Caches/p3bot",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			t.Parallel()
			c, err := Resolve(envOf(tt.goos, map[string]string{"HOME": "/h", "XDG_CONFIG_HOME": "relative"}))
			if err != nil {
				t.Fatal(err)
			}
			if c.Dirs != tt.want {
				t.Errorf("Dirs = %+v\nwant   %+v", c.Dirs, tt.want)
			}
			if c.BinSource != SourceDefault || c.VersionCacheTTL != time.Hour {
				t.Errorf("BinSource %q TTL %v", c.BinSource, c.VersionCacheTTL)
			}
			if c.File != filepath.Join(tt.want.Config, "config.cue") {
				t.Errorf("File = %q", c.File)
			}
		})
	}
}

func TestResolveXDGAndEnvOverride(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(v, filepath.Join(root, strings.ToLower(v)))
	}
	writeConfig(t, os.Getenv("XDG_CONFIG_HOME"), `bin_dir: "~/from-config"`)

	for _, goos := range []string{"linux", "darwin"} {
		env := Env{GOOS: goos, Getenv: os.Getenv}
		t.Setenv(BinDirEnv, "")
		c, err := Resolve(env)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range []string{c.Dirs.Config, c.Dirs.Data, c.Dirs.State, c.Dirs.Cache} {
			if !strings.HasPrefix(d, root+string(filepath.Separator)) || strings.HasPrefix(d, home) {
				t.Errorf("%s: tier %q is not under the XDG temp dirs", goos, d)
			}
		}
		if want := filepath.Join(home, "from-config"); c.Dirs.Bin != want || c.BinSource != c.File {
			t.Errorf("%s: config bin dir = %q from %q, want %q from the file", goos, c.Dirs.Bin, c.BinSource, want)
		}

		override := filepath.Join(root, "envbin")
		t.Setenv(BinDirEnv, override)
		c, err = Resolve(env)
		if err != nil {
			t.Fatal(err)
		}
		if c.Dirs.Bin != override || c.BinSource != SourceEnv {
			t.Errorf("%s: %s did not override config: %q from %q", goos, BinDirEnv, c.Dirs.Bin, c.BinSource)
		}
	}
}

func TestResolveExampleConfig(t *testing.T) {
	t.Parallel()
	xdg := t.TempDir()
	writeConfig(t, xdg, "bin_dir: \"~/x\"\nversion_cache_ttl: \"1h\"\n")
	c, err := Resolve(envOf("linux", map[string]string{"HOME": "/home/u", "XDG_CONFIG_HOME": xdg}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Dirs.Bin != "/home/u/x" {
		t.Errorf("Bin = %q, want /home/u/x", c.Dirs.Bin)
	}
	if c.VersionCacheTTL != time.Hour {
		t.Errorf("VersionCacheTTL = %v, want 1h", c.VersionCacheTTL)
	}
}

func TestResolveTTL(t *testing.T) {
	t.Parallel()
	xdg := t.TempDir()
	writeConfig(t, xdg, `version_cache_ttl: "1d30m"`)
	c, err := Resolve(envOf("linux", map[string]string{"HOME": "/h", "XDG_CONFIG_HOME": xdg}))
	if err != nil {
		t.Fatal(err)
	}
	if c.VersionCacheTTL != 24*time.Hour+30*time.Minute {
		t.Errorf("VersionCacheTTL = %v", c.VersionCacheTTL)
	}
}

func TestResolveAbsoluteBinDirIsCleaned(t *testing.T) {
	t.Parallel()
	c, err := Resolve(envOf("linux", map[string]string{"HOME": "/h", "XDG_CONFIG_HOME": t.TempDir(), BinDirEnv: "/opt/x/../bin/"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Dirs.Bin != "/opt/bin" {
		t.Errorf("Bin = %q, want /opt/bin", c.Dirs.Bin)
	}
}

func TestResolveInvalid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		file     string
		vars     map[string]string
		contains []string
	}{
		{name: "relative env bin dir", vars: map[string]string{BinDirEnv: "bin"}, contains: []string{`"bin"`, BinDirEnv}},
		{name: "tilde without slash", vars: map[string]string{BinDirEnv: "~bin"}, contains: []string{`"~bin"`}},
		{name: "relative config bin dir", file: `bin_dir: "x/bin"`, contains: []string{`"x/bin"`, "config.cue"}},
		{
			name:     "config bin dir checked under env override",
			file:     `bin_dir: "x/bin"`,
			vars:     map[string]string{BinDirEnv: "/opt/bin"},
			contains: []string{`"x/bin"`, "config.cue"},
		},
		{name: "empty config bin dir", file: `bin_dir: ""`, contains: []string{`""`, "config.cue"}},
		{name: "unknown field", file: `colour: "red"`, contains: []string{"config.cue", "colour"}},
		{name: "wrong type", file: `bin_dir: 3`, contains: []string{"config.cue"}},
		{name: "not concrete", file: `bin_dir: string`, contains: []string{"config.cue"}},
		{name: "syntax", file: `bin_dir: "`, contains: []string{"config.cue"}},
		{name: "bad duration", file: `version_cache_ttl: "90 minutes"`, contains: []string{"version_cache_ttl", "90 minutes"}},
		{name: "go duration form", file: `version_cache_ttl: "1.5h"`, contains: []string{"1.5h"}},
		{
			name:     "every problem reported",
			file:     "bin_dir: \"rel\"\nversion_cache_ttl: \"x\"",
			contains: []string{`"rel"`, `"x"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			xdg := t.TempDir()
			if tt.file != "" {
				writeConfig(t, xdg, tt.file)
			}
			vars := map[string]string{"HOME": "/h", "XDG_CONFIG_HOME": xdg}
			for k, v := range tt.vars {
				vars[k] = v
			}
			_, err := Resolve(envOf("linux", vars))
			requireConfigInvalid(t, err, tt.contains...)
		})
	}
}

func TestResolveUnreadableConfig(t *testing.T) {
	t.Parallel()
	xdg := t.TempDir()
	// A directory where the file should be cannot be read on any platform or as any user.
	if err := os.MkdirAll(filepath.Join(xdg, "p3bot", "config.cue"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve(envOf("linux", map[string]string{"HOME": "/h", "XDG_CONFIG_HOME": xdg}))
	requireConfigInvalid(t, err, "config.cue")
}

func TestResolveHome(t *testing.T) {
	t.Parallel()
	_, err := Resolve(envOf("linux", map[string]string{}))
	requireConfigInvalid(t, err, "HOME")
	if n := strings.Count(err.Error(), "HOME"); n != 1 {
		t.Errorf("HOME reported %d times: %q", n, err)
	}

	all := map[string]string{
		"XDG_CONFIG_HOME": t.TempDir(),
		"XDG_DATA_HOME":   "/d",
		"XDG_STATE_HOME":  "/s",
		"XDG_CACHE_HOME":  "/c",
		BinDirEnv:         "/b",
	}
	if _, err := Resolve(envOf("linux", all)); err != nil {
		t.Errorf("HOME is not needed when every location is absolute: %v", err)
	}

	all[BinDirEnv] = "~/b"
	_, err = Resolve(envOf("linux", all))
	requireConfigInvalid(t, err, "HOME")
}
