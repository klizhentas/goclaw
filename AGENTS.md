# AGENTS.md

## Mission
Implement the system described in `GOCLAW.md` with a strict minimal-first approach.

Primary goal:
- Ship a small, correct, locally testable Go system before adding features.

This repository is now optimized for:
- Golang implementation
- OpenAI API integration
- Local testing and fast iteration
- Structured logging with `log/slog`

## Source of Truth
1. `GOCLAW.md` is the execution blueprint.
2. If code and docs conflict, align code to `GOCLAW.md` unless user says otherwise.
3. Avoid speculative architecture not required by current phase.

## Non-Negotiable Engineering Principles
1. Minimalism over feature breadth.
2. Correctness over cleverness.
3. Explicitness over abstractions.
4. Local reproducibility over production polish in early phases.
5. Deterministic backend policy enforcement over model-dependent behavior.

## Stack and Standards
- Language: Go (1.22+ preferred)
- LLM API: OpenAI API
- Logging: `log/slog` (structured logs only)
- Persistence: SQLite
- Runtime: Single process for v1

## Implementation Scope (Current)
Implement only the v1 core from `GOCLAW.md`:
1. Inbound listener (CLI first).
2. SQLite conversation/message storage.
3. One active worker per conversation.
4. Global concurrency cap across conversations.
5. OpenAI streaming response path.
6. Persist assistant messages.
7. Trigger and role semantics (main vs non-main).

Do not implement yet:
- Containers
- IPC bridge
- Scheduler/cron
- Multi-channel fanout
- Complex permission UX
- Multi-provider abstraction layers

## Interface Contract (Must Preserve)
1. Natural-language chat interface.
2. Trigger-based invocation (`@Andy` default, configurable).
3. Non-main conversations require trigger by default.
4. Main conversation supports admin scope and looser triggering policy.
5. Backend enforces authorization/scoping rules; model does not decide permissions.

## Logging Requirements (`slog`)
Use structured logs consistently with stable fields.

Minimum fields:
- `conversation_id`
- `message_id`
- `stage` (`ingress`, `persist_in`, `prompt_build`, `model_stream`, `persist_out`, `egress`)
- `duration_ms` (when applicable)
- `error` (when applicable)

Rules:
1. No ad-hoc `fmt.Println` debug logs in core path.
2. Log one start and one completion/error event per stage.
3. Redact secrets/tokens from logs.

## OpenAI API Guidelines
1. Keep OpenAI client integration thin and isolated in one package (`internal/model`).
2. Use streaming for user-visible output.
3. Apply bounded request timeout via context.
4. Persist final assistant message after stream completion.
5. Never log API keys or raw auth headers.

## Concurrency Model
1. Per-conversation serialization via lock map.
2. Global semaphore limits simultaneous active conversations.
3. Store inbound message before starting model call.
4. Release locks and semaphore in `defer` paths to avoid deadlocks.

## Data Model (Minimum)
Tables:
1. `conversations`
2. `messages`

Behavior:
1. Append-only messages for audit/recovery.
2. Deterministic ordering by creation time + stable IDs.
3. Bounded history window retrieval for prompt building.

## Testing Policy (Local-First)
Every meaningful change should be locally verifiable.

Required:
1. Unit tests for DB operations.
2. Unit tests for prompt builder window/truncation behavior.
3. Unit tests for trigger and role policy checks.
4. Concurrency tests for one-worker-per-conversation guarantee.

Recommended commands:
```bash
make fmt
make test
make test-race
```

Use the `Makefile` targets for formatting and tests by default.

Run modes should use `Makefile` targets by default:
```bash
make run-sender
make run-worker
```

Manual local checks:
1. Multiple quick messages in one conversation do not overlap.
2. Multiple conversations process concurrently up to cap.
3. Restart process preserves history.
4. Non-main without trigger does not execute.
5. Main scope commands pass role checks.

## Development Workflow
For each task:
1. Confirm phase alignment with `GOCLAW.md`.
2. Implement smallest viable code change.
3. Add or update tests.
4. Run local tests.
5. Summarize what changed and what remains.

Prefer incremental PR-sized diffs, not big-bang rewrites.

## Code Organization
Recommended layout:
```text
cmd/miniclaw/main.go
internal/app/
internal/config/
internal/store/
internal/conversation/
internal/model/
internal/prompt/
internal/listener/
internal/outbound/
internal/types/
```

Rules:
1. Keep packages small and concrete.
2. No generic framework package.
3. No interface proliferation without active second implementation.

## Security and Secrets
1. Load secrets from environment only.
2. Never commit secrets.
3. Never print secrets in logs or errors.
4. Validate and constrain any future tool execution paths with deny-by-default policy.

## Decision Rules for Adding Complexity
Complexity is allowed only if:
1. Current minimal path is proven inadequate by real test evidence.
2. The new complexity has explicit acceptance criteria.
3. Tests are added to protect behavior.
4. Change remains explainable quickly.

If these are not met, defer the change.

## Definition of Done (v1)
v1 is done when:
1. Local CLI conversation loop works with OpenAI streaming.
2. Conversation serialization and global concurrency limits are reliable.
3. History persists across restarts.
4. Trigger/role interface behavior matches `GOCLAW.md`.
5. Logs are structured via `slog` and useful for debugging.
6. Test suite passes locally, including race checks.
