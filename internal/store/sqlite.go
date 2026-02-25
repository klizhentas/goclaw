package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/klizhentas/goclaw/internal/types"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	store := &SQLiteStore{db: db}
	if err := store.initSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) initSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
PRAGMA journal_mode = WAL;
CREATE TABLE IF NOT EXISTS conversations (
	id TEXT PRIMARY KEY,
	role TEXT NOT NULL,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS messages (
	id TEXT PRIMARY KEY,
	conversation_id TEXT NOT NULL,
	role TEXT NOT NULL,
	content TEXT NOT NULL,
	created_at DATETIME NOT NULL,
	FOREIGN KEY(conversation_id) REFERENCES conversations(id)
);
CREATE INDEX IF NOT EXISTS idx_messages_conversation_created
	ON messages(conversation_id, created_at, id);
CREATE TABLE IF NOT EXISTS queue_messages (
	id TEXT PRIMARY KEY,
	conversation_id TEXT NOT NULL,
	direction TEXT NOT NULL,
	content TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at DATETIME NOT NULL,
	claimed_at DATETIME,
	processed_at DATETIME,
	worker_id TEXT,
	error TEXT,
	related_message_id TEXT
);
CREATE INDEX IF NOT EXISTS idx_queue_messages_lookup
	ON queue_messages(direction, status, created_at, id);
CREATE TABLE IF NOT EXISTS tasks (
	id TEXT PRIMARY KEY,
	conversation_id TEXT NOT NULL,
	payload TEXT NOT NULL,
	schedule_type TEXT NOT NULL,
	run_at DATETIME,
	interval_sec INTEGER NOT NULL DEFAULT 0,
	status TEXT NOT NULL,
	next_run_at DATETIME NOT NULL,
	assigned_scheduler_id TEXT,
	lease_expires_at DATETIME,
	failure_count INTEGER NOT NULL DEFAULT 0,
	last_error TEXT,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tasks_due
	ON tasks(status, next_run_at, lease_expires_at, id);
CREATE TABLE IF NOT EXISTS task_runs (
	id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	conversation_id TEXT NOT NULL,
	payload TEXT NOT NULL,
	status TEXT NOT NULL,
	scheduler_id TEXT,
	started_at DATETIME NOT NULL,
	finished_at DATETIME,
	result_content TEXT,
	error TEXT,
	inbound_queue_message_id TEXT,
	assistant_message_id TEXT,
	created_at DATETIME NOT NULL,
	FOREIGN KEY(task_id) REFERENCES tasks(id)
);
CREATE INDEX IF NOT EXISTS idx_task_runs_task_created
	ON task_runs(task_id, created_at, id);
`); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CreateConversationIfMissing(ctx context.Context, conversationID string, role types.ConversationRole) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO conversations (id, role, created_at, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING;
`, conversationID, role, nowUTC(), nowUTC())
	if err != nil {
		return fmt.Errorf("create conversation: %w", err)
	}
	return nil
}

