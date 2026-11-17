// Package config resolves p3bot's directories and the settings in config.cue.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"

	"github.com/p3bot/p3bot/internal/duration"
	"github.com/p3bot/p3bot/internal/fault"
)

const name = "p3bot"

// BinDirEnv overrides the bin dir ahead of config.cue. Empty counts as unset.
const BinDirEnv = "P3BOT_BIN_DIR"

// DefaultVersionCacheTTL applies when config.cue does not set version_cache_ttl.
const DefaultVersionCacheTTL = time.Hour

// Bin dir sources other than a config file path.
const (
	SourceDefault = "default"
	SourceEnv     = BinDirEnv
)

//go:embed schema.cue
var schemaSrc []byte

// Env is the part of the process environment Resolve reads, captured by the
// caller so resolution depends only on its inputs.
type Env struct {
	GOOS   string
	Getenv func(string) string
}

// Dirs holds the absolute directories p3bot uses. Nothing here is created by
// resolution.
type Dirs struct {
	Bin    string
	Config string
	Data   string
	State  string
	Cache  string
}

// Config is the resolved configuration.
type Config struct {
	Dirs Dirs
	// BinSource names where Dirs.Bin came from: SourceDefault, SourceEnv, or
	// the config file path.
	BinSource       string
	File            string
	VersionCacheTTL time.Duration
}

type tier struct {
	xdgEnv    string
	unixRel   string
	darwinRel string
	darwinSub string
}

var (
	configTier = tier{"XDG_CONFIG_HOME", ".config", "Library/Application Support", "config"}
	dataTier   = tier{"XDG_DATA_HOME", ".local/share", "Library/Application Support", "data"}
	stateTier  = tier{"XDG_STATE_HOME", ".local/state", "Library/Application Support", "state"}
	cacheTier  = tier{"XDG_CACHE_HOME", ".cache", "Library/Caches", ""}
)

type fileConfig struct {
	BinDir          *string `json:"bin_dir"`
	VersionCacheTTL *string `json:"version_cache_ttl"`
}

type problem struct {
	message string
	fix     string
}

type resolver struct {
	env          Env
	home         string
	homeReported bool
	problems     []problem
}

func (r *resolver) fail(message, fix string) {
	r.problems = append(r.problems, problem{message, fix})
}

// homeDir returns $HOME, recording a problem once when it is needed but not
// an absolute path.
func (r *resolver) homeDir() (string, bool) {
	if r.home != "" {
		return r.home, true
	}
	if h := r.env.Getenv("HOME"); filepath.IsAbs(h) {
		r.home = filepath.Clean(h)
		return r.home, true
	}
	if r.homeReported {
		return "", false
	}
	r.homeReported = true
	r.fail("HOME is not set to an absolute path, so default directories cannot be resolved", "set HOME to your home directory")
	return "", false
}

func (r *resolver) tierDir(t tier) string {
	if base := r.env.Getenv(t.xdgEnv); filepath.IsAbs(base) {
		return filepath.Join(base, name)
	}
	home, ok := r.homeDir()
	if !ok {
		return ""
	}
	if r.env.GOOS == "darwin" {
		return filepath.Join(home, filepath.FromSlash(t.darwinRel), name, t.darwinSub)
	}
	return filepath.Join(home, filepath.FromSlash(t.unixRel), name)
}

// binDir validates a bin dir value: an absolute path, or one starting with
// ~/ that expands against $HOME.
func (r *resolver) binDir(value, source, fix string) string {
	switch {
	case filepath.IsAbs(value):
		return filepath.Clean(value)
	case strings.HasPrefix(value, "~/"):
		home, ok := r.homeDir()
		if !ok {
			return ""
		}
		return filepath.Join(home, value[2:])
	default:
		r.fail(fmt.Sprintf("bin dir %q from %s is not an absolute path or a ~/ path", value, source), fix)
		return ""
	}
}

