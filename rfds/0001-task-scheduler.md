# RFD 0001 - Scheduled Task Execution (SQLite + Queue)

- Status: Implemented (v1)
- Authors: goclaw maintainers
- Created: 2026-02-25
- Updated: 2026-02-25
- Tracking: local implementation in `internal/store`, `internal/app`, `cmd/goclaw`
- Related:
  - `rfds/0000-core-v1.md` (baseline core architecture)

## Summary
Add scheduled task execution to goclaw with a minimal, local-first design:

1. Tasks are persisted in SQLite.
2. Multiple schedulers can run concurrently.
3. Schedulers claim due tasks using compare-and-swap style updates with lease semantics.
4. Claimed tasks are routed through the existing inbound queue and processed by workers.
5. Task run results are appended to the conversation and persisted in task run history.

## Motivation
The system needs delayed and recurring execution without introducing RPC, distributed coordinators, or external dependencies in v1.

## Goals
- Support one-shot and interval tasks bound to a `conversation_id`.
- Keep backend authorization deterministic (no model-authorized writes).
- Support multiple scheduler processes safely.
- Reuse existing worker + queue path.
- Apply simple backpressure controls.
- Provide minimal CLI management (`create`, `ls`, `rm`, `update`).

## Non-Goals
- RPC scheduler API.
- Cron syntax and calendar scheduling.
- Rich ownership model (groups/ACL graph).
- External distributed lock manager.

## Design

### Data Model
Two new tables:

- `tasks`
  - Scheduling definition and lease state.
  - Key fields: `id`, `conversation_id`, `payload`, `schedule_type`, `run_at`, `interval_sec`, `status`, `next_run_at`, `assigned_scheduler_id`, `lease_expires_at`, `failure_count`, `last_error`.

- `task_runs`
  - Immutable run records for audit and retries.
  - Key fields: `id`, `task_id`, `conversation_id`, `payload`, `status`, `scheduler_id`, `started_at`, `finished_at`, `result_content`, `error`, `inbound_queue_message_id`, `assistant_message_id`.

### Scheduler Claiming (multi-scheduler safe)
Claim flow:

1. Select one due task (`status=active`, `next_run_at <= now`).
2. Require lease available (`lease_expires_at` missing or expired).
3. Exclude tasks with active queued/running run.
4. CAS-style update to set `assigned_scheduler_id` and `lease_expires_at`.
5. If update affects 0 rows, another scheduler won; retry later.

Lease is bounded by `TASK_LEASE_SECONDS`.

### Execution Path
1. Scheduler creates `task_runs` row with `queued`.
2. Scheduler enqueues inbound queue message linked to `task_run_id`.
3. Worker processes inbound message via existing prompt/model path.
4. Worker appends assistant output to conversation and outbound queue.
5. Worker marks task run `success` or `failed`.
6. Task state updates:
   - one-shot: `completed` on success
   - interval: `next_run_at = now + interval`
   - failure: exponential backoff and retry scheduling

### Backpressure
- Scheduler dispatch concurrency: `SCHEDULER_CONCURRENCY`.
- Existing worker global semaphore still applies.
- Existing per-conversation lock guarantees serialization.

### Retry / Backoff
On task run failure:
- increment `failure_count`
- set `next_run_at = now + backoff(failure_count)`
- backoff starts at 30s, doubles, capped at 1h

### Authorization Policy
Backend-enforced update authorization:
- `tasks update` requires `caller-id`.
- caller must match run assignment (`scheduler_id`) when present.
- model output does not bypass backend checks.

## CLI
Implemented subcommands:

- `tasks create --conversation <id> --payload <text> [--at RFC3339] [--every duration]`
- `tasks ls [--conversation <id>]`
- `tasks rm --id <task_id>` (soft delete)
- `tasks update --run-id <id> --caller-id <id> --status success|failed [--result ...] [--error ...]`

## Operational Notes
- Run scheduler with `-mode scheduler` (or `make run-scheduler`).
- Scheduler and worker are independent processes.
- Data and logs live in `./data/` by default.

## Risks / Tradeoffs
- SQLite lease-based coordination can create extra contention at high scale.
- Backoff policy is global/simple, not per-task configurable yet.
- No dedicated dead-letter queue in v1.

## Rollout
1. Ship schema + CLI.
2. Run one scheduler + one worker locally.
3. Validate with multiple schedulers for lease/cas behavior.
4. Introduce RPC layer later without replacing core DB state machine.

## Alternatives Considered
- In-memory scheduler only: rejected (no restart safety).
- External lock service: rejected (too much complexity for v1).
- Cron parser now: deferred to later phase.
