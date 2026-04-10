// Package store
// File: execlog_store.go
// Description: 도구 실행 이력 저장소 인터페이스 및 SQLite 구현
// Responsibility: execution_logs 테이블 CRUD, 재시도 체인, 스키마 초기화

package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

const createExecutionLogsSQL = `
CREATE TABLE IF NOT EXISTS execution_logs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id     INTEGER DEFAULT 0,
    tool_name      TEXT NOT NULL,
    target_server  TEXT NOT NULL DEFAULT 'localhost',
    input_params   TEXT DEFAULT '{}',
    output         TEXT DEFAULT '',
    error_message  TEXT DEFAULT '',
    exit_code      INTEGER DEFAULT 0,
    duration_ms    INTEGER DEFAULT 0,
    risk_level     TEXT NOT NULL DEFAULT 'none',
    success        INTEGER NOT NULL DEFAULT 1,
    retry_of       INTEGER DEFAULT NULL REFERENCES execution_logs(id),
    user_prompt    TEXT DEFAULT '',
    llm_reasoning  TEXT DEFAULT '',
    timestamp      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

const createExecLogIdxSQL = `
CREATE INDEX IF NOT EXISTS idx_exec_tool ON execution_logs(tool_name);
CREATE INDEX IF NOT EXISTS idx_exec_success ON execution_logs(success);
CREATE INDEX IF NOT EXISTS idx_exec_error ON execution_logs(error_message) WHERE error_message != '';`

// ExecutionLog는 execution_logs 테이블의 단일 행이다.
type ExecutionLog struct {
	ID           int64
	SessionID    int64
	ToolName     string
	TargetServer string
	InputParams  string    // JSON
	Output       string    // 최대 10000자
	ErrorMessage string
	ExitCode     int
	DurationMs   int64
	RiskLevel    string
	Success      bool
	RetryOf      *int64    // nil이면 첫 시도
	UserPrompt   string
	LLMReasoning string
	Timestamp    time.Time
}

// ExecLogStore는 도구 실행 이력 영속화를 담당하는 인터페이스이다.
type ExecLogStore interface {
	// SaveExecutionLog는 도구 실행 결과를 저장하고 ID를 반환한다.
	SaveExecutionLog(ctx context.Context, log ExecutionLog) (int64, error)

	// ListExecutionLogs는 최근 실행 이력을 반환한다.
	ListExecutionLogs(ctx context.Context, sessionID int64, limit int) ([]ExecutionLog, error)

	// GetLastFailedLog는 도구 이름으로 가장 최근 실패 로그를 반환한다.
	GetLastFailedLog(ctx context.Context, toolName string) (ExecutionLog, error)

	// GetExecutionLogByID는 ID로 단일 실행 로그를 반환한다.
	GetExecutionLogByID(ctx context.Context, id int64) (ExecutionLog, error)
}

// initExecLogSchema는 execution_logs 테이블과 인덱스를 생성한다.
func (s *SQLiteStore) initExecLogSchema(ctx context.Context) {
	if _, err := s.db.ExecContext(ctx, createExecutionLogsSQL); err != nil {
		slog.Warn("create execution_logs table", "err", err)
	}
	// 복수 인덱스는 개별 실행
	idxStmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_exec_tool ON execution_logs(tool_name)`,
		`CREATE INDEX IF NOT EXISTS idx_exec_success ON execution_logs(success)`,
		`CREATE INDEX IF NOT EXISTS idx_exec_error ON execution_logs(error_message) WHERE error_message != ''`,
	}
	for _, stmt := range idxStmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			slog.Warn("create execution_logs index", "err", err)
		}
	}
}

