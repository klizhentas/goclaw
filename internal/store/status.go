package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/gravitational/trace"
	"github.com/klizhentas/goclaw/internal/types"
)

type QueueDirectionStatus struct {
	Direction         types.QueueDirection
	Pending           int
	Processing        int
	Done              int
	Error             int
	OldestPendingAt   *time.Time
	OldestPendingAgeS int
}

type TaskStatusRow struct {
	ID                  string
	ConversationID      string
	Status              types.TaskStatus
	ScheduleType        types.TaskScheduleType
	NextRunAt           time.Time
	AssignedSchedulerID string
	LeaseExpiresAt      *time.Time
	FailureCount        int
	LastError           string
	LastRunStatus       string
	LastRunStartedAt    *time.Time
}

type TaskDiagnostics struct {
	GeneratedAt      time.Time
	ConversationID   string
	TasksTotal       int
	TasksActiveDue   int
	TasksClaimable   int
	TasksLeased      int
	TaskRunsQueued   int
	TaskRunsRunning  int
	WorkerIDs        []string
	SchedulerIDs     []string
	InboundQueue     QueueDirectionStatus
	OutboundQueue    QueueDirectionStatus
	TaskSnapshotRows []TaskStatusRow
}

func (s *SQLiteStore) GetTaskDiagnostics(ctx context.Context, conversationID string, limit int) (TaskDiagnostics, error) {
	if limit < 1 {
		limit = 20
	}
	now := nowUTC()
	out := TaskDiagnostics{
		GeneratedAt:    now,
		ConversationID: conversationID,
	}

	var err error
	out.InboundQueue, err = s.queueDirectionStatus(ctx, types.QueueDirectionInbound, now)
	if err != nil {
		return TaskDiagnostics{}, trace.Wrap(err)
	}
	out.OutboundQueue, err = s.queueDirectionStatus(ctx, types.QueueDirectionOutbound, now)
	if err != nil {
		return TaskDiagnostics{}, trace.Wrap(err)
	}

	out.TasksTotal, err = s.countTasks(ctx, conversationID)
	if err != nil {
		return TaskDiagnostics{}, trace.Wrap(err)
	}
	out.TasksActiveDue, err = s.countActiveDueTasks(ctx, conversationID, now)
	if err != nil {
		return TaskDiagnostics{}, trace.Wrap(err)
	}
	out.TasksClaimable, err = s.countClaimableDueTasks(ctx, conversationID, now)
	if err != nil {
		return TaskDiagnostics{}, trace.Wrap(err)
	}
	out.TasksLeased, err = s.countLeasedTasks(ctx, conversationID, now)
	if err != nil {
		return TaskDiagnostics{}, trace.Wrap(err)
	}

	out.TaskRunsQueued, err = s.countTaskRunsByStatus(ctx, types.TaskRunStatusQueued, conversationID)
	if err != nil {
		return TaskDiagnostics{}, trace.Wrap(err)
	}
	out.TaskRunsRunning, err = s.countTaskRunsByStatus(ctx, types.TaskRunStatusRunning, conversationID)
	if err != nil {
		return TaskDiagnostics{}, trace.Wrap(err)
	}

	out.WorkerIDs, err = s.listObservedWorkers(ctx, now)
	if err != nil {
		return TaskDiagnostics{}, trace.Wrap(err)
	}
	out.SchedulerIDs, err = s.listObservedSchedulers(ctx, now)
	if err != nil {
		return TaskDiagnostics{}, trace.Wrap(err)
	}

	out.TaskSnapshotRows, err = s.listTaskSnapshotRows(ctx, conversationID, limit)
	if err != nil {
		return TaskDiagnostics{}, trace.Wrap(err)
	}

	return out, nil
}

