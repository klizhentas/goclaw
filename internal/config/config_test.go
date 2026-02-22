package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ReadsAllowedToolsFromPolicyFile(t *testing.T) {
	t.Setenv("POLICY_PATH", filepath.Join(t.TempDir(), "goclaw.toml"))
	policyPath := os.Getenv("POLICY_PATH")
	if err := os.WriteFile(policyPath, []byte("[allow]\ntools = [\"echo\", \"date\"]\n"), 0o644); err != nil {
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
