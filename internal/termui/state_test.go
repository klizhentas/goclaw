package termui

import "testing"

func TestParseCommand(t *testing.T) {
	cmd := parseCommand("hello world")
	if cmd.kind != commandNone {
		t.Fatalf("expected commandNone, got %v", cmd.kind)
	}

	cmd = parseCommand("/new room-1")
	if cmd.kind != commandNew || cmd.arg != "room-1" {
		t.Fatalf("unexpected /new parse: %#v", cmd)
	}
	cmd = parseCommand("/new")
	if cmd.kind != commandNew || cmd.arg != "" {
		t.Fatalf("unexpected /new parse without arg: %#v", cmd)
	}

	cmd = parseCommand("/switch room-2")
	if cmd.kind != commandSwitch || cmd.arg != "room-2" {
		t.Fatalf("unexpected /switch parse: %#v", cmd)
	}
	cmd = parseCommand("/rename room-3")
	if cmd.kind != commandRename || cmd.arg != "room-3" {
		t.Fatalf("unexpected /rename parse: %#v", cmd)
	}

	cmd = parseCommand("/help")
	if cmd.kind != commandHelp {
		t.Fatalf("expected /help parse, got %#v", cmd)
	}

	cmd = parseCommand("/quit")
	if cmd.kind != commandQuit {
		t.Fatalf("expected /quit parse, got %#v", cmd)
	}
	cmd = parseCommand("/exit")
	if cmd.kind != commandQuit {
		t.Fatalf("expected /exit parse, got %#v", cmd)
	}

	cmd = parseCommand("/new room-1 extra")
	if cmd.kind != commandInvalid {
		t.Fatalf("expected commandInvalid for /new with too many args, got %#v", cmd)
	}
	cmd = parseCommand("/rename")
	if cmd.kind != commandInvalid {
		t.Fatalf("expected commandInvalid for /rename without args, got %#v", cmd)
	}
}

func TestConversationStateUnreadAndSwitch(t *testing.T) {
	s := newConversationState("main", "you", "goclaw")
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

func TestConversationStateNextDefaultConversationID(t *testing.T) {
	s := newConversationState("main", "you", "goclaw")
	if got := s.nextDefaultConversationID(); got != "default-1" {
		t.Fatalf("expected default-1, got %q", got)
	}
	s.ensureConversation("default-1")
	if got := s.nextDefaultConversationID(); got != "default-2" {
		t.Fatalf("expected default-2, got %q", got)
	}
}

func TestAppendAssistantMessageUsesGoclawLabel(t *testing.T) {
	s := newConversationState("main", "you", "goclaw")
	s.appendAssistantMessage("main", "hello")
	msgs := s.messages["main"]
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0] != "goclaw: hello" {
		t.Fatalf("unexpected message label: %q", msgs[0])
	}
}

func TestAppendMessagesUseConfiguredLabels(t *testing.T) {
	s := newConversationState("main", "human", "bot")
	s.appendUserMessage("main", "hi")
	s.appendAssistantMessage("main", "hello")
	msgs := s.messages["main"]
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0] != "human: hi" {
		t.Fatalf("unexpected user label: %q", msgs[0])
	}
	if msgs[1] != "bot: hello" {
		t.Fatalf("unexpected assistant label: %q", msgs[1])
	}
}

func TestRenameActiveConversation(t *testing.T) {
	s := newConversationState("main", "you", "goclaw")
	s.appendUserMessage("main", "hello")
	s.unread["main"] = 2
	oldID, err := s.renameActiveConversation("ops")
	if err != nil {
		t.Fatalf("rename failed: %v", err)
	}
	if oldID != "main" {
		t.Fatalf("unexpected old ID: %q", oldID)
	}
	if s.activeConversation() != "ops" {
		t.Fatalf("expected active conversation ops, got %q", s.activeConversation())
	}
	if _, ok := s.messages["main"]; ok {
		t.Fatal("expected old message key to be removed")
	}
	if len(s.messages["ops"]) != 1 {
		t.Fatalf("expected messages to move to new ID, got %d", len(s.messages["ops"]))
	}
	if s.unread["ops"] != 2 {
		t.Fatalf("expected unread to move to new ID, got %d", s.unread["ops"])
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