func (s *SQLiteStore) queueDirectionStatus(ctx context.Context, direction types.QueueDirection, now time.Time) (QueueDirectionStatus, error) {
	status := QueueDirectionStatus{Direction: direction}

	rows, err := s.db.QueryContext(ctx, `
SELECT status, COUNT(1)
FROM queue_messages
WHERE direction = ?
GROUP BY status;
`, direction)
	if err != nil {
		return QueueDirectionStatus{}, trace.Wrap(err)
	}
	defer rows.Close()
	for rows.Next() {
		var queueStatus types.QueueStatus
		var count int
		if err := rows.Scan(&queueStatus, &count); err != nil {
			return QueueDirectionStatus{}, trace.Wrap(err)
		}
		switch queueStatus {
		case types.QueueStatusPending:
			status.Pending = count
		case types.QueueStatusProcessing:
			status.Processing = count
		case types.QueueStatusDone:
			status.Done = count
		case types.QueueStatusError:
			status.Error = count
		}
	}
	if err := rows.Err(); err != nil {
		return QueueDirectionStatus{}, trace.Wrap(err)
	}

	var oldest sql.NullTime
	if err := s.db.QueryRowContext(ctx, `
SELECT MIN(created_at)
FROM queue_messages
WHERE direction = ? AND status = ?;
`, direction, types.QueueStatusPending).Scan(&oldest); err != nil {
		return QueueDirectionStatus{}, trace.Wrap(err)
	}
	if oldest.Valid {
		t := oldest.Time.UTC()
		status.OldestPendingAt = &t
		status.OldestPendingAgeS = int(now.Sub(t).Seconds())
	}
	return status, nil
}

func (s *SQLiteStore) countTasks(ctx context.Context, conversationID string) (int, error) {
	query := `SELECT COUNT(1) FROM tasks WHERE status != ?`
	args := []any{types.TaskStatusDeleted}
	if conversationID != "" {
		query += ` AND conversation_id = ?`
		args = append(args, conversationID)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, trace.Wrap(err)
	}
	return n, nil
}

func (s *SQLiteStore) countActiveDueTasks(ctx context.Context, conversationID string, now time.Time) (int, error) {
	query := `SELECT COUNT(1) FROM tasks WHERE status = ? AND next_run_at <= ?`
	args := []any{types.TaskStatusActive, now}
	if conversationID != "" {
		query += ` AND conversation_id = ?`
		args = append(args, conversationID)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, trace.Wrap(err)
	}
	return n, nil
}

func (s *SQLiteStore) countClaimableDueTasks(ctx context.Context, conversationID string, now time.Time) (int, error) {
	query := `
SELECT COUNT(1)
FROM tasks t
WHERE t.status = ?
  AND t.next_run_at <= ?
  AND (t.assigned_scheduler_id IS NULL OR t.assigned_scheduler_id = '' OR t.lease_expires_at IS NULL OR t.lease_expires_at <= ?)
  AND NOT EXISTS (
    SELECT 1 FROM task_runs r
    WHERE r.task_id = t.id AND r.status IN (?, ?)
  )`
	args := []any{types.TaskStatusActive, now, now, types.TaskRunStatusQueued, types.TaskRunStatusRunning}
	if conversationID != "" {
		query += ` AND t.conversation_id = ?`
		args = append(args, conversationID)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, trace.Wrap(err)
	}
	return n, nil
}

func (s *SQLiteStore) countLeasedTasks(ctx context.Context, conversationID string, now time.Time) (int, error) {
	query := `
SELECT COUNT(1)
FROM tasks
WHERE status = ?
  AND assigned_scheduler_id IS NOT NULL
  AND assigned_scheduler_id != ''
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at > ?`
	args := []any{types.TaskStatusActive, now}
	if conversationID != "" {
		query += ` AND conversation_id = ?`
		args = append(args, conversationID)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, trace.Wrap(err)
	}
	return n, nil
}

func (s *SQLiteStore) countTaskRunsByStatus(ctx context.Context, status types.TaskRunStatus, conversationID string) (int, error) {
	query := `SELECT COUNT(1) FROM task_runs WHERE status = ?`
	args := []any{status}
	if conversationID != "" {
		query += ` AND conversation_id = ?`
		args = append(args, conversationID)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, trace.Wrap(err)
	}
	return n, nil
}

