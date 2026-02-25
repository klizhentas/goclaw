package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ReadsAllowedToolsFromPolicyFile(t *testing.T) {
	t.Setenv("POLICY_PATH", filepath.Join(t.TempDir(), "goclaw.toml"))
	policyPath := os.Getenv("POLICY_PATH")
	content := `
[[allow.tool]]
name = "echo"
description = "Echo back provided text."

[[allow.tool]]
name = "date"
description = "Print current local date and time."
`
	if err := os.WriteFile(policyPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write policy file: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.AllowedTools) != 2 {
		t.Fatalf("expected 2 allowed tools, got %d", len(cfg.AllowedTools))
	}
}

func TestLoad_ReadsToolDescriptionsFromPolicyFile(t *testing.T) {
	t.Setenv("POLICY_PATH", filepath.Join(t.TempDir(), "goclaw.toml"))
	policyPath := os.Getenv("POLICY_PATH")
	content := `
[[allow.tool]]
name = "date"
description = "Print current local date and time."

[[allow.tool]]
name = "curl"
description = "Fetch HTTP resources from a URL."
`
	if err := os.WriteFile(policyPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write policy file: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if len(cfg.AllowedTools) != 2 {
		t.Fatalf("expected 2 allowed tools, got %d", len(cfg.AllowedTools))
	}
	if cfg.ToolDescriptions["date"] != "Print current local date and time." {
		t.Fatalf("unexpected date description: %q", cfg.ToolDescriptions["date"])
	}
	if cfg.ToolDescriptions["curl"] != "Fetch HTTP resources from a URL." {
		t.Fatalf("unexpected curl description: %q", cfg.ToolDescriptions["curl"])
	}
}

func TestLoad_ReadsUILabelsFromPolicyFile(t *testing.T) {
	t.Setenv("POLICY_PATH", filepath.Join(t.TempDir(), "goclaw.toml"))
	policyPath := os.Getenv("POLICY_PATH")
	content := `
[ui]
user_label = "operator"
assistant_label = "clawbot"
`
	if err := os.WriteFile(policyPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write policy file: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.UIUserLabel != "operator" {
		t.Fatalf("unexpected UIUserLabel: %q", cfg.UIUserLabel)
	}
	if cfg.UIAssistantLabel != "clawbot" {
		t.Fatalf("unexpected UIAssistantLabel: %q", cfg.UIAssistantLabel)
	}
}
