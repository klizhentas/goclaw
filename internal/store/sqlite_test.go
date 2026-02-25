package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/klizhentas/goclaw/internal/types"
)

func TestSQLiteStore_AppendAndRecentMessages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	st, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer st.Close()

	if err := st.CreateConversationIfMissing(ctx, "conv1", types.ConversationRoleNonMain); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if _, err := st.AppendMessage(ctx, "conv1", types.MessageRoleUser, "one"); err != nil {
		t.Fatalf("append one: %v", err)
	}
	if _, err := st.AppendMessage(ctx, "conv1", types.MessageRoleAssistant, "two"); err != nil {
		t.Fatalf("append two: %v", err)
	}
	if _, err := st.AppendMessage(ctx, "conv1", types.MessageRoleUser, "three"); err != nil {
		t.Fatalf("append three: %v", err)
	}

	recent, err := st.GetRecentMessages(ctx, "conv1", 2)
	if err != nil {
		t.Fatalf("get recent: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent messages, got %d", len(recent))
	}
	if recent[0].Content != "two" || recent[1].Content != "three" {
		t.Fatalf("unexpected ordering/content: %#v", recent)
	}
}

func TestSQLiteStore_PersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "persist.db")

	st, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := st.CreateConversationIfMissing(ctx, "conv1", types.ConversationRoleMain); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if _, err := st.AppendMessage(ctx, "conv1", types.MessageRoleUser, "persist me"); err != nil {
		t.Fatalf("append message: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	recent, err := reopened.GetRecentMessages(ctx, "conv1", 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(recent) != 1 || recent[0].Content != "persist me" {
		t.Fatalf("expected persisted message, got %#v", recent)
	}
}

func TestSQLiteStore_RemoveConversationRemovesMessagesAndTasks(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "remove_conversation.db")

	st, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer st.Close()

	if err := st.CreateConversationIfMissing(ctx, "conv1", types.ConversationRoleMain); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if _, err := st.AppendMessage(ctx, "conv1", types.MessageRoleUser, "hello"); err != nil {
		t.Fatalf("append message: %v", err)
	}
	if _, err := st.CreateTask(ctx, "conv1", "task", nil, 0); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := st.RemoveConversation(ctx, "conv1"); err != nil {
		t.Fatalf("remove conversation: %v", err)
	}

	msgs, err := st.GetRecentMessages(ctx, "conv1", 10)
	if err != nil {
		t.Fatalf("get recent messages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no messages after conversation removal, got %d", len(msgs))
	}

	tasks, err := st.ListTasks(ctx, "conv1")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks after conversation removal, got %d", len(tasks))
	}
}
