package main

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/klizhentas/goclaw/internal/config"
)

func TestNewParserBuildsWithOptionalLogLevel(t *testing.T) {
	var cli CLI
	if _, err := newParser(&cli); err != nil {
		t.Fatalf("newParser failed: %v", err)
	}
}

func TestApplyCLIOverridesDebugShortcut(t *testing.T) {
	cfg := config.Config{LogLevel: slog.LevelWarn, TermTheme: "light"}
	cli := CLI{Debug: true}
	if err := applyCLIOverrides(&cfg, &cli); err != nil {
		t.Fatalf("applyCLIOverrides returned error: %v", err)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("expected debug log level, got %v", cfg.LogLevel)
	}
}

func TestApplyCLIOverridesInvalidLogLevel(t *testing.T) {
	cfg := config.Config{LogLevel: slog.LevelWarn, TermTheme: "light"}
	cli := CLI{LogLevel: "verbose"}
	if err := applyCLIOverrides(&cfg, &cli); err == nil {
		t.Fatal("expected error for invalid log level")
	}
}

func TestApplyCLIOverridesInvalidTheme(t *testing.T) {
	cfg := config.Config{LogLevel: slog.LevelWarn, TermTheme: "light"}
	cli := CLI{}
	cli.Run.Theme = "sepia"
	if err := applyCLIOverrides(&cfg, &cli); err == nil {
		t.Fatal("expected error for invalid theme")
	}
}

func TestConciseTopLevelHelp(t *testing.T) {
	help := conciseTopLevelHelp()
	required := []string{
		"goclaw requires a subcommand.",
		"Usage:",
		"goclaw <command> [flags]",
		"run",
		"term",
		"tasks",
		`Use "goclaw --help" for full reference.`,
	}
	for _, want := range required {
		if !strings.Contains(help, want) {
			t.Fatalf("conciseTopLevelHelp() missing %q", want)
		}
	}
}
