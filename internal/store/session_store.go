// Package store
// File: session_store.go
// Description: 대화 세션 및 메시지 저장소 인터페이스 및 SQLite 구현
// Responsibility: conversations/messages 테이블 CRUD 및 스키마 초기화

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

const createConversationsSQL = `
CREATE TABLE IF NOT EXISTS conversations (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

const createMessagesSQL = `
CREATE TABLE IF NOT EXISTS messages (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role            TEXT NOT NULL,
    content         TEXT NOT NULL DEFAULT '',
    tool_calls      TEXT NOT NULL DEFAULT '[]',
    tool_call_id    TEXT NOT NULL DEFAULT '',
    name            TEXT NOT NULL DEFAULT '',
    timestamp       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

const createMessagesIdxSQL = `
CREATE INDEX IF NOT EXISTS idx_messages_conv ON messages(conversation_id);`

// Conversation은 대화 세션 메타데이터이다.
type Conversation struct {
	ID        int64
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SessionMessage는 messages 테이블의 단일 행이다.
type SessionMessage struct {
	ID             int64
	ConversationID int64
	Role           string
	Content        string
	ToolCalls      string // JSON 배열
	ToolCallID     string
	Name           string
	Timestamp      time.Time
}

// SessionStore는 대화 세션 영속화를 담당하는 인터페이스이다.
type SessionStore interface {
	// CreateConversation은 새 대화 세션을 생성하고 ID를 반환한다.
	CreateConversation(ctx context.Context, title string) (int64, error)

	// UpdateConversationTitle은 세션 제목을 업데이트한다.
	UpdateConversationTitle(ctx context.Context, id int64, title string) error

	// GetConversation은 ID로 세션을 조회한다.
	GetConversation(ctx context.Context, id int64) (Conversation, error)

	// ListConversations는 최근 세션 목록을 반환한다.
	ListConversations(ctx context.Context, limit int) ([]Conversation, error)

	// SaveMessage는 메시지를 세션에 저장한다.
	SaveMessage(ctx context.Context, msg SessionMessage) (int64, error)

	// LoadMessages는 세션의 모든 메시지를 시간순으로 반환한다.
	LoadMessages(ctx context.Context, conversationID int64) ([]SessionMessage, error)
}

// initSessionSchema는 conversations/messages 테이블과 인덱스를 생성한다.
func (s *SQLiteStore) initSessionSchema(ctx context.Context) {
	if _, err := s.db.ExecContext(ctx, createConversationsSQL); err != nil {
		slog.Warn("create conversations table", "err", err)
	}
	if _, err := s.db.ExecContext(ctx, createMessagesSQL); err != nil {
		slog.Warn("create messages table", "err", err)
	}
	if _, err := s.db.ExecContext(ctx, createMessagesIdxSQL); err != nil {
		slog.Warn("create messages index", "err", err)
	}
}

// CreateConversation은 새 대화 세션을 생성하고 ID를 반환한다.
func (s *SQLiteStore) CreateConversation(ctx context.Context, title string) (int64, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO conversations (title, created_at, updated_at) VALUES (?, ?, ?)`,
		title, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("create conversation: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get conversation id: %w", err)
	}
	slog.Debug("conversation created", "id", id, "title", title)
	return id, nil
}

// UpdateConversationTitle은 세션 제목과 updated_at을 업데이트한다.
func (s *SQLiteStore) UpdateConversationTitle(ctx context.Context, id int64, title string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE conversations SET title=?, updated_at=? WHERE id=?`,
		title, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("update conversation title %d: %w", id, err)
	}
	return nil
}

// GetConversation은 ID로 세션을 조회한다.
func (s *SQLiteStore) GetConversation(ctx context.Context, id int64) (Conversation, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, created_at, updated_at FROM conversations WHERE id=?`, id)
	var c Conversation
	if err := row.Scan(&c.ID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return Conversation{}, fmt.Errorf("conversation not found: %d", id)
		}
		return Conversation{}, fmt.Errorf("get conversation %d: %w", id, err)
	}
	return c, nil
}

// ListConversations는 최근 세션 목록을 updated_at 내림차순으로 반환한다.
func (s *SQLiteStore) ListConversations(ctx context.Context, limit int) ([]Conversation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, created_at, updated_at FROM conversations ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()

	var convs []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		convs = append(convs, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversations: %w", err)
	}
	return convs, nil
}

// SaveMessage는 메시지를 세션에 저장하고 ID를 반환한다.
func (s *SQLiteStore) SaveMessage(ctx context.Context, msg SessionMessage) (int64, error) {
	toolCallsJSON := msg.ToolCalls
	if toolCallsJSON == "" {
		toolCallsJSON = "[]"
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (conversation_id, role, content, tool_calls, tool_call_id, name, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		msg.ConversationID, msg.Role, msg.Content,
		toolCallsJSON, msg.ToolCallID, msg.Name, time.Now().UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("save message: %w", err)
	}

	// 세션 updated_at 갱신
	_, _ = s.db.ExecContext(ctx,
		`UPDATE conversations SET updated_at=? WHERE id=?`,
		time.Now().UTC(), msg.ConversationID,
	)

	id, _ := res.LastInsertId()
	return id, nil
}

// LoadMessages는 세션의 모든 메시지를 시간순으로 반환한다.
func (s *SQLiteStore) LoadMessages(ctx context.Context, conversationID int64) ([]SessionMessage, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, conversation_id, role, content, tool_calls, tool_call_id, name, timestamp
		 FROM messages WHERE conversation_id=? ORDER BY timestamp ASC`,
		conversationID)
	if err != nil {
		return nil, fmt.Errorf("load messages for conv %d: %w", conversationID, err)
	}
	defer rows.Close()

	var msgs []SessionMessage
	for rows.Next() {
		var m SessionMessage
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content,
			&m.ToolCalls, &m.ToolCallID, &m.Name, &m.Timestamp); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	return msgs, nil
}

// toolCallsToJSON은 ToolCalls 슬라이스를 JSON 문자열로 변환하는 헬퍼이다.
func ToolCallsToJSON(v interface{}) string {
	if v == nil {
		return "[]"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}
