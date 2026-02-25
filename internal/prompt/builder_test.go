package prompt

import (
	"strings"
	"testing"
	"time"

	"github.com/klizhentas/goclaw/internal/types"
)

func TestBuildMessages_Window(t *testing.T) {
	history := []types.Message{
		{Role: types.MessageRoleUser, Content: "1", CreatedAt: time.Now()},
		{Role: types.MessageRoleAssistant, Content: "2", CreatedAt: time.Now()},
		{Role: types.MessageRoleUser, Content: "3", CreatedAt: time.Now()},
	}

	messages := BuildMessages("sys", history, 2, 100)
	if len(messages) != 3 {
		t.Fatalf("expected system + 2 history messages, got %d", len(messages))
	}
	if messages[1].Content != "2" || messages[2].Content != "3" {
		t.Fatalf("unexpected window content: %#v", messages)
	}
}

func TestBuildMessages_Truncate(t *testing.T) {
	history := []types.Message{{Role: types.MessageRoleUser, Content: strings.Repeat("a", 10), CreatedAt: time.Now()}}
	messages := BuildMessages("sys", history, 1, 5)
	if messages[1].Content != "aa..." {
		t.Fatalf("expected truncation, got %q", messages[1].Content)
	}
}

func TestBuildSystemPrompt_IncludesConversationContext(t *testing.T) {
	p := BuildSystemPrompt(types.ConversationRoleMain, "Andy", "main")
	if !strings.Contains(p, `conversation_id="main"`) {
		t.Fatalf("expected conversation context in system prompt, got %q", p)
	}
	if !strings.Contains(p, "default context for task operations") {
		t.Fatalf("expected task context guidance in system prompt, got %q", p)
	}
}
