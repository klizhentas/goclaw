package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/klizhentas/goclaw/internal/app"
	"github.com/klizhentas/goclaw/internal/config"
)

func main() {
	modeFlag := flag.String("mode", "", "run mode: sender|worker|single")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	if *modeFlag != "" {
		cfg.Mode = *modeFlag
	}

	if err := os.MkdirAll(filepath.Dir(cfg.LogPath), 0o755); err != nil {
		slog.Error("create log directory", "path", cfg.LogPath, "error", err)
		os.Exit(1)
	}

	logFile, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		slog.Error("open log file", "path", cfg.LogPath, "error", err)
		os.Exit(1)
	}
	defer logFile.Close()

	logger := slog.New(slog.NewJSONHandler(logFile, &slog.HandlerOptions{Level: cfg.LogLevel})).With(
		"pid", os.Getpid(),
		"mode", cfg.Mode,
		"worker_id", cfg.WorkerID,
		"sender_id", cfg.SenderID,
	)
	slog.SetDefault(logger)
	application, err := app.New(cfg, logger)
	if err != nil {
		logger.Error("create app", "error", err)
		os.Exit(1)
	}
	defer application.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := application.Run(ctx, cfg.Mode); err != nil {
		logger.Error("run app", "mode", cfg.Mode, "error", err)
		os.Exit(1)
	}
}
