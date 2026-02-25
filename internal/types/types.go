package types

import "time"

type ConversationRole string

const (
	ConversationRoleMain    ConversationRole = "main"
	ConversationRoleNonMain ConversationRole = "non_main"
)

type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
)

type QueueDirection string

const (
	QueueDirectionInbound  QueueDirection = "inbound"
	QueueDirectionOutbound QueueDirection = "outbound"
)

type QueueStatus string

const (
	QueueStatusPending    QueueStatus = "pending"
	QueueStatusProcessing QueueStatus = "processing"
	QueueStatusDone       QueueStatus = "done"
	QueueStatusError      QueueStatus = "error"
)

type Conversation struct {
	ID        string
	Role      ConversationRole
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Message struct {
	ID             string
	ConversationID string
	Role           MessageRole
	Content        string
	CreatedAt      time.Time
}

type QueueMessage struct {
	ID               string
	ConversationID   string
	Direction        QueueDirection
	Content          string
	Status           QueueStatus
	CreatedAt        time.Time
	ClaimedAt        *time.Time
	ProcessedAt      *time.Time
	WorkerID         string
	Error            string
	RelatedMessageID string
}

type TaskScheduleType string

const (
	TaskScheduleTypeOneShot  TaskScheduleType = "one_shot"
	TaskScheduleTypeInterval TaskScheduleType = "interval"
)

type TaskStatus string

const (
	TaskStatusActive    TaskStatus = "active"
	TaskStatusPaused    TaskStatus = "paused"
	TaskStatusDeleted   TaskStatus = "deleted"
	TaskStatusCompleted TaskStatus = "completed"
)

type Task struct {
	ID                  string
	ConversationID      string
	Payload             string
	ScheduleType        TaskScheduleType
	RunAt               *time.Time
	IntervalSec         int
	Status              TaskStatus
	NextRunAt           time.Time
	AssignedSchedulerID string
	LeaseExpiresAt      *time.Time
	FailureCount        int
	LastError           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type TaskRunStatus string

const (
	TaskRunStatusQueued  TaskRunStatus = "queued"
	TaskRunStatusRunning TaskRunStatus = "running"
	TaskRunStatusSuccess TaskRunStatus = "success"
	TaskRunStatusFailed  TaskRunStatus = "failed"
)

type TaskRun struct {
	ID                    string
	TaskID                string
	ConversationID        string
	Payload               string
	Status                TaskRunStatus
	SchedulerID           string
	StartedAt             time.Time
	FinishedAt            *time.Time
	ResultContent         string
	Error                 string
	InboundQueueMessageID string
	AssistantMessageID    string
	CreatedAt             time.Time
}

func IsMainConversation(conversationID, mainConversationID string) bool {
	return conversationID == mainConversationID
}
