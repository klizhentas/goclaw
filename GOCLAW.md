# GOCLAW: Minimalist Reimplementation Plan (Go + Codex API)

## Why This Plan Exists
The original system is powerful, but complexity can hide correctness problems.  
This plan is intentionally biased toward **minimalism first**:

- Fewer moving parts
- Fewer abstractions
- Fast local testing
- Clear failure modes
- Easy to reason about and debug

The objective is not feature parity on day one.  
The objective is to ship a **small, correct core** and only add complexity with evidence.

## Minimalism Principles (Non-Negotiable)
1. Build the smallest thing that can run end-to-end locally.
2. Add one capability at a time; every new feature must justify itself.
3. No framework architecture “just in case.”
4. Prefer explicit code over generic abstractions.
5. Persist only what you need to recover safely.
6. Keep one clear execution path before adding alternatives.
7. Use measurable acceptance criteria for each phase.

## Product Scope for v1 (Strict)
v1 should do exactly this:

1. Accept inbound messages (local CLI first; Slack optional after CLI works).
2. Store conversation history in SQLite.
3. Ensure one active worker per conversation.
4. Call Codex API with recent context.
5. Stream assistant output back to user.
6. Persist assistant response.

Interface requirements for v1:
7. Use trigger-based natural language commands (not slash-command parsing).
8. Support main vs non-main conversation roles.
9. Support task intents in natural language (`list`, `pause`, `resume`, `cancel`, `schedule`) through model + tool calls.

Not in v1:
- Containers
- IPC file bridge
- Scheduler / cron jobs
- Multi-channel routing
- Advanced permission UX
- Subagents / swarms
- Complex mount security model

## Chat Interface Contract (Must Match)
The interface is conversational and trigger-driven.

1. Messages are normal chat text, not rigid command syntax.
2. Trigger prefix defaults to `@Andy` and is configurable (`ASSISTANT_NAME`).
3. Non-main conversations require trigger by default.
4. Main conversation is admin/control and can run without trigger (configurable behavior).
5. Assistant output should be clean chat text (no internal metadata/tags).
6. Use assistant name prefix in outbound when needed by channel mode.

### Supported User Intents (Natural Language)
From any conversation:
- Talk to assistant: `@Andy summarize this`
- Task operations for current conversation:
  - `@Andy schedule ...`
  - `@Andy list my tasks`
  - `@Andy pause task <id>`
  - `@Andy resume task <id>`
  - `@Andy cancel task <id>`

From main conversation (admin scope):
- Manage across conversations:
  - `@Andy list all tasks`
  - `@Andy schedule for <conversation>`
  - `@Andy add/register group <name>`

Important:
- Keep this as intent-level behavior, not regex-heavy command parsing.
- The model decides intent; Go code enforces authorization/policy.

### Role and Authorization Expectations
1. Main can act across conversations.
2. Non-main can only act within itself.
3. Unauthorized actions are denied deterministically in backend logic.
4. Authorization must not depend on model obedience.

## Architecture (v1)
Single Go process with 5 modules:

1. `listener`
2. `store`
3. `dispatcher`
4. `model`
5. `outbound`

Data flow:

1. Listener receives message.
2. Store appends message to DB.
3. Dispatcher acquires conversation lock.
4. Model client builds prompt from recent history and streams tokens.
5. Outbound sends stream/result.
6. Store appends assistant message.
7. Dispatcher releases lock.

That is the entire system.

Conversation semantics:
- One active run per conversation.
- Many conversations in parallel (global cap).
- Conversation history is passed as bounded context window.
- Each turn is append-only in store for replay/recovery.

## Suggested Repo Layout
```text
cmd/miniclaw/main.go
internal/app/app.go
internal/config/config.go
internal/store/sqlite.go
internal/conversation/lockmap.go
internal/model/codex_client.go
internal/prompt/builder.go
internal/listener/cli.go
internal/outbound/cli.go
internal/types/types.go
```

Keep this flat. Do not add packages until needed.

## Phase Plan

### Phase 0: Bootstrap (Day 1)
Goal: runnable shell with no model call.

Tasks:
1. Initialize Go module.
2. Add config loader (`.env` + env vars).
3. Add structured logger.
4. Build CLI loop that reads input and echoes.

Acceptance:
- `go run ./cmd/miniclaw` starts and accepts text input.

### Phase 1: Persistence (Day 1-2)
Goal: durable message history.

Schema (minimum):
- `conversations(id TEXT PRIMARY KEY, created_at TEXT, updated_at TEXT)`
- `messages(id TEXT PRIMARY KEY, conversation_id TEXT, role TEXT, content TEXT, created_at TEXT)`

Tasks:
1. Initialize SQLite.
2. Add `CreateConversationIfMissing`.
3. Add `AppendMessage`.
4. Add `GetRecentMessages(conversationID, limit)`.

Acceptance:
- Restart process, history remains.
- Basic DB tests pass.

### Phase 2: Conversation Serialization (Day 2)
Goal: prevent concurrent corruption per conversation.

Tasks:
1. Implement in-memory lock map (`map[conversationID]*sync.Mutex`).
2. Wrap message handling with lock/unlock.
3. Add a global semaphore for max active conversations (e.g. 3).

