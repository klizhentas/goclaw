package model

import (
	"strings"
	"testing"
)

func TestToolUsageSystemPrompt_IncludesAllowedTools(t *testing.T) {
	prompt := toolUsageSystemPrompt([]string{"curl", "date"})
	if !strings.Contains(prompt, "exec_local_tool") {
		t.Fatalf("expected exec_local_tool in prompt: %q", prompt)
	}
	if !strings.Contains(prompt, "curl") || !strings.Contains(prompt, "date") {
		t.Fatalf("expected allowed tools listed in prompt: %q", prompt)
	}
}
