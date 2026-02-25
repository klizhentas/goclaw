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
