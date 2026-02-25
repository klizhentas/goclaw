package termui

import "testing"

func TestClampDebugTab(t *testing.T) {
	if got := clampDebugTab(-1); got != debugTabSummary {
		t.Fatalf("expected summary for -1, got %d", got)
	}
	if got := clampDebugTab(debugTabRuns); got != debugTabRuns {
		t.Fatalf("expected runs tab, got %d", got)
	}
	if got := clampDebugTab(999); got != debugTabSummary {
		t.Fatalf("expected summary for out of range, got %d", got)
	}
}

func TestCycleDebugTabLocked(t *testing.T) {
	u := &UI{debugTabIndex: debugTabSummary}
	u.cycleDebugTabLocked(1)
	if u.debugTabIndex != debugTabQueue {
		t.Fatalf("expected queue tab, got %d", u.debugTabIndex)
	}
	u.cycleDebugTabLocked(-1)
	if u.debugTabIndex != debugTabSummary {
		t.Fatalf("expected summary tab, got %d", u.debugTabIndex)
	}
	u.cycleDebugTabLocked(-1)
	if u.debugTabIndex != debugTabRuns {
		t.Fatalf("expected wrap to runs tab, got %d", u.debugTabIndex)
	}
}

func TestParseTasksAction(t *testing.T) {
	action, usage := parseTasksAction("ls main")
	if usage != "" || action.Kind != CommandTasksList || action.ConversationID != "main" {
		t.Fatalf("unexpected ls parse: action=%#v usage=%q", action, usage)
	}

	action, usage = parseTasksAction("rm --id abc123")
	if usage != "" || action.Kind != CommandTasksRemove || action.TaskID != "abc123" {
		t.Fatalf("unexpected rm parse: action=%#v usage=%q", action, usage)
	}

	action, usage = parseTasksAction("rm-all --all")
	if usage != "" || action.Kind != CommandTasksRemoveAll || !action.All {
		t.Fatalf("unexpected rm-all parse: action=%#v usage=%q", action, usage)
	}
}
