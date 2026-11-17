# p3bot

Guidance for AI agents working in this repository. `p3bot` is the first p3bot tool a user installs, and the one that manages the rest. It installs, updates, uninstalls, lists, and diagnoses the tools in the [p3bot](https://github.com/p3bot) organisation and their dependencies, for people and for the AI agents working on their behalf.

## Status

The module is scaffolded with the CLI foundations. Commands that exist:

| Command | Behaviour |
| --- | --- |
| `p3bot` | Prints help. Resolves configuration first, so a broken config exits 78 |
| `p3bot --version` | Version from build info (`debug.ReadBuildInfo`); `go install` sets no linker flags |
| `p3bot completion bash\|zsh\|fish` | Completion script on stdout. Ignores configuration |

The accepted design is `p3bot-x6fh` in the tk board (`tk design get p3bot-x6fh`). Install, update, uninstall, list, get, env, doctor, skill, and quickstart land in later tickets, and each adds its command to the table above.

Settled decisions:

- Each tool's data (packages, platforms, dependencies, skill, service, data paths) lives in a `p3bot.cue` manifest at the root of that tool's repository, read per version through the Go module proxy
- p3bot does not install through Homebrew and publishes no formulae. Recognising leftover Homebrew copies belongs to `p3bot-d5r4`, and retiring the tap belongs to `p3bot-dw7j`

Output is read by AI agents as well as people. Keep it plain, stable, and parseable.

## Managed tools

The embedded index in `internal/index` names each managed binary and its module. Package paths are not fixed here; they come from each version's manifest. Change this table in the same edit as the index. `TestAgentsTableMatchesIndex` fails when they differ.

| Tool | Module |
| --- | --- |
| start | `github.com/p3bot/start` |
| agentdex | `github.com/p3bot/agentdex` |
| tk | `github.com/p3bot/tk` |
| tkv | `github.com/p3bot/tk` |
| kagi | `github.com/p3bot/kagi` |
| snag | `github.com/p3bot/snag` |
| webctl | `github.com/p3bot/webctl` |
| hotseat | `github.com/p3bot/hotseat` |
| p3bot | `github.com/p3bot/p3bot` |

`p3bot/library` is a CUE module library consumed by `start` through the CUE Central Registry. It is not a binary.

## Stack and layout

| Concern | Choice | Notes |
| --- | --- | --- |
| Module path | `github.com/p3bot/p3bot` | |
| Language | Go | Pure Go, no cgo. Build and test with `CGO_ENABLED=0` |
| Go version | The `go` line in `go.mod` (1.27.1) | The version the tool repositories are aligning on |
| CLI framework | Cobra | Consistent with the other p3bot CLIs |
| Platforms | Linux and macOS, amd64 and arm64 | Windows is a non-goal |
| License | MPL-2.0 | |

Dependencies are limited to `cobra`, `cuelang.org/go`, `agentdex`, `golang.org/x/mod`, `golang.org/x/term`, `fatih/color`, and `goccy/go-yaml`. Add each in the change that first uses it. `spf13/pflag` is Cobra's flag library, not a separate dependency, and may be imported directly.

Layout:

| Path | Holds |
| --- | --- |
| `cmd/p3bot/main.go` | Entry point: run, map a signal or error to an exit code, exit |
| `internal/cli` | Cobra tree, output helpers, JSON envelope, error rendering. One app and tree per run, no package-level flag state |
| `internal/fault` | Error type, codes, warning codes, and the code-to-exit mapping. Imports only the standard library |
| `internal/config` | Directory tiers, bin dir, and `config.cue` loading |
| `internal/duration` | The CLI guide's duration grammar (`30s`, `1h30m`, `7d`, `0`) |
| `internal/index` | Embedded index of managed binaries and modules |

Keep the operational logic out of `internal/cli`. Operational packages import no Cobra, read the environment only through values passed in, and return `*fault.Error`; `internal/cli` renders it unchanged. A user interface beyond the CLI (a local web page served on `127.0.0.1`, or a desktop UI) may be added later and must reuse the same packages. A GUI toolkit that needs cgo is out.

The Go CLI design guide (`start get contexts:golang/design/cli`) is advisory. Adopt its repo-shape conventions. Decisions recorded in this file override it. Where p3bot departs from it: the error type and exit mapping live in `internal/fault`, not `internal/cli`; there is no `--config` flag, system config, profiles, jobs, API client, or pagination; p3bot never prompts.

## Output and errors

- Results go to stdout. Warnings, hints, progress, and `--debug` logs go to stderr
- Global flags: `--json`, `--quiet`, `--no-input`, `--color auto|always|never`, `--verbose`, `--debug`
- `--json` puts one envelope on stdout: `{"status":"ok","data":...,"warnings":[...]}` or `{"status":"error","error":{"code","message","fix","transient","valid_values"}}`. Warnings ride in `warnings`. Next steps for a tool ride in a per-tool `next_commands` list in `data`, because the envelope's `next` is reserved for pagination cursors
- p3bot never prompts. Missing input is `INPUT_REQUIRED` (exit 2) with `valid_values`
- Build errors with `fault.New(code, message, fix)`, adding `WithValidValues` or `WithCause` as needed. It panics on an empty message or fix. Wrapping one with `%w` keeps its code and fix, and the added context shows in the rendered message
- Every code and its exit is defined once in `fault.Code.Exit`. `transient` is derived from the code: true exactly when the exit is 75. The `exhaustive` linter fails if a code is missing from the switch
- An error with no code renders as `UNKNOWN`, exit 1. Exit 75 is a promise that retrying can help; use it only for recognised network, proxy, rate-limit, and timeout failures
- A signal exits 128+signum (Ctrl-C is 130)

Exit codes: 0 ok, 1 failure, 2 usage, 3 not found, 4 permission, 5 conflict, 75 transient, 78 configuration.

## Directories and configuration

| Location | Linux default | macOS default | Holds |
| --- | --- | --- | --- |
| Bin dir | `~/.local/bin` | `~/.local/bin` | Installed binaries |
| Config tier | `~/.config/p3bot` | `~/Library/Application Support/p3bot/config` | `config.cue` |
| Data tier | `~/.local/share/p3bot` | `~/Library/Application Support/p3bot/data` | Reserved for a later binary store. Resolved, nothing written |
| State tier | `~/.local/state/p3bot` | `~/Library/Application Support/p3bot/state` | Ownership records and the lock file. Never synced |
| Cache tier | `~/.cache/p3bot` | `~/Library/Caches/p3bot` | Latest-version lookups |

- An absolute `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, or `XDG_CACHE_HOME` replaces the base on both platforms. Relative values are ignored
- Bin dir precedence: `P3BOT_BIN_DIR`, then `bin_dir` in `config.cue`, then `~/.local/bin`. An empty `P3BOT_BIN_DIR` counts as unset. A value must be absolute or start with `~/`; anything else is `CONFIG_INVALID` naming the value and its source
- `config.cue` holds only `bin_dir` and `version_cache_ttl` (duration grammar, default `1h`). Unknown fields, bad values, and an unreadable file are `CONFIG_INVALID` (exit 78), with every problem reported at once. A missing file is not an error
- There is no `--config` flag and no other p3bot environment variable

## Checks

| Check | Command |
| --- | --- |
| Build | `CGO_ENABLED=0 go build ./...` |
| Test | `CGO_ENABLED=0 go test ./...` |
| Format | `gofmt -l .` prints nothing |
| Vet | `go vet ./...` |
| Lint | `golangci-lint run` (`.golangci.yml`, schema v2) |

`go test -race` needs cgo and is not a project check.

Tests must not touch the real machine. Isolate `$HOME`, XDG directories, and install targets under `t.TempDir()`, and never reach the network in unit tests. `internal/cli` tests drive `app.execute` with an injected `system` (writers, environment, version), so they can run in parallel.

## Conventions

- Commit messages use Scoped Commits (`<scope>: <description>`), with no `Co-Authored-By` trailers
- Do not commit, push, tag, or release without an explicit request
- Agent-facing markdown uses no bold, italic, emojis, horizontal rules, HTML comments, task lists, or headings deeper than `###`
- This repository is cloned into the org workspace at `p3bot/` and is listed in the workspace `scripts/clone-refresh`, `scripts/pull-push`, and `scripts/go-install`