// SaveExecutionLog는 도구 실행 이력을 저장하고 ID를 반환한다.
func (s *SQLiteStore) SaveExecutionLog(ctx context.Context, log ExecutionLog) (int64, error) {
	successInt := 0
	if log.Success {
		successInt = 1
	}

	var retryOf interface{}
	if log.RetryOf != nil {
		retryOf = *log.RetryOf
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO execution_logs
		 (session_id, tool_name, target_server, input_params, output, error_message,
		  exit_code, duration_ms, risk_level, success, retry_of, user_prompt, llm_reasoning, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.SessionID, log.ToolName, log.TargetServer,
		log.InputParams, log.Output, log.ErrorMessage,
		log.ExitCode, log.DurationMs, log.RiskLevel, successInt,
		retryOf, log.UserPrompt, log.LLMReasoning, time.Now().UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("save execution log: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// ListExecutionLogs는 세션 ID로 실행 이력을 최신순으로 반환한다.
// sessionID가 0이면 전체 세션의 이력을 반환한다.
func (s *SQLiteStore) ListExecutionLogs(ctx context.Context, sessionID int64, limit int) ([]ExecutionLog, error) {
	var rows *sql.Rows
	var err error
	if sessionID == 0 {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, session_id, tool_name, target_server, input_params, output,
			        error_message, exit_code, duration_ms, risk_level, success,
			        retry_of, user_prompt, llm_reasoning, timestamp
			 FROM execution_logs ORDER BY timestamp DESC LIMIT ?`, limit)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, session_id, tool_name, target_server, input_params, output,
			        error_message, exit_code, duration_ms, risk_level, success,
			        retry_of, user_prompt, llm_reasoning, timestamp
			 FROM execution_logs WHERE session_id=? ORDER BY timestamp DESC LIMIT ?`,
			sessionID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list execution logs: %w", err)
	}
	defer rows.Close()
	return scanExecLogRows(rows)
}

// GetLastFailedLog는 도구 이름으로 가장 최근 실패 로그를 반환한다.
func (s *SQLiteStore) GetLastFailedLog(ctx context.Context, toolName string) (ExecutionLog, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, tool_name, target_server, input_params, output,
		        error_message, exit_code, duration_ms, risk_level, success,
		        retry_of, user_prompt, llm_reasoning, timestamp
		 FROM execution_logs WHERE tool_name=? AND success=0 ORDER BY timestamp DESC LIMIT 1`,
		toolName)
	logs, err := scanExecLogRow(row)
	if err == sql.ErrNoRows {
		return ExecutionLog{}, fmt.Errorf("no failed log for tool: %s", toolName)
	}
	return logs, err
}

// GetExecutionLogByID는 ID로 단일 실행 로그를 반환한다.
func (s *SQLiteStore) GetExecutionLogByID(ctx context.Context, id int64) (ExecutionLog, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, tool_name, target_server, input_params, output,
		        error_message, exit_code, duration_ms, risk_level, success,
		        retry_of, user_prompt, llm_reasoning, timestamp
		 FROM execution_logs WHERE id=?`, id)
	log, err := scanExecLogRow(row)
	if err == sql.ErrNoRows {
		return ExecutionLog{}, fmt.Errorf("execution log not found: %d", id)
	}
	return log, err
}

func scanExecLogRows(rows *sql.Rows) ([]ExecutionLog, error) {
	var logs []ExecutionLog
	for rows.Next() {
		log, err := scanExecLogRow(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate execution logs: %w", err)
	}
	return logs, nil
}

type execLogScanner interface {
	Scan(dest ...interface{}) error
}

func scanExecLogRow(row execLogScanner) (ExecutionLog, error) {
	var l ExecutionLog
	var retryOf sql.NullInt64
	var successInt int
	err := row.Scan(
		&l.ID, &l.SessionID, &l.ToolName, &l.TargetServer,
		&l.InputParams, &l.Output, &l.ErrorMessage, &l.ExitCode,
		&l.DurationMs, &l.RiskLevel, &successInt,
		&retryOf, &l.UserPrompt, &l.LLMReasoning, &l.Timestamp,
	)
	if err != nil {
		return ExecutionLog{}, fmt.Errorf("scan execution log: %w", err)
	}
	l.Success = successInt != 0
	if retryOf.Valid {
		l.RetryOf = &retryOf.Int64
	}
	return l, nil
}
