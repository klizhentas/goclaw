package policy

import (
	"testing"

	"github.com/klizhentas/goclaw/internal/config"
)

func TestShouldProcess_NonMainNeedsTrigger(t *testing.T) {
	cfg := config.Config{AssistantName: "Andy", MainConversationID: "main", NonMainNeedsTrigger: true, MainNeedsTrigger: false}

	if ShouldProcess(cfg, "room1", "hello") {
		t.Fatalf("expected non-main message without trigger to be skipped")
	}
	if !ShouldProcess(cfg, "room1", "@Andy hello") {
		t.Fatalf("expected non-main message with trigger to be processed")
	}
}

func TestShouldProcess_MainNoTrigger(t *testing.T) {
	cfg := config.Config{AssistantName: "Andy", MainConversationID: "main", NonMainNeedsTrigger: true, MainNeedsTrigger: false}

	if !ShouldProcess(cfg, "main", "hello") {
		t.Fatalf("expected main message without trigger to be processed")
	}
}
