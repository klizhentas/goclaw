package termui

import "testing"

func TestParseInputLine(t *testing.T) {
	conversationID, content, ok := parseInputLine("main", "other: hello")
	if !ok || conversationID != "other" || content != "hello" {
		t.Fatalf("unexpected parse result: ok=%v conversation=%q content=%q", ok, conversationID, content)
	}

	conversationID, content, ok = parseInputLine("main", "hello world")
	if !ok || conversationID != "main" || content != "hello world" {
		t.Fatalf("unexpected parse result without explicit conversation: ok=%v conversation=%q content=%q", ok, conversationID, content)
	}
}

func TestConversationStateUnreadAndSwitch(t *testing.T) {
	s := newConversationState("main")
	s.appendAssistantMessage("room-1", "a")
	if s.unread["room-1"] != 1 {
		t.Fatalf("expected unread=1 for room-1, got %d", s.unread["room-1"])
	}

	roomIdx := s.ensureConversation("room-1")
	if !s.switchToIndex(roomIdx) {
		t.Fatal("expected switch to room-1 to succeed")
	}
	if s.unread["room-1"] != 0 {
		t.Fatalf("expected unread reset after switching, got %d", s.unread["room-1"])
	}
}

func TestIndexFromRune(t *testing.T) {
	idx, ok := indexFromRune('1')
	if !ok || idx != 0 {
		t.Fatalf("expected rune '1' => index 0, got idx=%d ok=%v", idx, ok)
	}
	if _, ok := indexFromRune('0'); ok {
		t.Fatal("expected rune '0' to be invalid")
	}
}
