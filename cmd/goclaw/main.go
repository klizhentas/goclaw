package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/alecthomas/kong"
	"github.com/gravitational/trace"
	"github.com/klizhentas/goclaw/internal/app"
	"github.com/klizhentas/goclaw/internal/config"
	"github.com/klizhentas/goclaw/internal/store"
	"github.com/klizhentas/goclaw/internal/types"
)

type CLI struct {
	Run       RunCmd       `cmd:"" help:"Run one or more modes concurrently (example: run --mode=term,scheduler)."`
	Term      TermCmd      `cmd:"" help:"Start terminal mode (enqueue user input, print outbound responses)." aliases:"terminal"`
	Worker    WorkerCmd    `cmd:"" help:"Start worker mode (claim and process inbound queue messages)."`
	Scheduler SchedulerCmd `cmd:"" help:"Start scheduler mode (claim due tasks and enqueue task runs)."`
	Single    SingleCmd    `cmd:"" help:"Run single-process interactive mode."`
	Tasks     TasksCmd     `cmd:"" help:"Manage task definitions and task runs."`
}

type RunCmd struct {
	Mode     string `name:"mode" required:"" help:"Comma-separated modes: term,worker,scheduler,single."`
	WorkerID string `name:"worker-id" help:"Worker identity override (used when mode includes worker). If omitted, worker ID is auto-generated at startup."`
}

type TermCmd struct{}
type WorkerCmd struct {
	WorkerID string `name:"worker-id" help:"Worker identity override. If omitted, worker ID is auto-generated at startup."`
}
type SchedulerCmd struct{}
type SingleCmd struct{}

type TasksCmd struct {
	Create         TasksCreateCmd         `cmd:"" help:"Create one-shot or recurring task."`
	Ls             TasksLsCmd             `cmd:"" help:"List tasks (optionally by conversation)." aliases:"list"`
	LsAll          TasksLsAllCmd          `cmd:"" help:"List all tasks globally."`
	LsConversation TasksLsConversationCmd `cmd:"" help:"List tasks for one conversation." aliases:"ls-conversation"`
	Rm             TasksRmCmd             `cmd:"" help:"Soft-delete a task by ID." aliases:"remove,delete"`
	RmAll          TasksRmAllCmd          `cmd:"" help:"Soft-delete tasks globally or for one conversation." aliases:"remove-all,delete-all"`
	Status         TasksStatusCmd         `cmd:"" help:"Show scheduler/task/queue diagnostics in table format."`
	Update         TasksUpdateCmd         `cmd:"" help:"Update a task run status (authorized backend path)."`
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

type TasksLsAllCmd struct{}

type TasksLsConversationCmd struct {
	Conversation string `name:"conversation" required:"" help:"Conversation ID filter."`
}

type TasksRmCmd struct {
	ID string `name:"id" required:"" help:"Task ID."`
}

type TasksRmAllCmd struct {
	Conversation string `name:"conversation" help:"Optional conversation ID filter for bulk removal."`
	All          bool   `name:"all" help:"Remove all tasks globally when no conversation filter is provided."`
}

type TasksStatusCmd struct {
	Conversation string `name:"conversation" help:"Optional conversation ID filter."`
	Limit        int    `name:"limit" default:"20" help:"Max number of task rows to display."`
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
	applyCLIWorkerIDOverride(&cfg, &cli)
	if strings.TrimSpace(cfg.WorkerID) == "" {
		cfg.WorkerID = autoWorkerID()
	}
	logger := mustBuildLogger(cfg)
	slog.SetDefault(logger)
	globals := &Globals{cfg: cfg, logger: logger}
	installDebugSignalHandler(logger)

	if err := ctx.Run(globals); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		logger.Error("command failed", "error", err)
		os.Exit(1)
	}
}

