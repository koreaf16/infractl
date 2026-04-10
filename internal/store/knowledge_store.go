// Package store
// File: knowledge_store.go
// Description: 지식 베이스 저장소 인터페이스 및 SQLite CRUD 구현
// Responsibility: knowledge_base 테이블 CRUD, FTS5 키워드 검색, KnowledgeStore 인터페이스 제공

package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// KnowledgeEntry는 knowledge_base 테이블의 단일 행이다.
type KnowledgeEntry struct {
	ID                int64
	Category          string    // 'error_pattern' | 'system_knowledge' | 'procedure' | 'tip'
	Title             string
	Situation         string    // 어떤 맥락에서 발생했는지
	Resolution        string    // 해결 방법
	ToolName          string    // 관련 도구명
	ErrorPattern      string    // 에러 메시지 패턴 (키워드 매칭용)
	SuccessCommand    string    // 성공한 명령/쿼리
	Embedding         []byte    // Phase 7: 벡터 임베딩 BLOB (nil이면 FTS5만 검색)
	SourceExecutionID *int64    // execution_logs 참조 (nil이면 수동 등록)
	Confidence        float64   // 신뢰도 (0.0~1.0)
	UseCount          int       // 참조된 횟수
	CreatedAt         time.Time
	LastUsedAt        *time.Time
}

// EmbeddingRow는 벡터 검색용 경량 행이다 — 전체 텍스트 없이 ID와 BLOB만 로드한다.
type EmbeddingRow struct {
	ID         int64
	Embedding  []byte
	Confidence float64
}

// KnowledgeStore는 지식 베이스 영속화를 담당하는 인터페이스이다.
type KnowledgeStore interface {
	// SaveKnowledge는 지식 항목을 저장하고 ID를 반환한다.
	SaveKnowledge(ctx context.Context, entry KnowledgeEntry) (int64, error)

	// SearchKnowledge는 FTS5로 키워드 검색하여 관련 지식을 반환한다.
	SearchKnowledge(ctx context.Context, query string, limit int) ([]KnowledgeEntry, error)

	// GetKnowledge는 ID로 지식 항목을 조회한다.
	GetKnowledge(ctx context.Context, id int64) (KnowledgeEntry, error)

	// ListKnowledge는 카테고리 필터로 지식 목록을 최신순 반환한다. category=""이면 전체.
	ListKnowledge(ctx context.Context, category string, limit int) ([]KnowledgeEntry, error)

	// DeleteKnowledge는 ID로 지식 항목을 삭제한다.
	DeleteKnowledge(ctx context.Context, id int64) error

	// IncrementUseCount는 지식 항목의 사용 횟수를 증가시키고 last_used_at을 갱신한다.
	IncrementUseCount(ctx context.Context, id int64) error

	// ListEmbeddings는 embedding이 있는 모든 행의 ID·BLOB·confidence를 반환한다.
	// Phase 7 벡터 유사도 검색용 경량 쿼리이다.
	ListEmbeddings(ctx context.Context) ([]EmbeddingRow, error)

	// CountKnowledge는 카테고리별 지식 건수를 반환한다.
	CountKnowledge(ctx context.Context) (map[string]int, error)
}

// SaveKnowledge는 지식 항목을 저장하고 ID를 반환한다.
// FTS5 가상 테이블에도 동기화한다. entry.Embedding이 nil이면 NULL로 저장한다.
func (s *SQLiteStore) SaveKnowledge(ctx context.Context, entry KnowledgeEntry) (int64, error) {
	var srcID interface{}
	if entry.SourceExecutionID != nil {
		srcID = *entry.SourceExecutionID
	}

	var embedding interface{}
	if len(entry.Embedding) > 0 {
		embedding = entry.Embedding
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO knowledge_base
		 (category, title, situation, resolution, tool_name, error_pattern, success_command,
		  embedding, source_execution_id, confidence, use_count, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		entry.Category, entry.Title, entry.Situation, entry.Resolution,
		entry.ToolName, entry.ErrorPattern, entry.SuccessCommand,
		embedding, srcID, entry.Confidence, time.Now().UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("save knowledge: %w", err)
	}
	id, _ := res.LastInsertId()

	// FTS5 동기화
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO knowledge_fts(rowid, title, situation, resolution, error_pattern, category)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, entry.Title, entry.Situation, entry.Resolution, entry.ErrorPattern, entry.Category,
	); err != nil {
		slog.Warn("sync knowledge to fts5", "id", id, "err", err)
	}

	slog.Info("knowledge saved", "id", id, "category", entry.Category, "title", entry.Title)
	return id, nil
}

