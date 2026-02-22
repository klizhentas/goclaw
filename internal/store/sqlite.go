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
	id, err := newID()
	if err != nil {
		return types.QueueMessage{}, err
	}
	item := types.QueueMessage{
		ID:             id,
		ConversationID: conversationID,
		Direction:      types.QueueDirectionInbound,
		Content:        content,
		Status:         types.QueueStatusPending,
		CreatedAt:      nowUTC(),
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO queue_messages (id, conversation_id, direction, content, status, created_at)
VALUES (?, ?, ?, ?, ?, ?);
`, item.ID, item.ConversationID, item.Direction, item.Content, item.Status, item.CreatedAt)
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
	err = tx.QueryRowContext(ctx, `
SELECT id, conversation_id, direction, content, status, created_at
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
