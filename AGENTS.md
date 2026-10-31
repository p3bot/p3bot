# p3bot

Guidance for AI agents working in this repository. `p3bot` is the first p3bot tool a user installs, and the one that manages the rest. It installs, updates, uninstalls, lists, and diagnoses the tools in the [p3bot](https://github.com/p3bot) organisation and their dependencies, for people and for the AI agents working on their behalf.

## Status

The repository is not scaffolded. It holds `LICENSE` (MPL-2.0) and this file. There is no `go.mod`, no code, and no design document yet.

- Do not scaffold or implement without an explicit request
- Behaviour listed below is intended scope, not a contract. Where a detail is not stated here, ask rather than invent it
- Update this file in the same change that lands the first code, replacing intended scope with what exists

## Intended scope

| Capability | Summary |
| --- | --- |
| Install | Install p3bot tools; report missing dependencies for each tool |
| Update | Update installed tools |
| Uninstall | Remove installed tools |
| List | List all p3bot tools and show which are installed |
| Doctor | Check the installation and environment, and report problems |
| Diagnostics | Other diagnostic information about tools, versions, and the environment |
| Dependency state | Stored data describing each tool's dependencies, used by install and update |

Open decision: where dependency state lives, either centrally in this repository or in each tool's own repository. Do not choose one without confirmation.

Output is read by AI agents as well as people. Keep it plain, stable, and parseable.

## Managed tools

The current tool set, taken from the org workspace `scripts/go-install`. That script installs from local checkouts; `p3bot` is expected to replace it for users.

| Tool | Repository | Package |
| --- | --- | --- |
| start | p3bot/start | `./cmd/start` |
| agentdex | p3bot/agentdex | `./cmd/agentdex` |
| tk | p3bot/tk | `./cmd/tk` |
| tkv | p3bot/tk | `./cmd/tkv` |
| kagi | p3bot/kagi | `.` |
| snag | p3bot/snag | `./cmd/snag` |
| webctl | p3bot/webctl | `./cmd/webctl` |
| hotseat | p3bot/hotseat | `./cmd/hotseat` |

`p3bot/library` is a CUE module library consumed by `start` through the CUE Central Registry. It is not a binary. `p3bot/homebrew-tap` publishes Homebrew formulae for agentdex, kagi, snag, start, tk, and webctl; how `p3bot` relates to Homebrew is undecided.

## Stack and layout

| Concern | Choice | Notes |
| --- | --- | --- |
| Module path | `github.com/p3bot/p3bot` | |
| Language | Go | Pure Go, no cgo. Build and test with `CGO_ENABLED=0` |
| Go version | The `go` line in `go.mod` | Sibling repos range from 1.25 to 1.27 |
| CLI framework | Cobra | Consistent with the other p3bot CLIs |
| License | MPL-2.0 | |

Intended layout:

- `cmd/p3bot/main.go`: minimal entry point; run, map a signal or error to an exit code, exit
- `internal/cli`: Cobra command tree, kept thin
- `internal/...`: install, update, uninstall, list, doctor, and dependency logic, with no Cobra imports

Keep the operational logic out of `internal/cli`. A user interface beyond the CLI (a local web page served on `127.0.0.1`, or a desktop UI) may be added later and must reuse the same packages. A GUI toolkit that needs cgo is out.

The Go CLI design guide (`start get golang/design/cli`) is advisory. Adopt its repo-shape conventions. Decisions recorded in this file override it.

## Checks

Once `go.mod` exists:

| Check | Command |
| --- | --- |
| Build | `CGO_ENABLED=0 go build ./...` |
| Test | `CGO_ENABLED=0 go test ./...` |
| Format | `gofmt -l .` prints nothing |
| Vet | `go vet ./...` |
| Lint | `golangci-lint run` (`.golangci.yml`, schema v2) |

`go test -race` needs cgo and is not a project check.

Tests that install, update, or uninstall must not touch the real machine. Isolate `$HOME`, XDG directories, and install targets under `t.TempDir()`, and never reach the network in unit tests.

## Conventions

- Commit messages use Scoped Commits (`<scope>: <description>`), with no `Co-Authored-By` trailers
- Do not commit, push, tag, or release without an explicit request
- Agent-facing markdown uses no bold, italic, emojis, horizontal rules, HTML comments, task lists, or headings deeper than `###`
- This repository is cloned into the org workspace at `p3bot/` and is listed in the workspace `scripts/clone-refresh` and `scripts/pull-push`. Add it to `scripts/go-install` once `cmd/p3bot` exists
