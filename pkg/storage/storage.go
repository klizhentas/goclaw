package storage

import (
	"context"
	"time"

	"github.com/klizhentas/goclaw/internal/types"
)

// Store defines the persistence contract used by app/runtime flows.
// Implementations may be SQLite now and other SQL drivers later.
type Store interface {
	Close() error

	CreateConversationIfMissing(ctx context.Context, conversationID string, role types.ConversationRole) error
	AppendMessage(ctx context.Context, conversationID string, role types.MessageRole, content string) (types.Message, error)
	GetRecentMessages(ctx context.Context, conversationID string, limit int) ([]types.Message, error)

	EnqueueInbound(ctx context.Context, conversationID, content string) (types.QueueMessage, error)
	EnqueueInboundForTaskRun(ctx context.Context, conversationID, content, taskRunID string) (types.QueueMessage, error)
	EnqueueOutbound(ctx context.Context, conversationID, content, relatedMessageID string) (types.QueueMessage, error)
	ClaimNextInbound(ctx context.Context, workerID string) (*types.QueueMessage, error)
	ClaimNextOutbound(ctx context.Context, workerID string) (*types.QueueMessage, error)
	MarkQueueDone(ctx context.Context, id string) error
	MarkQueueError(ctx context.Context, id string, errMessage string) error

	CreateTask(ctx context.Context, conversationID, payload string, runAt *time.Time, intervalSec int) (types.Task, error)
	ListTasks(ctx context.Context, conversationID string) ([]types.Task, error)
	RemoveTask(ctx context.Context, taskID string) error
	RemoveTasks(ctx context.Context, conversationID string) (int, error)
	RemoveConversation(ctx context.Context, conversationID string) error
	ClaimDueTask(ctx context.Context, schedulerID string, lease time.Duration) (*types.Task, error)
	CreateTaskRunQueued(ctx context.Context, task types.Task, schedulerID string) (types.TaskRun, error)
	SetTaskRunInboundQueueMessageID(ctx context.Context, runID, queueMessageID string) error
	ReleaseTaskClaim(ctx context.Context, taskID string) error
	CompleteTaskRunSuccess(ctx context.Context, runID, resultContent, assistantMessageID string) error
	CompleteTaskRunFailure(ctx context.Context, runID, errorMessage string) error
	UpdateTaskRunAuthorized(ctx context.Context, runID, callerID string, status types.TaskRunStatus, resultContent, errorMessage, assistantMessageID string) error
}
