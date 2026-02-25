package termui

import "time"

type DebugTaskRow struct {
	ID               string
	ConversationID   string
	Status           string
	NextRunAt        time.Time
	LastRunStatus    string
	LastRunStartedAt *time.Time
	FailureCount     int
}

type DebugSnapshot struct {
	GeneratedAt       time.Time
	ScopeConversation string

	Workers    []string
	Schedulers []string

	TasksTotal     int
	TasksDue       int
	TasksClaimable int
	TasksLeased    int

	TaskRunsQueued  int
	TaskRunsRunning int

	InboundPending    int
	InboundProcessing int
	InboundError      int

	OutboundPending    int
	OutboundProcessing int
	OutboundError      int

	RecentTasks []DebugTaskRow
}
