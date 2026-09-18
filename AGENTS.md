# Repository Guidelines

## Project Structure & Module Organization

Tether is a Go 1.26 communications hub with one root `main` package. `main.go` dispatches CLI commands; `store.go` persists JSON; `imap.go`, `ics.go`, and `gcal.go` integrate mail and calendars. `llm.go` and `triage.go` classify threads; `discord.go` and `interactions.go` handle Discord.

Tests are colocated as `*_test.go`; dependencies live in `vendor/`. `flake.nix` defines packaging and formatting; `tether.nix` defines services.

## Build, Test, and Development Commands

On this Nix system, supply tools explicitly:

- `nix-shell -p go gnumake --run 'make build'`: build the `tether` executable using vendored dependencies.
- `nix-shell -p go gnumake --run 'make test'`: run all Go tests.
- `nix-shell -p go gnumake --run 'make fmt'`: format Go source with `gofmt`.
- `nix fmt`: format Nix files with the flake's `nixfmt` formatter.
- `TETHER_STATE_DIR=/tmp/tether-dev ./tether list threads`: inspect an isolated development store.

`make deploy` tests and rsyncs to atlas with deletion enabled. `make provision` writes remote credentials.

## Coding Style & Naming Conventions

Use `gofmt` tabs and formatting for Go. As a Go-specific exception to the global snake_case preference, match existing PascalCase exports and camelCase internal identifiers. Keep filenames lowercase, related behavior together, and dependencies minimal. Handle operational errors explicitly. Update all callers when changing interfaces and remove unused code. Format changed code before finishing.

## Architectural Safeguards

Preserve exclusive store locking and release it after each command; idle bot services must not retain it. Verify Discord interaction signatures and restrict commands to the configured channel. Keep interaction concurrency bounded at four and acknowledge requests before slow work. Preserve nudge quiet hours, cooldowns, and daily caps. Test email direction and thread state transitions when changing follow-ups.

## Testing Guidelines

Use Go's standard `testing` package, `TestMeaningfulBehavior` names, and table-driven cases where useful. Use `t.TempDir()` for stores and `httptest` for HTTP integrations. Cover failures and state transitions without live services. Run `make test` in the Nix shell for behavior changes. No numeric coverage threshold is configured.

## Commit & Pull Request Guidelines

Git history contains only `init: tether flake package`; it establishes no broader PR convention. Use concise, lowercase conventional titles such as `fix: handle empty llm responses`. Commit only when requested; omit attribution trailers and unnecessary bodies. Recommended PR descriptions explain behavior changes, relevant issues, and validation performed.

## Configuration & Secrets

Consult `README.md` and `secretspec.toml` for configuration. Never commit credentials, private calendar URLs, or user state. Develop with a scratch `TETHER_STATE_DIR`.
