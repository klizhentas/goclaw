package tools

import (
	"context"
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
