package model

import (
	"strings"
	"testing"
)

func TestToolUsageSystemPrompt_IncludesAllowedTools(t *testing.T) {
	prompt := toolUsageSystemPrompt([]string{"curl", "date"}, map[string]string{
		"curl": "Fetch HTTP resources from APIs.",
		"date": "Print current local date and time.",
	})
	if !strings.Contains(prompt, "exec_local_tool") {
		t.Fatalf("expected exec_local_tool in prompt: %q", prompt)
	}
	if !strings.Contains(prompt, "curl") || !strings.Contains(prompt, "date") {
		t.Fatalf("expected allowed tools listed in prompt: %q", prompt)
	}
	if !strings.Contains(prompt, "Fetch HTTP resources from APIs.") {
		t.Fatalf("expected tool descriptions listed in prompt: %q", prompt)
	}
}
