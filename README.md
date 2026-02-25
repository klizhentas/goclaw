# goclaw

Minimal Go implementation scaffold for GOCLAW v1.

## Modes

`miniclaw` supports three run modes:

- `sender`: reads terminal input and enqueues inbound messages into SQLite queue.
- `worker`: polls SQLite queue, runs model, persists assistant replies, and enqueues outbound queue records.
- `scheduler`: claims due tasks using SQLite CAS + lease, creates task runs, and routes runs to worker via inbound queue.
- `single`: local end-to-end in one process (enqueue + process immediately).

Binary is built into `build/miniclaw` by `make build` and run targets.

## Local End-to-End (Two Terminals)

Terminal 1:

```bash
make run-worker
```

Terminal 2:

```bash
make run-sender
```

Input format:

```text
<conversation_id>: <message>
```

Example:

```text
main: hello
room-1: @Andy summarize this
```

Type `exit` to quit sender.

## Real OpenAI Worker

Set environment variables before running worker:

```bash
export MODEL_BACKEND=openai
export OPENAI_API_KEY=...your key...
export OPENAI_MODEL=gpt-4o-mini
# optional: export OPENAI_BASE_URL=https://api.openai.com
make run-worker
```

Default model backend is `echo` for local simulation.
When `MODEL_BACKEND=openai`, worker startup logs a diagnostics event with masked key hint, HTTP status, model visibility, and API errors (including quota/auth failures).

### Local Tool Calling (Allowlist via TOML)

Create `goclaw.toml`:

```toml
[allow]
tools = ["echo", "date"]
```

Set `POLICY_PATH` if your file is elsewhere.  
Default policy path is `./data/goclaw.toml`.
When tools are allowlisted, worker exposes one tool callback to OpenAI: `exec_local_tool`.
Only commands in `[allow.tools]` are executed. Tool result sent back to the model contains:

- `stdout`
- `return_code`

## Logging

Structured logs are written to a shared file (`LOG_PATH`, default `./data/goclaw.log`) instead of stdout.
Each log record includes:

- `pid`
- `mode`
- `worker_id`
- `sender_id`

Defaults:

- `DATABASE_PATH=./data/goclaw.db`
- `LOG_PATH=./data/goclaw.log`

## Tasks CLI

```bash
build/miniclaw tasks create --conversation main --payload "echo hello"
build/miniclaw tasks create --conversation main --payload "daily check" --every 10m
build/miniclaw tasks ls
build/miniclaw tasks rm --id <task_id>
build/miniclaw tasks update --run-id <run_id> --caller-id scheduler-1 --status success --result \"done\"
```

## Test and Format

```bash
make fmt
make vet
make test
make test-race
```