Acceptance:
- Parallel test confirms one active handler per conversation.
- Different conversations can run concurrently.

### Phase 3: Codex API Integration (Day 2-3)
Goal: real assistant responses with streaming.

Tasks:
1. Build minimal Codex client wrapper.
2. Send prompt using recent history.
3. Stream tokens/events to outbound.
4. Persist final assistant response.

Acceptance:
- User sees streamed response locally.
- Assistant reply appears in DB.

Note:
- Keep provider adapter thin. No abstraction layers for multiple LLM vendors yet.

### Phase 4: Prompt Builder (Day 3)
Goal: stable, predictable context strategy.

Tasks:
1. Use fixed-size sliding window (e.g. last 20 messages).
2. Add small fixed system prompt.
3. Truncate very long messages safely.
4. Include role metadata (`main` vs `non-main`) and trigger policy hints in system prompt.
5. Include brief command expectations so task intents resolve consistently.

Acceptance:
- Prompt size bounded.
- Behavior deterministic and debuggable.

Not now:
- vector DB
- semantic memory
- auto summarization pipelines

### Phase 5: Local Tooling Permission Model (Optional v1.1)
Goal: minimal safe tool execution.

Tasks:
1. Define tiny tool set (maybe just `read_file` and `run_command`).
2. Hard allowlist for command prefixes and file roots.
3. Deny-by-default policy check before execution.

Acceptance:
- Unauthorized path/command always blocked.
- Authorized commands logged with audit trail.

Rule:
- Keep policy in Go, deterministic.
- No interactive permission prompts in v1.

### Phase 6: Slack Listener (v2)
Goal: replace CLI listener while preserving core.

Tasks:
1. Add Slack Events/Webhook adapter.
2. Map Slack channel/thread to `conversation_id`.
3. Reuse same dispatcher/store/model path.
4. Preserve trigger behavior and conversation role semantics.

Acceptance:
- Same core logic works unchanged for Slack.

## Anti-Bloat Guardrails
If a change violates any of these, reject it:

1. Introduces a new subsystem without production evidence.
2. Adds indirection without removing real complexity.
3. Requires more than one way to do the same thing.
4. Adds config knobs before a concrete need exists.
5. Expands feature scope before reliability of current scope is proven.

## Reliability Rules
1. Every inbound message gets an idempotency key.
2. Store-first handling: persist inbound before model call.
3. Timeouts on all model calls.
4. Clear retry policy (small bounded retries).
5. On crash, replay incomplete conversations safely.
6. Never execute cross-conversation actions without backend auth checks.

## Observability (Minimal but Sufficient)
Log fields:
- `conversation_id`
- `message_id`
- `stage` (ingress, prompt_build, model_stream, persist, egress)
- `duration_ms`
- `error`

Metrics (optional early):
- active conversations
- queue depth
- response latency p50/p95
- error rate

Do not add full monitoring stack before local correctness.

## Test Strategy
Unit tests:
1. DB CRUD and ordering.
2. Prompt builder truncation/window behavior.
3. Locking semantics.
4. Policy checks (if tools enabled).
5. Trigger gating behavior (main vs non-main).
6. Authorization behavior for task/group intents.

Integration tests:
1. CLI input -> model -> persisted output path.
2. Concurrent conversations.
3. Crash/restart recovery with existing DB.

Manual test checklist:
1. Send 10 quick messages in one conversation: no overlap bugs.
2. Send messages in 3 conversations: concurrency works.
3. Kill process mid-response and restart: no DB corruption.
4. Non-main message without trigger does not run.
5. Main conversation can run admin intents.
6. Non-main cannot pause/cancel another conversation's task.

## Migration Path from Minimal -> Advanced
Only after v1 is stable:

1. Add Slack listener (keep same core).
2. Add summary compression for long conversations.
3. Add simple scheduler.
4. Consider containerized tool execution if needed.
5. Consider richer permission UX if needed.

Each step must preserve existing tests and keep architecture understandable.

## Detailed Implementation Checklist
1. Create module and folders.
2. Implement config + logger.
3. Implement SQLite init and schema migration.
4. Implement repository methods.
5. Implement conversation lock map + global semaphore.
6. Implement CLI listener/outbound.
7. Implement Codex client with streaming.
8. Implement prompt builder (fixed window).
9. Implement interface policy module:
   - trigger check
   - conversation role check
   - auth guard for cross-conversation actions
10. Wire request lifecycle in `app.go`.
11. Add graceful shutdown and context cancellation.
12. Add tests for DB, locks, prompt builder, interface policy.
13. Run local soak test (long conversation + concurrency).

## Definition of Done (v1)
v1 is done when:
1. End-to-end local conversation works with streamed Codex responses.
2. One-conversation serialization is guaranteed.
3. Multiple conversations run concurrently within a global cap.
4. All messages are persisted and recoverable after restart.
5. Codebase remains small and easy to explain in under 10 minutes.
6. Interface contract is met: triggering, role scoping, and task intent behavior.

## Final Note
If you must choose between “more features” and “more confidence,” choose confidence.  
Minimalism is not fewer capabilities forever; it is the fastest path to a robust core.
