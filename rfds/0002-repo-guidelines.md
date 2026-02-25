# Repository Guidelines

## Language, Purpose, and Modules
- The project written in Go. Backend uses SQL with pluggable drivers, starting with SQLite (prefer pure Go drivers to avoid CGO).
- Backend entrypoints live under `cmd/`. Shared libraries belong in `pkg/`; app-specific helpers go in `internal/`.
- Keep packages small, acyclic, and testable; pass `context.Context` where work can block or cancel.

## Tooling and Build Basics
- Format with `gofmt` (or `go fmt ./...`); organize imports with `goimports`. Run `go vet ./...` before sending changes.
- CLI parsing should use `alecthomas/kong` for structured subcommands, typed flags, and native `--help` behavior.
- CLI UX should follow `clig.dev` guidance: clear error messages, discoverable commands, focused help per subcommand, and actionable examples.
- Use `gravitational/trace` for error creation and wrapping (`trace.Wrap`, `trace.BadParameter`, etc.). Avoid bare `fmt.Errorf` unless formatting non-error values.
- Use `log/slog` for structured logging; prefer text handler locally. Do not call `log.Fatal`/`slog.Error` with exits from libraries—propagate errors and let `main` decide when to `os.Exit(1)`.
- When adding dependencies, run `go mod tidy` to keep `go.mod`/`go.sum` clean.

## Concurrency and Locking
- Prefer a clean `defer`-based release model for all locks/semaphores/resources: acquire once, release once in a nearby `defer`.
- Avoid scattered inline `Unlock()`/release calls across multiple branches; this pattern is error-prone and increases deadlock risk.
- Keep lock scope explicit and minimal; avoid nested lock acquisition unless strictly required and documented.
- When using callbacks/events that may re-enter code paths, avoid invoking lock-taking callbacks while holding locks.

## Testing Guidelines
- Default to table-driven tests in `*_test.go`. Use Go's `testing` package and `stretchr/testify/require` for assertions where it improves clarity.
- Run `go test ./... -race` for full checks; `go test ./...` for quick iteration. Add coverage flags when touching critical paths.
- For database-backed tests, keep the storage layer behind an interface so SQLite setups (or in-memory variants) can be used without external services.

## Database and Storage Practices
- Define a storage interface (e.g., `pkg/storage`) that captures required queries/transactions. Keep SQL in one place and prefer prepared statements.
- Keep migrations versioned and idempotent. Start with SQLite; design for additional drivers (e.g., Postgres) without leaking driver-specific behavior into callers.
- Use context-aware timeouts for DB calls and wrap driver errors with `trace.Wrap` to preserve root causes.
- Prefer `trace.BadParameter`, `trace.NotFound`, `trace.AccessDenied`, etc., for domain errors so callers can branch on error kind.

## Contribution Workflow
- Write concise, imperative commit subjects (e.g., "Add sqlite storage driver"). Include rationale and tests run in PR descriptions.
- Update docs or examples when changing behavior, flags, or API contracts. Note follow-up work or known gaps explicitly.
- Never commit secrets; use environment variables/config files for local dev secrets and keep fixtures sanitized.
