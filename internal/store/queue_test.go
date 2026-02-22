package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/klizhentas/goclaw/internal/types"
)

func TestQueue_EnqueueClaimDone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "queue.db")

	st, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer st.Close()

	queued, err := st.EnqueueInbound(ctx, "c1", "@Andy hello")
	if err != nil {
		t.Fatalf("enqueue inbound: %v", err)
	}
	if queued.Status != types.QueueStatusPending {
		t.Fatalf("unexpected initial status: %s", queued.Status)
	}

	claimed, err := st.ClaimNextInbound(ctx, "worker-a")
	if err != nil {
		t.Fatalf("claim inbound: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected claimed message")
	}
	if claimed.ID != queued.ID {
		t.Fatalf("claimed wrong id: got %s want %s", claimed.ID, queued.ID)
	}
	if claimed.Status != types.QueueStatusProcessing {
		t.Fatalf("unexpected claimed status: %s", claimed.Status)
	}

	next, err := st.ClaimNextInbound(ctx, "worker-b")
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if next != nil {
		t.Fatal("expected no second pending message")
	}

	if err := st.MarkQueueDone(ctx, queued.ID); err != nil {
		t.Fatalf("mark done: %v", err)
	}
}

func TestQueue_MarkErrorAndOutbound(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "queue_out.db")

	st, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer st.Close()

	queued, err := st.EnqueueInbound(ctx, "c2", "@Andy fail")
	if err != nil {
		t.Fatalf("enqueue inbound: %v", err)
	}
	if err := st.MarkQueueError(ctx, queued.ID, "boom"); err != nil {
		t.Fatalf("mark error: %v", err)
	}

	out, err := st.EnqueueOutbound(ctx, "c2", "response", "msg-1")
	if err != nil {
		t.Fatalf("enqueue outbound: %v", err)
	}
	if out.Direction != types.QueueDirectionOutbound {
		t.Fatalf("unexpected outbound direction: %s", out.Direction)
	}
	if out.Status != types.QueueStatusPending {
		t.Fatalf("unexpected outbound status: %s", out.Status)
	}
	if out.RelatedMessageID != "msg-1" {
		t.Fatalf("unexpected related message id: %s", out.RelatedMessageID)
	}

	claimedOut, err := st.ClaimNextOutbound(ctx, "sender-a")
	if err != nil {
		t.Fatalf("claim outbound: %v", err)
	}
	if claimedOut == nil || claimedOut.ID != out.ID {
		t.Fatalf("expected claimed outbound id %s, got %#v", out.ID, claimedOut)
	}
}
