package policy

import (
	"strings"

	"github.com/klizhentas/goclaw/internal/config"
	"github.com/klizhentas/goclaw/internal/types"
)

func ShouldProcess(cfg config.Config, conversationID string, content string) bool {
	isMain := types.IsMainConversation(conversationID, cfg.MainConversationID)
	requiresTrigger := cfg.NonMainNeedsTrigger
	if isMain {
		requiresTrigger = cfg.MainNeedsTrigger
	}
	if !requiresTrigger {
		return true
	}
	return HasTrigger(cfg.AssistantName, content)
}

func HasTrigger(assistantName, content string) bool {
	trimmed := strings.TrimSpace(content)
	trigger := "@" + strings.ToLower(strings.TrimSpace(assistantName))
	return strings.HasPrefix(strings.ToLower(trimmed), trigger)
}
