package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/klizhentas/goclaw/internal/types"
)

func TestTasks_CreateListClaim(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	st, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer st.Close()

	task, err := st.CreateTask(ctx, "conv-a", "ping", nil, 0)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if task.ScheduleType != types.TaskScheduleTypeOneShot {
		t.Fatalf("unexpected schedule type: %s", task.ScheduleType)
	}

	list, err := st.ListTasks(ctx, "conv-a")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 task, got %d", len(list))
	}

	claimed, err := st.ClaimDueTask(ctx, "scheduler-1", 30*time.Second)
	if err != nil {
		t.Fatalf("claim due task: %v", err)
	}
	if claimed == nil || claimed.ID != task.ID {
		t.Fatalf("unexpected claimed task: %#v", claimed)
	}

	nextClaim, err := st.ClaimDueTask(ctx, "scheduler-2", 30*time.Second)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if nextClaim != nil {
		t.Fatalf("expected no task on second claim, got %#v", nextClaim)
	}
}

func TestTasks_RunUpdateAuthorizationAndBackoff(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "task_runs.db")
	st, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer st.Close()

	task, err := st.CreateTask(ctx, "conv-a", "ping", nil, 0)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	run, err := st.CreateTaskRunQueued(ctx, task, "scheduler-1")
	if err != nil {
		t.Fatalf("create task run: %v", err)
	}

	if err := st.UpdateTaskRunAuthorized(ctx, run.ID, "scheduler-2", types.TaskRunStatusFailed, "", "boom", ""); err == nil {
		t.Fatal("expected authorization error")
	}

	if err := st.UpdateTaskRunAuthorized(ctx, run.ID, "scheduler-1", types.TaskRunStatusFailed, "", "boom", ""); err != nil {
		t.Fatalf("update task run failed: %v", err)
	}

	list, err := st.ListTasks(ctx, "conv-a")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 task, got %d", len(list))
	}
	if list[0].FailureCount != 1 {
		t.Fatalf("expected failure_count=1, got %d", list[0].FailureCount)
	}
	if !list[0].NextRunAt.After(time.Now().UTC()) {
		t.Fatalf("expected next_run_at in future, got %s", list[0].NextRunAt)
	}
}

func TestTasks_RemoveTasksScopedAndGlobal(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "task_remove.db")
	st, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer st.Close()

	if _, err := st.CreateTask(ctx, "conv-a", "one", nil, 0); err != nil {
		t.Fatalf("create task conv-a: %v", err)
	}
	if _, err := st.CreateTask(ctx, "conv-b", "two", nil, 0); err != nil {
		t.Fatalf("create task conv-b: %v", err)
	}

	removed, err := st.RemoveTasks(ctx, "conv-a")
	if err != nil {
		t.Fatalf("remove tasks conv-a: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 removed task for conv-a, got %d", removed)
	}

	listA, err := st.ListTasks(ctx, "conv-a")
	if err != nil {
		t.Fatalf("list conv-a: %v", err)
	}
	if len(listA) != 0 {
		t.Fatalf("expected 0 tasks for conv-a, got %d", len(listA))
	}

	removed, err = st.RemoveTasks(ctx, "")
	if err != nil {
		t.Fatalf("remove all tasks: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 removed task globally, got %d", removed)
	}

	all, err := st.ListTasks(ctx, "")
	if err != nil {
		t.Fatalf("list all tasks: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected 0 tasks after global remove, got %d", len(all))
	}
}
