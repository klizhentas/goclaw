# RFD 0003 - Terminal UI Upgrade with tview (In-Place `term`)

- Status: proposed
- Authors: goclaw maintainers
- Created: 2026-02-25
- Updated: 2026-02-25
- Tracking: `cmd/goclaw`, `internal/listener`, `internal/outbound`, `internal/app`
- Related:
  - `rfds/0000-core-v1.md` (core runtime + queue semantics)
  - `rfds/0001-task-scheduler.md` (task execution feedback in conversations)
  - `rfds/0002-repo-guidelines.md` (CLI UX conventions)

## Summary
Upgrade the existing `term` experience to a richer terminal UI using `github.com/rivo/tview`, without introducing a new CLI mode.

The upgraded `term` must provide:
1. Vertical conversation tabs on the left.
2. Conversation window on the right (message stream + input).
3. Fast conversation switching via number shortcuts and common terminal conventions.
4. In-place updates to conversation content as messages arrive.

## Goals
1. Keep the CLI surface simple: keep using `term`; do not add `term-ui`.
2. Improve operator productivity for multi-conversation workflows.
3. Preserve current queue-driven behavior and persistence model.
4. Use shortcuts that avoid common tmux conflicts.

## Non-Goals
1. No web UI.
2. No protocol changes for queue/schema.
3. No scheduler/worker architecture changes.
4. No multi-process IPC changes beyond existing queue model.

## UX Requirements
1. Layout:
   - Left pane: conversation list/tabs (vertical).
   - Right pane top: message history for active conversation.
   - Right pane bottom: input field.
2. Switching:
   - `Alt+1..9` selects conversation by index.
   - `Ctrl+n` / `Ctrl+p` cycles next/previous conversation.
   - `Tab` / `Shift+Tab` cycles conversations.
3. Input:
   - `Enter` sends message to active conversation.
   - `Esc` clears current input.
   - `Ctrl+c` exits.
4. Updates:
   - Active conversation appends inbound/outbound content live.
   - Inactive conversations show unread indicator.
   - Switching conversation redraws right pane with selected history.

## tmux/Shortcut Policy
Chosen shortcuts intentionally avoid popular tmux prefix conflicts:
1. Avoid `Ctrl+b` and `Ctrl+a` patterns.
2. Prefer `Alt+number` for direct selection.
3. Keep navigation conventions consistent with common terminal apps.

## Functional Behavior
1. `term` mode remains queue-backed:
   - enqueue inbound user messages,
   - poll outbound queue and display responses.
2. Conversation identity remains backend source of truth.
3. Existing message persistence and ordering semantics remain unchanged.
4. Existing trigger/role policy enforcement remains backend-side.

## Rollout Plan
Phase A:
1. Introduce tview app skeleton and pane layout in `term`.
2. Render conversations and active conversation message view.

Phase B:
1. Wire inbound send + outbound polling in TUI loop.
2. Support conversation switching and live redraw.

Phase C:
1. Add unread counters and status line.
2. Improve focus behavior and key handling polish.

Phase D:
1. Documentation updates.
2. Tests for key routing, conversation selection, and message routing.

## Acceptance Criteria
1. `goclaw term` opens tview interface with left tabs and right conversation window.
2. User can switch conversations by number and cycling keys.
3. Outbound responses update active conversation in place.
4. Inactive conversations accumulate unread indicators.
5. No keybindings conflict with common tmux defaults.
6. Existing `run --mode=term,scheduler` flow continues to work.

## Risks and Mitigations
1. Terminal compatibility variance:
   - Mitigation: keep keymap minimal and test on common terminals.
2. Event loop complexity:
   - Mitigation: keep UI state small and route DB polling via clear channels.
3. Regressions in existing term flow:
   - Mitigation: phase rollout and keep backend queue contracts unchanged.
