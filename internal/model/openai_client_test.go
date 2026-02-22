package model

import "testing"

func TestNormalizeExecArgs_CommandField(t *testing.T) {
	got, err := normalizeExecArgs(`{"command":"date","args":[]}`, execLocalToolArgs{})
	if err != nil {
		t.Fatalf("normalize args: %v", err)
	}
	if got.Tool != "date" {
		t.Fatalf("expected tool date, got %q", got.Tool)
	}
}

func TestNormalizeExecArgs_CommandString(t *testing.T) {
	got, err := normalizeExecArgs(`{"command":"curl https://example.com"}`, execLocalToolArgs{})
	if err != nil {
		t.Fatalf("normalize args: %v", err)
	}
	if got.Tool != "curl" {
		t.Fatalf("expected tool curl, got %q", got.Tool)
	}
	if len(got.Args) != 1 || got.Args[0] != "https://example.com" {
		t.Fatalf("unexpected args: %#v", got.Args)
	}
}

func TestNormalizeExecArgs_MissingTool(t *testing.T) {
	_, err := normalizeExecArgs(`{"args":[]}`, execLocalToolArgs{})
	if err == nil {
		t.Fatal("expected missing tool error")
	}
}
