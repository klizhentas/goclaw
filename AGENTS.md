# AGENTS.md

## Mission
Implement goclaw with a strict minimal-first approach: small, correct, locally testable increments.

## Source of Truth
1. `rfds/` is the canonical design source for architecture and feature decisions.
2. `GOCLAW.md` is a compatibility pointer only.
3. If implementation details are unclear, prefer the latest accepted RFD over ad-hoc assumptions.
4. If docs and code conflict, align code to accepted RFDs unless user says otherwise.
5. Repository standards are defined in `rfds/0002-repo-guidelines.md`.

## Engineering Essentials
1. Keep changes minimal, explicit, and test-backed.
2. Preserve deterministic backend policy enforcement (auth/scope checks in backend, not model behavior).
3. Use structured logging with `log/slog` in core paths.
4. Do not log secrets (API keys, auth headers, tokens).
5. Prefer incremental PR-sized changes over rewrites.

## Repo Workflow (Required)
1. Format and test via `Makefile` targets:
```bash
make fmt
make test
make test-race
```
2. Run services via `Makefile` targets:
```bash
make run-sender
make run-worker
make run-scheduler
```
3. Build binary via:
```bash
make build
```

## Runtime Defaults
1. Runtime artifacts live under `data/` by default.
2. Build artifacts live under `build/`.
3. Policy file default path is `./data/goclaw.toml`.

## Scope Guardrails
1. Do not add speculative abstractions.
2. Keep packages concrete and small.
3. Add complexity only with clear acceptance criteria and tests.

## Security and Secrets
1. Load secrets from environment.
2. Never commit secrets.
3. Keep tool execution deny-by-default and allowlist-driven.

## Design References
For overall system design and evolution, see:
- `rfds/`
- `rfds/0000-core-v1.md`
