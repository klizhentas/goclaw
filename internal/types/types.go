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

func IsMainConversation(conversationID, mainConversationID string) bool {
	return conversationID == mainConversationID
}
