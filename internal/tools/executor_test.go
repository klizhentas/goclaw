package tools

import (
	"context"
	"strings"
	"testing"
)

func TestExecutor_AllowlistedCommand(t *testing.T) {
	e := NewExecutor([]string{"echo"})
	result, err := e.Execute(context.Background(), "echo", []string{"hello"})
	if err != nil {
		t.Fatalf("execute echo: %v", err)
	}
	if result.ReturnCode != 0 {
		t.Fatalf("expected return code 0, got %d", result.ReturnCode)
	}
	if result.Stdout == "" {
		t.Fatal("expected stdout from echo")
	}
}

func TestExecutor_DeniedCommand(t *testing.T) {
	e := NewExecutor([]string{"echo"})
	_, err := e.Execute(context.Background(), "date", nil)
	if err == nil {
		t.Fatal("expected denied command error")
	}
}

func TestExecutor_CapturesStderrOnFailure(t *testing.T) {
	e := NewExecutor([]string{"sh"})
	result, err := e.Execute(context.Background(), "sh", []string{"-c", "echo boom 1>&2; exit 3"})
	if err != nil {
		t.Fatalf("execute failing sh command: %v", err)
	}
	if result.ReturnCode != 3 {
		t.Fatalf("expected return code 3, got %d", result.ReturnCode)
	}
	if !strings.Contains(result.Stderr, "boom") {
		t.Fatalf("expected stderr to contain boom, got %q", result.Stderr)
	}
}