func (s *SQLiteStore) listObservedWorkers(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT worker_id
FROM queue_messages
WHERE direction = ?
  AND worker_id IS NOT NULL
  AND worker_id != ''
  AND claimed_at >= ?
ORDER BY worker_id;
`, types.QueueDirectionInbound, now.Add(-5*time.Minute))
	if err != nil {
		return nil, trace.Wrap(err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, trace.Wrap(err)
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, trace.Wrap(err)
	}
	return result, nil
}

func (s *SQLiteStore) listObservedSchedulers(ctx context.Context, now time.Time) ([]string, error) {
	seen := map[string]struct{}{}
	var result []string

	rowsA, err := s.db.QueryContext(ctx, `
SELECT DISTINCT assigned_scheduler_id
FROM tasks
WHERE assigned_scheduler_id IS NOT NULL
  AND assigned_scheduler_id != ''
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at > ?
ORDER BY assigned_scheduler_id;
`, now)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	defer rowsA.Close()
	for rowsA.Next() {
		var id string
		if err := rowsA.Scan(&id); err != nil {
			return nil, trace.Wrap(err)
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			result = append(result, id)
		}
	}
	if err := rowsA.Err(); err != nil {
		return nil, trace.Wrap(err)
	}

	rowsB, err := s.db.QueryContext(ctx, `
SELECT DISTINCT scheduler_id
FROM task_runs
WHERE scheduler_id IS NOT NULL
  AND scheduler_id != ''
  AND started_at >= ?
ORDER BY scheduler_id;
`, now.Add(-15*time.Minute))
	if err != nil {
		return nil, trace.Wrap(err)
	}
	defer rowsB.Close()
	for rowsB.Next() {
		var id string
		if err := rowsB.Scan(&id); err != nil {
			return nil, trace.Wrap(err)
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			result = append(result, id)
		}
	}
	if err := rowsB.Err(); err != nil {
		return nil, trace.Wrap(err)
	}

	return result, nil
}

func (s *SQLiteStore) listTaskSnapshotRows(ctx context.Context, conversationID string, limit int) ([]TaskStatusRow, error) {
	query := `
SELECT
  t.id,
  t.conversation_id,
  t.status,
  t.schedule_type,
  t.next_run_at,
  t.assigned_scheduler_id,
  t.lease_expires_at,
  t.failure_count,
  t.last_error,
  (
    SELECT r.status
    FROM task_runs r
    WHERE r.task_id = t.id
    ORDER BY r.created_at DESC, r.id DESC
    LIMIT 1
  ) AS last_run_status,
  (
    SELECT r.started_at
    FROM task_runs r
    WHERE r.task_id = t.id
    ORDER BY r.created_at DESC, r.id DESC
    LIMIT 1
  ) AS last_run_started_at
FROM tasks t
WHERE t.status != ?`
	args := []any{types.TaskStatusDeleted}
	if conversationID != "" {
		query += ` AND t.conversation_id = ?`
		args = append(args, conversationID)
	}
	query += ` ORDER BY t.next_run_at ASC, t.id ASC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	defer rows.Close()

	out := make([]TaskStatusRow, 0, limit)
	for rows.Next() {
		var row TaskStatusRow
		var assigned sql.NullString
		var lease sql.NullTime
		var lastErr sql.NullString
		var lastRunStatus sql.NullString
		var lastRunStarted sql.NullTime
		if err := rows.Scan(
			&row.ID,
			&row.ConversationID,
			&row.Status,
			&row.ScheduleType,
			&row.NextRunAt,
			&assigned,
			&lease,
			&row.FailureCount,
			&lastErr,
			&lastRunStatus,
			&lastRunStarted,
		); err != nil {
			return nil, trace.Wrap(err)
		}
		if assigned.Valid {
			row.AssignedSchedulerID = assigned.String
		}
		if lease.Valid {
			t := lease.Time.UTC()
			row.LeaseExpiresAt = &t
		}
		if lastErr.Valid {
			row.LastError = lastErr.String
		}
		if lastRunStatus.Valid {
			row.LastRunStatus = lastRunStatus.String
		}
		if lastRunStarted.Valid {
			t := lastRunStarted.Time.UTC()
			row.LastRunStartedAt = &t
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, trace.Wrap(err)
	}
	return out, nil
}
