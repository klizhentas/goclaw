# goclaw Reference

Detailed CLI, runtime, and configuration reference.

## CLI Overview

The CLI uses `alecthomas/kong` subcommand parsing and supports `--help` at every command level.

```bash
goclaw <subcommand> [flags]
```

Global flags:

- `--log-level <debug|info|warn|error>` process log level override
- `-d` shortcut for debug logs (`--log-level=debug`)

Primary subcommands:

- `run` - run one or more runtime modes in one process (`--mode=term,scheduler`)
- `term` (alias: `terminal`) - terminal sender mode
- `worker` - worker mode
- `scheduler` - scheduler mode
- `single` - single-process interactive mode
- `tasks` - task management commands

Examples:

```bash
build/goclaw --help
build/goclaw run --help
build/goclaw term --help
build/goclaw tasks --help
build/goclaw tasks create --help
```

If no subcommand is provided, CLI returns an error with usage guidance.

## Subcommands

### `run`

Runs one or more modes concurrently.

```bash
build/goclaw run --mode=<term|worker|scheduler|single>[,...] [--worker-id <id>] [--theme <light|dark>]
```

Examples:

```bash
build/goclaw run --mode=term,scheduler
build/goclaw run --mode=term,scheduler --theme dark
build/goclaw run --mode=worker
build/goclaw run --mode=worker --worker-id worker-2
build/goclaw run --mode=single
```

Notes:

- `single` cannot be combined with other modes.
- `term,scheduler` is useful for local scheduled-task testing from one process.

### `term` / `terminal`

Starts terminal sender mode.

```bash
build/goclaw term [--theme <light|dark>]
```

Behavior:

- Opens a `tview` terminal UI with:
  - vertical conversation tabs on the left
  - message window on the right
  - input field at the bottom
- Enqueues inbound messages to SQLite queue.
- Polls outbound queue and prints assistant responses.
- Updates active conversation in-place when responses arrive.
- Supports light/dark themes (`--theme`, default `light`).
- Supports explicit conversation commands in input:
  - `/new <conversation_id>`
  - `/switch <conversation_id>`
  - `/rename <conversation_id>`
  - `/remove [conversation_id]`
  - `/help`
- Supports a debug dashboard toggle via `Ctrl+g`.
- Status panel shows last keypress and runtime hints.
- Re-pressing an already selected `Alt+<n>` tab flashes the tab briefly as press feedback.
- Debug dashboard shows yellow warnings when worker capacity is insufficient (for example, open conversations exceed observed workers).

Input format:

```text
<message>                     # send to active conversation
/new [conversation_id]        # create/select conversation (auto: default-<n> if omitted)
/switch <conversation_id>     # switch to existing conversation
/rename <conversation_id>     # rename current conversation
/remove [conversation_id]     # remove current or named conversation (and its tasks)
/tasks ...                    # manage tasks from term (ls/rm/rm-all)
/help                         # list command hints
/quit or /exit                # exit terminal UI
```

Shortcuts:

- `Alt+1..9`: switch conversation by index
- `Ctrl+n` / `Ctrl+p`: next/previous conversation
- `Tab` / `Shift+Tab`: cycle conversations
- `Ctrl+g`: toggle debug dashboard
- `Enter`: send
- `Esc`: clear input
- `Ctrl+c`: quit

In debug dashboard mode, `Tab`/`Shift+Tab` and `Ctrl+n`/`Ctrl+p` cycle debug sections (Summary, Queue, Workers/Schedulers, Last Runs).

### `worker`

Starts worker mode.

```bash
build/goclaw worker [--worker-id <id>]
```

Behavior:

- Claims inbound queue messages.
- Builds prompt and calls configured model backend.
- Persists assistant response.
- Enqueues outbound queue message.
- `--worker-id` overrides worker identity for this process.

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
build/goclaw tasks <create|ls|ls-all|ls-conversation|rm|rm-all|status|update> [flags]
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

### `tasks ls-all`

```bash
build/goclaw tasks ls-all
```

Lists all tasks globally.

### `tasks ls-conversation`

```bash
build/goclaw tasks ls-conversation --conversation <conversation_id>
```

Lists tasks for one conversation.

### `tasks rm-all`

```bash
build/goclaw tasks rm-all --all
build/goclaw tasks rm-all --conversation <conversation_id>
```

Removes tasks in bulk:

- `--all`: remove all tasks globally
- `--conversation`: remove all tasks for a conversation

### `tasks status`

```bash
build/goclaw tasks status [--conversation <conversation_id>] [--limit 20]
```

Flags:

- `--conversation` optional: filter diagnostics to one conversation
- `--limit` optional: max number of task rows to show (default `20`)

Output includes:

- task counts (`due now`, `claimable`, `leased`)
- observed schedulers and workers
- queue state (pending/processing/done/error with oldest pending age)
- per-task snapshot with lease and latest run status/error

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
make run-term          # runs: build/goclaw run --mode=term,scheduler
make run-worker        # runs: build/goclaw run --mode=worker
make run-scheduler     # runs: build/goclaw run --mode=scheduler
make run CMD=single    # generic runner (build/goclaw run --mode=$(CMD))
make run CMD=worker,scheduler
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
export OPENAI_MODEL=gpt-5.2
# optional
export OPENAI_BASE_URL=https://api.openai.com
```

Worker startup logs include diagnostics (masked key hint, status, model visibility).

## Tool Calling (OpenAI)

Default policy file:

```text
./data/goclaw.toml
```

Quickstart policy template:

```bash
cp assets/goclaw.toml data/goclaw.toml
```

Example allowlist:

```toml
[ui]
user_label = "you"
assistant_label = "goclaw"

[[allow.tool]]
name = "echo"
description = "Print text to stdout."

[[allow.tool]]
name = "curl"
description = "Perform HTTP requests to external APIs."

[[allow.tool]]
name = "date"
description = "Print current local date and time."
```

`[[allow.tool]]` entries define both the allowlist and descriptions injected into the model system prompt to improve tool selection.
When allowlist is non-empty, worker exposes `exec_local_tool` and backend-enforces command allowlist.
`[ui]` labels are used by terminal chat rendering.

## Environment Variables

Core paths and runtime:

- `DATABASE_PATH` default `./data/goclaw.db`
- `LOG_PATH` default `./data/goclaw.log`
- `POLICY_PATH` default `./data/goclaw.toml`
- `MODE` default `single` (internal fallback)

Identity / routing:

- `WORKER_ID` default auto-generated (`worker-<pid>-<suffix>`)
- `SENDER_ID` default `sender-1`
- `SCHEDULER_ID` default `scheduler-1`
- `TERM_THEME` default `light` (`light` or `dark`)

For multi-worker setups, set a unique `WORKER_ID` per process (for example `worker-1`, `worker-2`), otherwise diagnostics will show them as one worker.

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
- `LOG_LEVEL` default `WARN`
- `UI_USER_LABEL` default `you`
- `UI_ASSISTANT_LABEL` default `goclaw`

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
