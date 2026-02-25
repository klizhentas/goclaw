# goclaw

Minimal Go assistant runtime with SQLite-backed queues, worker/scheduler loops, and an OpenAI model backend.

## Build

```bash
make build
```

Binary:

```text
build/goclaw
```

## CLI Overview

The CLI uses `alecthomas/kong` subcommand parsing and supports `--help` at every command level.

```bash
goclaw <subcommand> [flags]
```

Primary subcommands:

- `term` (alias: `terminal`) - terminal sender mode
- `worker` - worker mode
- `scheduler` - scheduler mode
- `single` - single-process interactive mode
- `tasks` - task management commands

Examples:

```bash
build/goclaw --help
build/goclaw term --help
build/goclaw tasks --help
build/goclaw tasks create --help
```

If no subcommand is provided, CLI returns an error with usage guidance.

## Subcommands

### `term` / `terminal`

Starts terminal sender mode.

Behavior:

- Reads stdin lines as inbound messages.
- Enqueues inbound messages to SQLite queue.
- Polls outbound queue and prints assistant responses.

Input format:

```text
<conversation_id>: <message>
```

Example:

```text
main: hello
room-1: @Andy summarize this
```

Type `exit` to quit.

### `worker`

Starts worker mode.

Behavior:

- Claims inbound queue messages.
- Builds prompt and calls configured model backend.
- Persists assistant response.
- Enqueues outbound queue message.

### `scheduler`

Starts scheduler mode.

Behavior:

- Claims due tasks with SQLite compare-and-swap + lease semantics.
- Creates task runs.
- Enqueues inbound execution messages for worker processing.

### `single`

Single-process local mode.

Behavior:

- Runs interactive listener and processing in one process.

## `tasks` Command Reference

```bash
build/goclaw tasks <create|ls|rm|update> [flags]
```

### `tasks create`

```bash
build/goclaw tasks create \
  --conversation <conversation_id> \
  --payload "<task text>" \
  [--at RFC3339] \
  [--every duration]
```

Flags:

- `--conversation` required: conversation id
- `--payload` required: execution payload text
- `--at` optional: one-shot schedule time (RFC3339)
- `--every` optional: recurring interval (`30s`, `10m`, `1h`)

Semantics:

- `--every` set => recurring interval task
- `--every` absent => one-shot task
- one-shot with no `--at` => due immediately

Examples:

```bash
build/goclaw tasks create --conversation main --payload "echo hello"
build/goclaw tasks create --conversation main --payload "daily check" --every 10m
build/goclaw tasks create --conversation main --payload "status report" --at 2026-02-25T18:00:00Z
```

### `tasks ls`

```bash
build/goclaw tasks ls [--conversation <conversation_id>]
```

Flags:

- `--conversation` optional: filter by conversation id

### `tasks rm`

```bash
build/goclaw tasks rm --id <task_id>
```

Flags:

- `--id` required: task id

### `tasks update`

```bash
build/goclaw tasks update \
  --run-id <task_run_id> \
  --caller-id <agent_or_scheduler_id> \
  --status success|failed \
  [--result "..."] \
  [--error "..."] \
  [--assistant-message-id <message_id>]
```

Flags:

- `--run-id` required: task run id
- `--caller-id` required: caller identity used for backend authorization
- `--status` required: `success` or `failed`
- `--result` optional: success output content
- `--error` optional: failure error content
- `--assistant-message-id` optional: linked assistant message id

## Make Targets

```bash
make run-sender      # runs: build/goclaw term
make run-worker      # runs: build/goclaw worker
make run-scheduler   # runs: build/goclaw scheduler
make run CMD=single  # generic runner
```

## Model Backends

### Echo (default)

```bash
export MODEL_BACKEND=echo
```

### OpenAI

```bash
export MODEL_BACKEND=openai
export OPENAI_API_KEY=...
export OPENAI_MODEL=gpt-4o-mini
# optional
export OPENAI_BASE_URL=https://api.openai.com
```

Worker startup logs include diagnostics (masked key hint, status, model visibility).

## Tool Calling (OpenAI)

Default policy file:

```text
./data/goclaw.toml
```

Example allowlist:

```toml
[allow]
tools = ["echo", "date", "curl"]
```

When allowlist is non-empty, worker exposes `exec_local_tool` and backend-enforces command allowlist.

## Environment Variables

Core paths and runtime:

- `DATABASE_PATH` default `./data/goclaw.db`
- `LOG_PATH` default `./data/goclaw.log`
- `POLICY_PATH` default `./data/goclaw.toml`
- `MODE` default `single` (internal fallback)

Identity / routing:

- `WORKER_ID` default `worker-1`
- `SENDER_ID` default `sender-1`
- `SCHEDULER_ID` default `scheduler-1`

Concurrency / timing:

- `MAX_ACTIVE_CONVERSATIONS` default `3`
- `QUEUE_POLL_INTERVAL_MS` default `500`
- `REQUEST_TIMEOUT_SECONDS` default `30`
- `TASK_LEASE_SECONDS` default `60`
- `SCHEDULER_CONCURRENCY` default `2`

Assistant behavior:

- `ASSISTANT_NAME` default `Andy`
- `MAIN_CONVERSATION_ID` default `main`
- `NON_MAIN_NEEDS_TRIGGER` default `true`
- `MAIN_NEEDS_TRIGGER` default `false`
- `HISTORY_WINDOW` default `20`
- `LOG_LEVEL` default `INFO`

## Logging

Structured logs are written to `LOG_PATH` (JSON format).

Common fields:

- `pid`
- `mode`
- `worker_id`
- `sender_id`
- `scheduler_id`
- `conversation_id`
- `message_id`
- `stage`
- `duration_ms`
- `error`

## Development Checks

```bash
make fmt
make vet
make test
make test-race
make build
```
