package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"github.com/gravitational/trace"
	"github.com/klizhentas/goclaw/internal/app"
	"github.com/klizhentas/goclaw/internal/config"
	"github.com/klizhentas/goclaw/internal/store"
	"github.com/klizhentas/goclaw/internal/types"
)

type CLI struct {
	Term      TermCmd      `cmd:"" help:"Start terminal mode (enqueue user input, print outbound responses)." aliases:"terminal"`
	Worker    WorkerCmd    `cmd:"" help:"Start worker mode (claim and process inbound queue messages)."`
	Scheduler SchedulerCmd `cmd:"" help:"Start scheduler mode (claim due tasks and enqueue task runs)."`
	Single    SingleCmd    `cmd:"" help:"Run single-process interactive mode."`
	Tasks     TasksCmd     `cmd:"" help:"Manage task definitions and task runs."`
}

type TermCmd struct{}
type WorkerCmd struct{}
type SchedulerCmd struct{}
type SingleCmd struct{}

type TasksCmd struct {
	Create TasksCreateCmd `cmd:"" help:"Create one-shot or recurring task."`
	Ls     TasksLsCmd     `cmd:"" help:"List tasks." aliases:"list"`
	Rm     TasksRmCmd     `cmd:"" help:"Soft-delete a task." aliases:"remove,delete"`
	Update TasksUpdateCmd `cmd:"" help:"Update a task run status (authorized backend path)."`
}

type TasksCreateCmd struct {
	Conversation string     `name:"conversation" required:"" help:"Conversation ID to bind task to."`
	Payload      string     `name:"payload" required:"" help:"Task payload text routed to worker."`
	At           *time.Time `name:"at" help:"One-shot run time in RFC3339 (example: 2026-02-25T18:00:00Z)."`
	Every        string     `name:"every" help:"Recurring interval in Go duration format (example: 30s, 10m, 1h)."`
}

type TasksLsCmd struct {
	Conversation string `name:"conversation" help:"Optional conversation ID filter."`
}

type TasksRmCmd struct {
	ID string `name:"id" required:"" help:"Task ID."`
}

type TasksUpdateCmd struct {
	RunID              string `name:"run-id" required:"" help:"Task run ID."`
	CallerID           string `name:"caller-id" required:"" help:"Caller identity used for backend authorization."`
	Status             string `name:"status" required:"" enum:"success,failed" help:"Run terminal status."`
	Result             string `name:"result" help:"Success result content."`
	Error              string `name:"error" help:"Failure error content."`
	AssistantMessageID string `name:"assistant-message-id" help:"Assistant message ID linked to this run."`
}

type Globals struct {
	cfg    config.Config
	logger *slog.Logger
}

func main() {
	var cli CLI
	parser, err := kong.New(
		&cli,
		kong.Name("goclaw"),
		kong.Description("goclaw assistant runtime CLI"),
		kong.UsageOnError(),
	)
	if err != nil {
		slog.Error("build cli parser", "error", err)
		os.Exit(1)
	}

	ctx, err := parser.Parse(os.Args[1:])
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger := mustBuildLogger(cfg)
	slog.SetDefault(logger)
	globals := &Globals{cfg: cfg, logger: logger}

	if err := ctx.Run(globals); err != nil {
		logger.Error("command failed", "error", err)
		os.Exit(1)
	}
}

func (c *TermCmd) Run(globals *Globals) error {
	return runMode(globals, "sender")
}

func (c *WorkerCmd) Run(globals *Globals) error {
	return runMode(globals, "worker")
}

func (c *SchedulerCmd) Run(globals *Globals) error {
	return runMode(globals, "scheduler")
}

func (c *SingleCmd) Run(globals *Globals) error {
	return runMode(globals, "single")
}

func (c *TasksCreateCmd) Run(globals *Globals) error {
	st, err := store.NewSQLiteStore(globals.cfg.DatabasePath)
	if err != nil {
		return trace.Wrap(err)
	}
	defer st.Close()

	var runAt *time.Time
	if c.At != nil {
		t := c.At.UTC()
		runAt = &t
	}

	intervalSec := 0
	if strings.TrimSpace(c.Every) != "" {
		d, err := time.ParseDuration(c.Every)
		if err != nil {
			return trace.Wrap(err)
		}
		intervalSec = int(d.Seconds())
		if intervalSec < 1 {
			return trace.BadParameter("--every must be >= 1s")
		}
	}

	task, err := st.CreateTask(context.Background(), c.Conversation, c.Payload, runAt, intervalSec)
	if err != nil {
		return trace.Wrap(err)
	}
	fmt.Printf("created task id=%s conversation_id=%s schedule_type=%s\n", task.ID, task.ConversationID, task.ScheduleType)
	return nil
}

func (c *TasksLsCmd) Run(globals *Globals) error {
	st, err := store.NewSQLiteStore(globals.cfg.DatabasePath)
	if err != nil {
		return trace.Wrap(err)
	}
	defer st.Close()

	tasks, err := st.ListTasks(context.Background(), c.Conversation)
	if err != nil {
		return trace.Wrap(err)
	}
	for _, t := range tasks {
		fmt.Printf("id=%s conversation_id=%s status=%s next_run_at=%s schedule=%s interval_sec=%d failures=%d\n",
			t.ID, t.ConversationID, t.Status, t.NextRunAt.Format(time.RFC3339), t.ScheduleType, t.IntervalSec, t.FailureCount)
	}
	return nil
}

func (c *TasksRmCmd) Run(globals *Globals) error {
	st, err := store.NewSQLiteStore(globals.cfg.DatabasePath)
	if err != nil {
		return trace.Wrap(err)
	}
	defer st.Close()

	if err := st.RemoveTask(context.Background(), c.ID); err != nil {
		return trace.Wrap(err)
	}
	fmt.Printf("removed task id=%s\n", c.ID)
	return nil
}

func (c *TasksUpdateCmd) Run(globals *Globals) error {
	st, err := store.NewSQLiteStore(globals.cfg.DatabasePath)
	if err != nil {
		return trace.Wrap(err)
	}
	defer st.Close()

	var status types.TaskRunStatus
	switch strings.ToLower(strings.TrimSpace(c.Status)) {
	case "success":
		status = types.TaskRunStatusSuccess
	case "failed":
		status = types.TaskRunStatusFailed
	default:
		return trace.BadParameter("--status must be success|failed")
	}

	if err := st.UpdateTaskRunAuthorized(context.Background(), c.RunID, c.CallerID, status, c.Result, c.Error, c.AssistantMessageID); err != nil {
		return trace.Wrap(err)
	}
	fmt.Printf("updated run id=%s status=%s\n", c.RunID, status)
	return nil
}

func runMode(globals *Globals, mode string) error {
	application, err := app.New(globals.cfg, globals.logger)
	if err != nil {
		return trace.Wrap(err)
	}
	defer application.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return trace.Wrap(application.Run(ctx, mode))
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