func (c *TermCmd) Run(globals *Globals) error {
	return runMode(globals, "term")
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

func (c *RunCmd) Run(globals *Globals) error {
	modes, err := parseModes(c.Mode)
	if err != nil {
		return trace.Wrap(err)
	}
	return runModes(globals, modes)
}

func applyCLIWorkerIDOverride(cfg *config.Config, cli *CLI) {
	if cfg == nil || cli == nil {
		return
	}
	if v := strings.TrimSpace(cli.Run.WorkerID); v != "" {
		cfg.WorkerID = v
	}
	if v := strings.TrimSpace(cli.Worker.WorkerID); v != "" {
		cfg.WorkerID = v
	}
}

func autoWorkerID() string {
	return fmt.Sprintf("worker-%d-%d", os.Getpid(), time.Now().UnixNano()%1_000_000)
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
	return printTasks(tasks)
}

func (c *TasksLsAllCmd) Run(globals *Globals) error {
	st, err := store.NewSQLiteStore(globals.cfg.DatabasePath)
	if err != nil {
		return trace.Wrap(err)
	}
	defer st.Close()

	tasks, err := st.ListTasks(context.Background(), "")
	if err != nil {
		return trace.Wrap(err)
	}
	return printTasks(tasks)
}

func (c *TasksLsConversationCmd) Run(globals *Globals) error {
	st, err := store.NewSQLiteStore(globals.cfg.DatabasePath)
	if err != nil {
		return trace.Wrap(err)
	}
	defer st.Close()

	tasks, err := st.ListTasks(context.Background(), c.Conversation)
	if err != nil {
		return trace.Wrap(err)
	}
	return printTasks(tasks)
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

func (c *TasksRmAllCmd) Run(globals *Globals) error {
	st, err := store.NewSQLiteStore(globals.cfg.DatabasePath)
	if err != nil {
		return trace.Wrap(err)
	}
	defer st.Close()

	conversation := strings.TrimSpace(c.Conversation)
	if conversation == "" && !c.All {
		return trace.BadParameter("use --all to remove all tasks globally, or pass --conversation")
	}
	removed, err := st.RemoveTasks(context.Background(), conversation)
	if err != nil {
		return trace.Wrap(err)
	}
	if conversation == "" {
		fmt.Printf("removed %d tasks globally\n", removed)
		return nil
	}
	fmt.Printf("removed %d tasks for conversation=%s\n", removed, conversation)
	return nil
}

func (c *TasksStatusCmd) Run(globals *Globals) error {
	st, err := store.NewSQLiteStore(globals.cfg.DatabasePath)
	if err != nil {
		return trace.Wrap(err)
	}
	defer st.Close()

	diag, err := st.GetTaskDiagnostics(context.Background(), c.Conversation, c.Limit)
	if err != nil {
		return trace.Wrap(err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	scope := diag.ConversationID
	if scope == "" {
		scope = "(all)"
	}

	fmt.Fprintln(w, "SYSTEM\tVALUE")
	fmt.Fprintf(w, "generated_at\t%s\n", diag.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "conversation_scope\t%s\n", scope)
	fmt.Fprintf(w, "tasks_total\t%d\n", diag.TasksTotal)
	fmt.Fprintf(w, "tasks_active_due_now\t%d\n", diag.TasksActiveDue)
	fmt.Fprintf(w, "tasks_claimable_due_now\t%d\n", diag.TasksClaimable)
	fmt.Fprintf(w, "tasks_leased\t%d\n", diag.TasksLeased)
	fmt.Fprintf(w, "task_runs_queued\t%d\n", diag.TaskRunsQueued)
	fmt.Fprintf(w, "task_runs_running\t%d\n", diag.TaskRunsRunning)
	fmt.Fprintf(w, "workers_observed_5m\t%s\n", listOrNone(diag.WorkerIDs))
	fmt.Fprintf(w, "schedulers_observed_15m\t%s\n", listOrNone(diag.SchedulerIDs))
	fmt.Fprintf(w, "worker_capacity_hint\t%s\n", workerCapacityHint(diag))
	fmt.Fprintln(w)

	fmt.Fprintln(w, "QUEUE\tPENDING\tPROCESSING\tDONE\tERROR\tOLDEST_PENDING_AGE")
	printQueueRow(w, diag.InboundQueue)
	printQueueRow(w, diag.OutboundQueue)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "TASK_ID\tCONVERSATION\tSTATUS\tSCHEDULE\tNEXT_RUN_AT\tLEASED_BY\tLEASE_EXPIRES_AT\tFAILURES\tLAST_RUN\tLAST_RUN_AT\tLAST_ERROR")
	for _, t := range diag.TaskSnapshotRows {
		leaseExpires := "-"
		if t.LeaseExpiresAt != nil {
			leaseExpires = t.LeaseExpiresAt.Format(time.RFC3339)
		}
		leasedBy := t.AssignedSchedulerID
		if leasedBy == "" {
			leasedBy = "-"
		}
		lastRun := t.LastRunStatus
		if lastRun == "" {
			lastRun = "-"
		}
		lastRunAt := "-"
		if t.LastRunStartedAt != nil {
			lastRunAt = t.LastRunStartedAt.Format(time.RFC3339)
		}
		lastErr := truncateForTable(t.LastError, 80)
		if strings.TrimSpace(lastErr) == "" {
			lastErr = "-"
		}
		fmt.Fprintf(
			w,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
			t.ID,
			t.ConversationID,
			t.Status,
			t.ScheduleType,
			t.NextRunAt.Format(time.RFC3339),
			leasedBy,
			leaseExpires,
			t.FailureCount,
			lastRun,
			lastRunAt,
			lastErr,
		)
	}
	if len(diag.TaskSnapshotRows) == 0 {
		fmt.Fprintln(w, "(no tasks)\t-\t-\t-\t-\t-\t-\t-\t-\t-\t-")
	}

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

func listOrNone(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ",")
}

func printTasks(tasks []types.Task) error {
	for _, t := range tasks {
		fmt.Printf("id=%s conversation_id=%s status=%s next_run_at=%s schedule=%s interval_sec=%d failures=%d\n",
			t.ID, t.ConversationID, t.Status, t.NextRunAt.Format(time.RFC3339), t.ScheduleType, t.IntervalSec, t.FailureCount)
	}
	return nil
}

func workerCapacityHint(diag store.TaskDiagnostics) string {
	workers := len(diag.WorkerIDs)
	pending := diag.InboundQueue.Pending
	processing := diag.InboundQueue.Processing

	switch {
	case pending == 0 && processing == 0:
		return "OK (no inbound backlog)"
	case workers == 0 && (pending > 0 || processing > 0):
		return "NO (no workers observed in last 5m)"
	case pending > workers*2:
		return "LOW (pending backlog exceeds observed workers)"
	default:
		return "OK"
	}
}

func printQueueRow(w *tabwriter.Writer, q store.QueueDirectionStatus) {
	age := "-"
	if q.OldestPendingAt != nil {
		age = fmt.Sprintf("%ds", q.OldestPendingAgeS)
	}
	fmt.Fprintf(
		w,
		"%s\t%d\t%d\t%d\t%d\t%s\n",
		q.Direction,
		q.Pending,
		q.Processing,
		q.Done,
		q.Error,
		age,
	)
}

func truncateForTable(raw string, max int) string {
	if len(raw) <= max {
		return raw
	}
	if max <= 3 {
		return raw[:max]
	}
	return strings.TrimSpace(raw[:max-3]) + "..."
}

func runMode(globals *Globals, mode string) error {
	modes, err := parseModes(mode)
	if err != nil {
		return trace.Wrap(err)
	}
	if len(modes) != 1 {
		return trace.BadParameter("expected single mode, got %q", mode)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return trace.Wrap(runModeWithContext(ctx, globals, modes[0]))
}

func runModes(globals *Globals, modes []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(modes) == 1 {
		return trace.Wrap(runModeWithContext(ctx, globals, modes[0]))
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type modeResult struct {
		mode string
		err  error
	}

	foreground := ""
	for _, mode := range modes {
		if mode == "term" {
			foreground = mode
			break
		}
	}

	background := make([]string, 0, len(modes))
	for _, mode := range modes {
		if mode == foreground {
			continue
		}
		background = append(background, mode)
	}

	resultCh := make(chan modeResult, len(background))
	var wg sync.WaitGroup
	for _, mode := range background {
		mode := mode
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runModeWithContext(runCtx, globals, mode); err != nil {
				resultCh <- modeResult{mode: mode, err: err}
				cancel()
				return
			}
			resultCh <- modeResult{mode: mode, err: nil}
		}()
	}

	bgDone := make(chan struct{})
	go func() {
		defer close(bgDone)
		wg.Wait()
	}()

	var fgErr error
	if foreground != "" {
		fgErr = runModeWithContext(runCtx, globals, foreground)
		cancel()
		<-bgDone
		if fgErr != nil {
			return trace.Wrap(fgErr, "mode=%s", foreground)
		}
	} else {
		<-bgDone
	}

	close(resultCh)
	for result := range resultCh {
		if result.err != nil {
			return trace.Wrap(result.err, "mode=%s", result.mode)
		}
	}
	return nil
}

func runModeWithContext(ctx context.Context, globals *Globals, mode string) error {
	application, err := app.New(globals.cfg, globals.logger)
	if err != nil {
		return trace.Wrap(err)
	}
	defer application.Close()

	return trace.Wrap(application.Run(ctx, mode))
}

func parseModes(raw string) ([]string, error) {
	fields := strings.Split(strings.ToLower(strings.TrimSpace(raw)), ",")
	seen := make(map[string]struct{})
	modes := make([]string, 0, len(fields))
	for _, f := range fields {
		m := strings.TrimSpace(f)
		if m == "" {
			continue
		}
		switch m {
		case "term":
		case "worker":
		case "scheduler":
		case "single":
		default:
			return nil, trace.BadParameter("unsupported mode %q (use term,worker,scheduler,single)", m)
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		modes = append(modes, m)
	}
	if len(modes) == 0 {
		return nil, trace.BadParameter("--mode must include at least one mode")
	}
	if len(modes) > 1 {
		for _, m := range modes {
			if m == "single" {
				return nil, trace.BadParameter("mode single cannot be combined with other modes")
			}
		}
	}
	return modes, nil
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

func installDebugSignalHandler(logger *slog.Logger) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR2)

	go func() {
		for range ch {
			dump := goroutineDump()
			_, _ = fmt.Fprintf(os.Stderr, "\n=== SIGUSR2 goroutine dump (pid=%d) ===\n%s\n", os.Getpid(), dump)
			logger.Error(
				"received SIGUSR2; goroutine dump",
				"stage", "ingress",
				"goroutines", runtime.NumGoroutine(),
				"stack", dump,
			)
		}
	}()
}

func goroutineDump() string {
	var buf bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&buf, 2); err != nil {
		return fmt.Sprintf("failed to capture goroutine dump: %v", err)
	}
	return buf.String()
}
