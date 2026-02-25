package prompt

import (
	"fmt"
	"strings"

	"github.com/klizhentas/goclaw/internal/types"
)

type PromptMessage struct {
	Role    string
	Content string
}

func BuildSystemPrompt(conversationRole types.ConversationRole, assistantName, conversationID string) string {
	return fmt.Sprintf(
		"You are %s. Conversation role=%s. Execution context: conversation_id=%q. "+
			"Always review and use this conversation_id as the default context for task operations unless the user explicitly asks for another conversation. "+
			"Respect backend-enforced authorization and trigger policy.",
		assistantName,
		conversationRole,
		conversationID,
	)
}

func BuildMessages(systemPrompt string, history []types.Message, window int, maxContentLen int) []PromptMessage {
	if window < 1 {
		window = 1
	}
	if maxContentLen < 1 {
		maxContentLen = 1
	}

	start := 0
	if len(history) > window {
		start = len(history) - window
	}
	trimmed := history[start:]

	result := make([]PromptMessage, 0, len(trimmed)+1)
	result = append(result, PromptMessage{Role: "system", Content: systemPrompt})
	for _, msg := range trimmed {
		result = append(result, PromptMessage{
			Role:    string(msg.Role),
			Content: truncate(msg.Content, maxContentLen),
		})
	}
	return result
}

func truncate(text string, max int) string {
	if len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	return strings.TrimSpace(text[:max-3]) + "..."
}
