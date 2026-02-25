package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/klizhentas/goclaw/internal/app"
	"github.com/klizhentas/goclaw/internal/config"
	"github.com/klizhentas/goclaw/internal/store"
	"github.com/klizhentas/goclaw/internal/types"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	logger := mustBuildLogger(cfg)
	slog.SetDefault(logger)

	if len(os.Args) > 1 && os.Args[1] == "tasks" {
		if err := runTasksCommand(context.Background(), cfg, os.Args[2:]); err != nil {
			logger.Error("tasks command failed", "error", err)
			os.Exit(1)
		}
		return
	}

	modeFlag := flag.String("mode", "", "run mode: sender|worker|scheduler|single")
	flag.Parse()
	if *modeFlag != "" {
		cfg.Mode = *modeFlag
	}

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

func mustBuildLogger(cfg config.Config) *slog.Logger {
	if err := os.MkdirAll(filepath.Dir(cfg.LogPath), 0o755); err != nil {
		slog.Error("create log directory", "path", cfg.LogPath, "error", err)
		os.Exit(1)
	}

	logFile, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		slog.Error("open log file", "path", cfg.LogPath, "error", err)
		os.Exit(1)
	}

	return slog.New(slog.NewJSONHandler(logFile, &slog.HandlerOptions{Level: cfg.LogLevel})).With(
		"pid", os.Getpid(),
		"mode", cfg.Mode,
		"worker_id", cfg.WorkerID,
		"sender_id", cfg.SenderID,
		"scheduler_id", cfg.SchedulerID,
	)
}

func runTasksCommand(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: goclaw tasks <create|ls|rm|update>")
	}

	st, err := store.NewSQLiteStore(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer st.Close()

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "create":
		fs := flag.NewFlagSet("tasks create", flag.ContinueOnError)
		conversationID := fs.String("conversation", "", "conversation id")
		payload := fs.String("payload", "", "task payload text")
		runAtRaw := fs.String("at", "", "run at (RFC3339)")
		everyRaw := fs.String("every", "", "repeat interval (e.g. 5m)")
		if err := fs.Parse(subArgs); err != nil {
			return err
		}
		if strings.TrimSpace(*conversationID) == "" {
			return fmt.Errorf("--conversation is required")
		}
		if strings.TrimSpace(*payload) == "" {
			return fmt.Errorf("--payload is required")
		}

		var runAt *time.Time
		if *runAtRaw != "" {
			parsed, err := time.Parse(time.RFC3339, *runAtRaw)
			if err != nil {
				return fmt.Errorf("parse --at: %w", err)
			}
			runAt = &parsed
		}
		intervalSec := 0
		if *everyRaw != "" {
			d, err := time.ParseDuration(*everyRaw)
			if err != nil {
				return fmt.Errorf("parse --every: %w", err)
			}
			intervalSec = int(d.Seconds())
			if intervalSec < 1 {
				return fmt.Errorf("--every must be >= 1s")
			}
		}

		task, err := st.CreateTask(ctx, *conversationID, *payload, runAt, intervalSec)
		if err != nil {
			return err
		}
		fmt.Printf("created task id=%s conversation_id=%s schedule_type=%s\n", task.ID, task.ConversationID, task.ScheduleType)
		return nil

	case "ls":
		fs := flag.NewFlagSet("tasks ls", flag.ContinueOnError)
		conversationID := fs.String("conversation", "", "optional conversation id filter")
		if err := fs.Parse(subArgs); err != nil {
			return err
		}
		tasks, err := st.ListTasks(ctx, *conversationID)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			fmt.Printf("id=%s conversation_id=%s status=%s next_run_at=%s schedule=%s interval_sec=%d failures=%d\n",
				t.ID, t.ConversationID, t.Status, t.NextRunAt.Format(time.RFC3339), t.ScheduleType, t.IntervalSec, t.FailureCount)
		}
		return nil

	case "rm":
		fs := flag.NewFlagSet("tasks rm", flag.ContinueOnError)
		taskID := fs.String("id", "", "task id")
		if err := fs.Parse(subArgs); err != nil {
			return err
		}
		if strings.TrimSpace(*taskID) == "" {
			return fmt.Errorf("--id is required")
		}
		if err := st.RemoveTask(ctx, *taskID); err != nil {
			return err
		}
		fmt.Printf("removed task id=%s\n", *taskID)
		return nil

	case "update":
		fs := flag.NewFlagSet("tasks update", flag.ContinueOnError)
		runID := fs.String("run-id", "", "task run id")
		callerID := fs.String("caller-id", "", "caller/agent id")
		statusRaw := fs.String("status", "", "success|failed")
		result := fs.String("result", "", "result content")
		errorMsg := fs.String("error", "", "error content")
		assistantMessageID := fs.String("assistant-message-id", "", "assistant message id")
		if err := fs.Parse(subArgs); err != nil {
			return err
		}
		if strings.TrimSpace(*runID) == "" {
			return fmt.Errorf("--run-id is required")
		}
		if strings.TrimSpace(*callerID) == "" {
			return fmt.Errorf("--caller-id is required")
		}

		var status types.TaskRunStatus
		switch strings.ToLower(strings.TrimSpace(*statusRaw)) {
		case "success":
			status = types.TaskRunStatusSuccess
		case "failed", "failure", "error":
			status = types.TaskRunStatusFailed
		default:
			return fmt.Errorf("--status must be success|failed")
		}

		if err := st.UpdateTaskRunAuthorized(ctx, *runID, *callerID, status, *result, *errorMsg, *assistantMessageID); err != nil {
			return err
		}
		fmt.Printf("updated run id=%s status=%s\n", *runID, status)
		return nil

	default:
		return fmt.Errorf("unknown tasks subcommand %q", sub)
	}
}