// Resolve returns the configuration for env. Every problem found is reported
// in one CONFIG_INVALID error. A missing config.cue is not a problem.
func Resolve(env Env) (Config, error) {
	r := &resolver{env: env}
	var c Config
	c.Dirs.Config = r.tierDir(configTier)
	c.Dirs.Data = r.tierDir(dataTier)
	c.Dirs.State = r.tierDir(stateTier)
	c.Dirs.Cache = r.tierDir(cacheTier)

	var file fileConfig
	if c.Dirs.Config != "" {
		c.File = filepath.Join(c.Dirs.Config, "config.cue")
		if err := load(c.File, &file); err != nil {
			r.fail(err.Error(), "correct or remove "+c.File)
			file = fileConfig{}
		}
	}

	// The file's bin_dir is checked even when overridden, so the file is valid
	// or not regardless of the environment it is read in.
	var fileBin string
	if file.BinDir != nil {
		fileBin = r.binDir(*file.BinDir, c.File, "set bin_dir in "+c.File+" to an absolute path or a ~/ path")
	}
	switch v := env.Getenv(BinDirEnv); {
	case v != "":
		c.BinSource = SourceEnv
		c.Dirs.Bin = r.binDir(v, BinDirEnv, "set "+BinDirEnv+" to an absolute path or a ~/ path, or unset it")
	case file.BinDir != nil:
		c.BinSource = c.File
		c.Dirs.Bin = fileBin
	default:
		c.BinSource = SourceDefault
		if home, ok := r.homeDir(); ok {
			c.Dirs.Bin = filepath.Join(home, ".local", "bin")
		}
	}

	c.VersionCacheTTL = DefaultVersionCacheTTL
	if file.VersionCacheTTL != nil {
		ttl, err := duration.Parse(*file.VersionCacheTTL)
		if err != nil {
			r.fail(fmt.Sprintf("version_cache_ttl in %s: %v", c.File, err), "set version_cache_ttl in "+c.File+" to a duration such as 1h, or remove it")
		}
		c.VersionCacheTTL = ttl
	}

	if len(r.problems) > 0 {
		msgs := make([]string, len(r.problems))
		fixes := make([]string, len(r.problems))
		for i, p := range r.problems {
			msgs[i], fixes[i] = p.message, p.fix
		}
		return Config{}, fault.New(fault.ConfigInvalid, strings.Join(msgs, "; "), strings.Join(fixes, "; "))
	}
	return c, nil
}

// load decodes path into out. A missing file leaves out unset; any other read
// failure is an error, because guessing past an unreadable file could install
// somewhere the user did not choose.
func load(path string, out *fileConfig) error {
	src, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	ctx := cuecontext.New()
	schema := ctx.CompileBytes(schemaSrc).LookupPath(cue.ParsePath("#Config"))
	v := schema.Unify(ctx.CompileBytes(src, cue.Filename(path)))
	if err := v.Validate(cue.Concrete(true)); err != nil {
		return errors.New(describe(path, err))
	}
	if err := v.Decode(out); err != nil {
		return errors.New(describe(path, err))
	}
	return nil
}

// describe renders CUE errors as "path:line:col: field: message", locating
// each in the user's file rather than the embedded schema.
func describe(path string, err error) string {
	var parts []string
	for _, e := range cueerrors.Errors(err) {
		loc := path
		for _, pos := range cueerrors.Positions(e) {
			if pos.Filename() == path {
				loc = fmt.Sprintf("%s:%d:%d", path, pos.Line(), pos.Column())
				break
			}
		}
		format, args := e.Msg()
		msg := fmt.Sprintf(format, args...)
		if field := strings.Join(slices.DeleteFunc(e.Path(), func(s string) bool { return s == "#Config" }), "."); field != "" {
			msg = field + ": " + msg
		}
		parts = append(parts, loc+": "+strings.Join(strings.Fields(msg), " "))
	}
	return strings.Join(parts, "; ")
}