func (s *SQLiteStore) AppendMessage(ctx context.Context, conversationID string, role types.MessageRole, content string) (types.Message, error) {
	id, err := newID()
	if err != nil {
		return types.Message{}, err
	}
	msg := types.Message{
		ID:             id,
		ConversationID: conversationID,
		Role:           role,
		Content:        content,
		CreatedAt:      nowUTC(),
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO messages (id, conversation_id, role, content, created_at)
VALUES (?, ?, ?, ?, ?);
`, msg.ID, msg.ConversationID, msg.Role, msg.Content, msg.CreatedAt)
	if err != nil {
		return types.Message{}, fmt.Errorf("append message: %w", err)
	}
	return msg, nil
}

func (s *SQLiteStore) GetRecentMessages(ctx context.Context, conversationID string, limit int) ([]types.Message, error) {
	if limit < 1 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, conversation_id, role, content, created_at
FROM messages
WHERE conversation_id = ?
ORDER BY created_at DESC, id DESC
LIMIT ?;
`, conversationID, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent messages: %w", err)
	}
	defer rows.Close()

	recent := make([]types.Message, 0, limit)
	for rows.Next() {
		var msg types.Message
		if err := rows.Scan(&msg.ID, &msg.ConversationID, &msg.Role, &msg.Content, &msg.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		recent = append(recent, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}

	for i, j := 0, len(recent)-1; i < j; i, j = i+1, j-1 {
		recent[i], recent[j] = recent[j], recent[i]
	}
	return recent, nil
}

func (s *SQLiteStore) EnqueueInbound(ctx context.Context, conversationID, content string) (types.QueueMessage, error) {
	return s.enqueueInbound(ctx, conversationID, content, "")
}

func (s *SQLiteStore) EnqueueInboundForTaskRun(ctx context.Context, conversationID, content, taskRunID string) (types.QueueMessage, error) {
	return s.enqueueInbound(ctx, conversationID, content, taskRunID)
}

func (s *SQLiteStore) enqueueInbound(ctx context.Context, conversationID, content, relatedID string) (types.QueueMessage, error) {
	id, err := newID()
	if err != nil {
		return types.QueueMessage{}, err
	}
	item := types.QueueMessage{
		ID:               id,
		ConversationID:   conversationID,
		Direction:        types.QueueDirectionInbound,
		Content:          content,
		Status:           types.QueueStatusPending,
		CreatedAt:        nowUTC(),
		RelatedMessageID: relatedID,
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO queue_messages (id, conversation_id, direction, content, status, created_at, related_message_id)
VALUES (?, ?, ?, ?, ?, ?, ?);
`, item.ID, item.ConversationID, item.Direction, item.Content, item.Status, item.CreatedAt, item.RelatedMessageID)
	if err != nil {
		return types.QueueMessage{}, fmt.Errorf("enqueue inbound: %w", err)
	}
	return item, nil
}

func (s *SQLiteStore) EnqueueOutbound(ctx context.Context, conversationID, content, relatedMessageID string) (types.QueueMessage, error) {
	id, err := newID()
	if err != nil {
		return types.QueueMessage{}, err
	}
	now := nowUTC()
	item := types.QueueMessage{
		ID:               id,
		ConversationID:   conversationID,
		Direction:        types.QueueDirectionOutbound,
		Content:          content,
		Status:           types.QueueStatusPending,
		CreatedAt:        now,
		RelatedMessageID: relatedMessageID,
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO queue_messages (id, conversation_id, direction, content, status, created_at, related_message_id)
VALUES (?, ?, ?, ?, ?, ?, ?);
`, item.ID, item.ConversationID, item.Direction, item.Content, item.Status, item.CreatedAt, item.RelatedMessageID)
	if err != nil {
		return types.QueueMessage{}, fmt.Errorf("enqueue outbound: %w", err)
	}
	return item, nil
}

func (s *SQLiteStore) ClaimNextInbound(ctx context.Context, workerID string) (*types.QueueMessage, error) {
	return s.claimNextByDirection(ctx, workerID, types.QueueDirectionInbound)
}

func (s *SQLiteStore) ClaimNextOutbound(ctx context.Context, workerID string) (*types.QueueMessage, error) {
	return s.claimNextByDirection(ctx, workerID, types.QueueDirectionOutbound)
}

func (s *SQLiteStore) claimNextByDirection(ctx context.Context, workerID string, direction types.QueueDirection) (*types.QueueMessage, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var item types.QueueMessage
	var related sql.NullString
	err = tx.QueryRowContext(ctx, `
SELECT id, conversation_id, direction, content, status, created_at, related_message_id
FROM queue_messages
WHERE direction = ? AND status = ?
ORDER BY created_at, id
LIMIT 1;
`, direction, types.QueueStatusPending).Scan(
		&item.ID,
		&item.ConversationID,
		&item.Direction,
		&item.Content,
		&item.Status,
		&item.CreatedAt,
		&related,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			if err := tx.Commit(); err != nil {
				return nil, fmt.Errorf("commit empty claim tx: %w", err)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("select pending %s: %w", direction, err)
	}
	if related.Valid {
		item.RelatedMessageID = related.String
	}

	claimedAt := nowUTC()
	result, err := tx.ExecContext(ctx, `
UPDATE queue_messages
SET status = ?, worker_id = ?, claimed_at = ?
WHERE id = ? AND status = ?;
`, types.QueueStatusProcessing, workerID, claimedAt, item.ID, types.QueueStatusPending)
	if err != nil {
		return nil, fmt.Errorf("claim %s: %w", direction, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("claim rows affected: %w", err)
	}
	if affected != 1 {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit stale claim tx: %w", err)
		}
		return nil, nil
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim tx: %w", err)
	}
	item.Status = types.QueueStatusProcessing
	item.WorkerID = workerID
	item.ClaimedAt = &claimedAt
	return &item, nil
}

func (s *SQLiteStore) MarkQueueDone(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE queue_messages
SET status = ?, processed_at = ?
WHERE id = ?;
`, types.QueueStatusDone, nowUTC(), id)
	if err != nil {
		return fmt.Errorf("mark queue done: %w", err)
	}
	return nil
}

func (s *SQLiteStore) MarkQueueError(ctx context.Context, id string, errMessage string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE queue_messages
SET status = ?, processed_at = ?, error = ?
WHERE id = ?;
`, types.QueueStatusError, nowUTC(), errMessage, id)
	if err != nil {
		return fmt.Errorf("mark queue error: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CreateTask(ctx context.Context, conversationID, payload string, runAt *time.Time, intervalSec int) (types.Task, error) {
	id, err := newID()
	if err != nil {
		return types.Task{}, err
	}
	now := nowUTC()
	scheduleType := types.TaskScheduleTypeOneShot
	nextRunAt := now
	if runAt != nil {
		nextRunAt = runAt.UTC()
	}
	if intervalSec > 0 {
		scheduleType = types.TaskScheduleTypeInterval
	}

	task := types.Task{
		ID:             id,
		ConversationID: conversationID,
		Payload:        payload,
		ScheduleType:   scheduleType,
		RunAt:          runAt,
		IntervalSec:    intervalSec,
		Status:         types.TaskStatusActive,
		NextRunAt:      nextRunAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO tasks (id, conversation_id, payload, schedule_type, run_at, interval_sec, status, next_run_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
`, task.ID, task.ConversationID, task.Payload, task.ScheduleType, nullableTime(task.RunAt), task.IntervalSec, task.Status, task.NextRunAt, task.CreatedAt, task.UpdatedAt)
	if err != nil {
		return types.Task{}, fmt.Errorf("create task: %w", err)
	}
	return task, nil
}

func (s *SQLiteStore) ListTasks(ctx context.Context, conversationID string) ([]types.Task, error) {
	query := `
SELECT id, conversation_id, payload, schedule_type, run_at, interval_sec, status, next_run_at,
       assigned_scheduler_id, lease_expires_at, failure_count, last_error, created_at, updated_at
FROM tasks
WHERE status != ?`
	args := []any{types.TaskStatusDeleted}
	if conversationID != "" {
		query += " AND conversation_id = ?"
		args = append(args, conversationID)
	}
	query += " ORDER BY created_at DESC, id DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	result := make([]types.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tasks: %w", err)
	}
	return result, nil
}

func (s *SQLiteStore) RemoveTask(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE tasks
SET status = ?, updated_at = ?, assigned_scheduler_id = NULL, lease_expires_at = NULL
WHERE id = ?;
`, types.TaskStatusDeleted, nowUTC(), taskID)
	if err != nil {
		return fmt.Errorf("remove task: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ClaimDueTask(ctx context.Context, schedulerID string, lease time.Duration) (*types.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin claim task tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := nowUTC()
	var task types.Task
	row := tx.QueryRowContext(ctx, `
SELECT t.id, t.conversation_id, t.payload, t.schedule_type, t.run_at, t.interval_sec, t.status, t.next_run_at,
       t.assigned_scheduler_id, t.lease_expires_at, t.failure_count, t.last_error, t.created_at, t.updated_at
FROM tasks t
WHERE t.status = ?
  AND t.next_run_at <= ?
  AND (t.assigned_scheduler_id IS NULL OR t.assigned_scheduler_id = '' OR t.lease_expires_at IS NULL OR t.lease_expires_at <= ?)
  AND NOT EXISTS (
    SELECT 1 FROM task_runs r
    WHERE r.task_id = t.id AND r.status IN (?, ?)
  )
ORDER BY t.next_run_at, t.id
LIMIT 1;
`, types.TaskStatusActive, now, now, types.TaskRunStatusQueued, types.TaskRunStatusRunning)
	if err := scanTaskFromRow(row, &task); err != nil {
		if err == sql.ErrNoRows {
			if err := tx.Commit(); err != nil {
				return nil, fmt.Errorf("commit empty task claim tx: %w", err)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("select due task: %w", err)
	}

	leaseUntil := now.Add(lease)
	result, err := tx.ExecContext(ctx, `
UPDATE tasks
SET assigned_scheduler_id = ?, lease_expires_at = ?, updated_at = ?
WHERE id = ?
  AND status = ?
  AND next_run_at <= ?
  AND (assigned_scheduler_id IS NULL OR assigned_scheduler_id = '' OR lease_expires_at IS NULL OR lease_expires_at <= ?);
`, schedulerID, leaseUntil, now, task.ID, types.TaskStatusActive, now, now)
	if err != nil {
		return nil, fmt.Errorf("claim task cas update: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("claim task rows affected: %w", err)
	}
	if affected != 1 {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit stale task claim tx: %w", err)
		}
		return nil, nil
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit task claim tx: %w", err)
	}
	task.AssignedSchedulerID = schedulerID
	task.LeaseExpiresAt = &leaseUntil
	task.UpdatedAt = now
	return &task, nil
}

func (s *SQLiteStore) CreateTaskRunQueued(ctx context.Context, task types.Task, schedulerID string) (types.TaskRun, error) {
	runID, err := newID()
	if err != nil {
		return types.TaskRun{}, err
	}
	now := nowUTC()
	run := types.TaskRun{
		ID:             runID,
		TaskID:         task.ID,
		ConversationID: task.ConversationID,
		Payload:        task.Payload,
		Status:         types.TaskRunStatusQueued,
		SchedulerID:    schedulerID,
		StartedAt:      now,
		CreatedAt:      now,
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO task_runs (id, task_id, conversation_id, payload, status, scheduler_id, started_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);
`, run.ID, run.TaskID, run.ConversationID, run.Payload, run.Status, run.SchedulerID, run.StartedAt, run.CreatedAt)
	if err != nil {
		return types.TaskRun{}, fmt.Errorf("create task run: %w", err)
	}
	return run, nil
}

func (s *SQLiteStore) SetTaskRunInboundQueueMessageID(ctx context.Context, runID, queueMessageID string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE task_runs
SET inbound_queue_message_id = ?
WHERE id = ?;
`, queueMessageID, runID)
	if err != nil {
		return fmt.Errorf("set task run inbound queue message id: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ReleaseTaskClaim(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE tasks
SET assigned_scheduler_id = NULL, lease_expires_at = NULL, updated_at = ?
WHERE id = ?;
`, nowUTC(), taskID)
	if err != nil {
		return fmt.Errorf("release task claim: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CompleteTaskRunSuccess(ctx context.Context, runID, resultContent, assistantMessageID string) error {
	return s.completeTaskRun(ctx, runID, true, resultContent, "", assistantMessageID)
}

func (s *SQLiteStore) CompleteTaskRunFailure(ctx context.Context, runID, errorMessage string) error {
	return s.completeTaskRun(ctx, runID, false, "", errorMessage, "")
}

func (s *SQLiteStore) completeTaskRun(ctx context.Context, runID string, success bool, resultContent, errorMessage, assistantMessageID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin complete task run tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var taskID string
	if err := tx.QueryRowContext(ctx, `SELECT task_id FROM task_runs WHERE id = ?;`, runID).Scan(&taskID); err != nil {
		return fmt.Errorf("select task_id for run %s: %w", runID, err)
	}

	now := nowUTC()
	if success {
		_, err = tx.ExecContext(ctx, `
UPDATE task_runs
SET status = ?, finished_at = ?, result_content = ?, error = NULL, assistant_message_id = ?
WHERE id = ?;
`, types.TaskRunStatusSuccess, now, resultContent, assistantMessageID, runID)
		if err != nil {
			return fmt.Errorf("update task run success: %w", err)
		}

		var scheduleType types.TaskScheduleType
		var intervalSec int
		var status types.TaskStatus
		if err := tx.QueryRowContext(ctx, `SELECT schedule_type, interval_sec, status FROM tasks WHERE id = ?;`, taskID).Scan(&scheduleType, &intervalSec, &status); err != nil {
			return fmt.Errorf("select task scheduling info: %w", err)
		}

		if scheduleType == types.TaskScheduleTypeInterval && status == types.TaskStatusActive {
			nextRun := now.Add(time.Duration(intervalSec) * time.Second)
			_, err = tx.ExecContext(ctx, `
UPDATE tasks
SET next_run_at = ?, failure_count = 0, last_error = NULL, assigned_scheduler_id = NULL, lease_expires_at = NULL, updated_at = ?
WHERE id = ?;
`, nextRun, now, taskID)
			if err != nil {
				return fmt.Errorf("update recurring task after success: %w", err)
			}
		} else {
			_, err = tx.ExecContext(ctx, `
UPDATE tasks
SET status = ?, failure_count = 0, last_error = NULL, assigned_scheduler_id = NULL, lease_expires_at = NULL, updated_at = ?
WHERE id = ?;
`, types.TaskStatusCompleted, now, taskID)
			if err != nil {
				return fmt.Errorf("update one-shot task after success: %w", err)
			}
		}
	} else {
		_, err = tx.ExecContext(ctx, `
UPDATE task_runs
SET status = ?, finished_at = ?, error = ?, result_content = NULL
WHERE id = ?;
`, types.TaskRunStatusFailed, now, errorMessage, runID)
		if err != nil {
			return fmt.Errorf("update task run failure: %w", err)
		}

		var failureCount int
		if err := tx.QueryRowContext(ctx, `SELECT failure_count FROM tasks WHERE id = ?;`, taskID).Scan(&failureCount); err != nil {
			return fmt.Errorf("select task failure count: %w", err)
		}
		failureCount++
		nextRun := now.Add(backoffForFailures(failureCount))
		_, err = tx.ExecContext(ctx, `
UPDATE tasks
SET next_run_at = ?, failure_count = ?, last_error = ?, assigned_scheduler_id = NULL, lease_expires_at = NULL, updated_at = ?
WHERE id = ?;
`, nextRun, failureCount, errorMessage, now, taskID)
		if err != nil {
			return fmt.Errorf("update task after failure: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit complete task run tx: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateTaskRun(ctx context.Context, runID string, status types.TaskRunStatus, resultContent, errorMessage, assistantMessageID string) error {
	switch status {
	case types.TaskRunStatusSuccess:
		return s.CompleteTaskRunSuccess(ctx, runID, resultContent, assistantMessageID)
	case types.TaskRunStatusFailed:
		return s.CompleteTaskRunFailure(ctx, runID, errorMessage)
	default:
		return fmt.Errorf("unsupported task run update status %q", status)
	}
}

func (s *SQLiteStore) UpdateTaskRunAuthorized(ctx context.Context, runID, callerID string, status types.TaskRunStatus, resultContent, errorMessage, assistantMessageID string) error {
	var schedulerID sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT scheduler_id FROM task_runs WHERE id = ?;`, runID).Scan(&schedulerID); err != nil {
		return fmt.Errorf("load task run for authorization: %w", err)
	}
	if callerID == "" {
		return fmt.Errorf("caller_id is required")
	}
	if schedulerID.Valid && schedulerID.String != "" && schedulerID.String != callerID {
		return fmt.Errorf("caller %q is not allowed to update run %q assigned to %q", callerID, runID, schedulerID.String)
	}
	return s.UpdateTaskRun(ctx, runID, status, resultContent, errorMessage, assistantMessageID)
}

func scanTask(rows *sql.Rows) (types.Task, error) {
	var task types.Task
	var runAt sql.NullTime
	var leaseExpiresAt sql.NullTime
	var assigned sql.NullString
	var lastError sql.NullString
	if err := rows.Scan(
		&task.ID,
		&task.ConversationID,
		&task.Payload,
		&task.ScheduleType,
		&runAt,
		&task.IntervalSec,
		&task.Status,
		&task.NextRunAt,
		&assigned,
		&leaseExpiresAt,
		&task.FailureCount,
		&lastError,
		&task.CreatedAt,
		&task.UpdatedAt,
	); err != nil {
		return types.Task{}, fmt.Errorf("scan task: %w", err)
	}
	if runAt.Valid {
		t := runAt.Time
		task.RunAt = &t
	}
	if leaseExpiresAt.Valid {
		t := leaseExpiresAt.Time
		task.LeaseExpiresAt = &t
	}
	if assigned.Valid {
		task.AssignedSchedulerID = assigned.String
	}
	if lastError.Valid {
		task.LastError = lastError.String
	}
	return task, nil
}

func scanTaskFromRow(row *sql.Row, task *types.Task) error {
	var runAt sql.NullTime
	var leaseExpiresAt sql.NullTime
	var assigned sql.NullString
	var lastError sql.NullString
	if err := row.Scan(
		&task.ID,
		&task.ConversationID,
		&task.Payload,
		&task.ScheduleType,
		&runAt,
		&task.IntervalSec,
		&task.Status,
		&task.NextRunAt,
		&assigned,
		&leaseExpiresAt,
		&task.FailureCount,
		&lastError,
		&task.CreatedAt,
		&task.UpdatedAt,
	); err != nil {
		return err
	}
	if runAt.Valid {
		t := runAt.Time
		task.RunAt = &t
	}
	if leaseExpiresAt.Valid {
		t := leaseExpiresAt.Time
		task.LeaseExpiresAt = &t
	}
	if assigned.Valid {
		task.AssignedSchedulerID = assigned.String
	}
	if lastError.Valid {
		task.LastError = lastError.String
	}
	return nil
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

func backoffForFailures(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	if failures > 7 {
		failures = 7
	}
	base := 30 * time.Second
	d := base * time.Duration(1<<(failures-1))
	max := time.Hour
	if d > max {
		return max
	}
	return d
}

func newID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func nowUTC() time.Time {
	return time.Now().UTC()
}