// SearchKnowledge는 FTS5 전문 검색으로 query에 매칭되는 지식을 반환한다.
func (s *SQLiteStore) SearchKnowledge(ctx context.Context, query string, limit int) ([]KnowledgeEntry, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT kb.id, kb.category, kb.title, kb.situation, kb.resolution,
		        kb.tool_name, kb.error_pattern, kb.success_command,
		        kb.source_execution_id, kb.confidence, kb.use_count,
		        kb.created_at, kb.last_used_at
		 FROM knowledge_fts
		 JOIN knowledge_base kb ON knowledge_fts.rowid = kb.id
		 WHERE knowledge_fts MATCH ?
		 ORDER BY rank
		 LIMIT ?`,
		query, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search knowledge: %w", err)
	}
	defer rows.Close()
	return scanKnowledgeRows(rows)
}

// GetKnowledge는 ID로 지식 항목을 조회한다.
func (s *SQLiteStore) GetKnowledge(ctx context.Context, id int64) (KnowledgeEntry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, category, title, situation, resolution,
		        tool_name, error_pattern, success_command,
		        source_execution_id, confidence, use_count,
		        created_at, last_used_at
		 FROM knowledge_base WHERE id=?`, id)
	return scanKnowledgeRow(row)
}

// ListKnowledge는 카테고리별 지식 목록을 최신순으로 반환한다.
func (s *SQLiteStore) ListKnowledge(ctx context.Context, category string, limit int) ([]KnowledgeEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	var rows *sql.Rows
	var err error
	if category == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, category, title, situation, resolution,
			        tool_name, error_pattern, success_command,
			        source_execution_id, confidence, use_count,
			        created_at, last_used_at
			 FROM knowledge_base ORDER BY created_at DESC LIMIT ?`, limit)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, category, title, situation, resolution,
			        tool_name, error_pattern, success_command,
			        source_execution_id, confidence, use_count,
			        created_at, last_used_at
			 FROM knowledge_base WHERE category=? ORDER BY created_at DESC LIMIT ?`,
			category, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list knowledge: %w", err)
	}
	defer rows.Close()
	return scanKnowledgeRows(rows)
}

// DeleteKnowledge는 ID로 지식 항목과 FTS5 인덱스를 삭제한다.
func (s *SQLiteStore) DeleteKnowledge(ctx context.Context, id int64) error {
	// FTS5에서 먼저 삭제
	s.db.ExecContext(ctx, //nolint: errcheck
		`INSERT INTO knowledge_fts(knowledge_fts, rowid, title, situation, resolution, error_pattern, category)
		 SELECT 'delete', id, title, situation, resolution, error_pattern, category
		 FROM knowledge_base WHERE id=?`, id)

	res, err := s.db.ExecContext(ctx, `DELETE FROM knowledge_base WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete knowledge %d: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("knowledge not found: %d", id)
	}
	return nil
}

// IncrementUseCount는 지식 항목의 사용 횟수를 증가시키고 last_used_at을 갱신한다.
func (s *SQLiteStore) IncrementUseCount(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE knowledge_base SET use_count=use_count+1, last_used_at=? WHERE id=?`,
		time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("increment use count %d: %w", id, err)
	}
	return nil
}

// ListEmbeddings는 embedding이 있는 모든 행의 ID·BLOB·confidence를 반환한다.
func (s *SQLiteStore) ListEmbeddings(ctx context.Context) ([]EmbeddingRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, embedding, confidence FROM knowledge_base WHERE embedding IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("list embeddings: %w", err)
	}
	defer rows.Close()

	var result []EmbeddingRow
	for rows.Next() {
		var r EmbeddingRow
		if err := rows.Scan(&r.ID, &r.Embedding, &r.Confidence); err != nil {
			return nil, fmt.Errorf("scan embedding row: %w", err)
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate embeddings: %w", err)
	}
	return result, nil
}

// CountKnowledge는 카테고리별 지식 건수를 반환한다.
func (s *SQLiteStore) CountKnowledge(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT category, COUNT(*) FROM knowledge_base GROUP BY category`)
	if err != nil {
		return nil, fmt.Errorf("count knowledge: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var cat string
		var cnt int
		if err := rows.Scan(&cat, &cnt); err != nil {
			return nil, fmt.Errorf("scan count row: %w", err)
		}
		result[cat] = cnt
	}
	return result, nil
}

func scanKnowledgeRows(rows *sql.Rows) ([]KnowledgeEntry, error) {
	var result []KnowledgeEntry
	for rows.Next() {
		entry, err := scanKnowledgeRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate knowledge: %w", err)
	}
	return result, nil
}

type knowledgeScanner interface {
	Scan(dest ...interface{}) error
}

func scanKnowledgeRow(row knowledgeScanner) (KnowledgeEntry, error) {
	var e KnowledgeEntry
	var srcID sql.NullInt64
	var lastUsed sql.NullTime
	err := row.Scan(
		&e.ID, &e.Category, &e.Title, &e.Situation, &e.Resolution,
		&e.ToolName, &e.ErrorPattern, &e.SuccessCommand,
		&srcID, &e.Confidence, &e.UseCount,
		&e.CreatedAt, &lastUsed,
	)
	if err == sql.ErrNoRows {
		return KnowledgeEntry{}, fmt.Errorf("knowledge not found")
	}
	if err != nil {
		return KnowledgeEntry{}, fmt.Errorf("scan knowledge: %w", err)
	}
	if srcID.Valid {
		e.SourceExecutionID = &srcID.Int64
	}
	if lastUsed.Valid {
		e.LastUsedAt = &lastUsed.Time
	}
	return e, nil
}
