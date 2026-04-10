// Package store
// File: schedule_store.go
// Description: 스케줄 타입 정의, 저장소 인터페이스, SQLite 구현
// Responsibility: schedules 테이블 CRUD 및 활성 스케줄 조회 제공

package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// Schedule은 정기 실행 작업 정의이다.
type Schedule struct {
	ID         int64
	Name       string
	CronExpr   string
	Prompt     string
	LastRun    *time.Time
	LastResult string
	Enabled    bool
	CreatedAt  time.Time
}

// ScheduleStore는 Schedule 영속화 인터페이스이다.
type ScheduleStore interface {
	SaveSchedule(ctx context.Context, s Schedule) (int64, error)
	GetSchedule(ctx context.Context, id int64) (Schedule, error)
	GetScheduleByName(ctx context.Context, name string) (Schedule, error)
	ListSchedules(ctx context.Context) ([]Schedule, error)
	SetScheduleEnabled(ctx context.Context, id int64, enabled bool) error
	UpdateLastRun(ctx context.Context, id int64, result string) error
	DeleteSchedule(ctx context.Context, id int64) error
}

const createScheduleSQL = `
CREATE TABLE IF NOT EXISTS schedules (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL UNIQUE,
    cron_expr   TEXT    NOT NULL,
    prompt      TEXT    NOT NULL,
    last_run    DATETIME,
    last_result TEXT    NOT NULL DEFAULT '',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// initScheduleSchema는 schedules 테이블을 생성한다.
func (s *SQLiteStore) initScheduleSchema(ctx context.Context) {
	if _, err := s.db.ExecContext(ctx, createScheduleSQL); err != nil {
		slog.Warn("init schedule schema", "err", err)
	}
}

// SaveSchedule은 스케줄을 저장하고 ID를 반환한다.
func (s *SQLiteStore) SaveSchedule(ctx context.Context, sch Schedule) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO schedules (name, cron_expr, prompt, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		sch.Name, sch.CronExpr, sch.Prompt, boolToInt(sch.Enabled), time.Now(),
	)
	if err != nil {
		return 0, fmt.Errorf("save schedule: %w", err)
	}
	return res.LastInsertId()
}

// GetSchedule은 ID로 스케줄을 조회한다.
func (s *SQLiteStore) GetSchedule(ctx context.Context, id int64) (Schedule, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, cron_expr, prompt, last_run, last_result, enabled, created_at
		 FROM schedules WHERE id = ?`, id)
	return scanSchedule(row.Scan)
}

// GetScheduleByName은 이름으로 스케줄을 조회한다.
func (s *SQLiteStore) GetScheduleByName(ctx context.Context, name string) (Schedule, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, cron_expr, prompt, last_run, last_result, enabled, created_at
		 FROM schedules WHERE name = ?`, name)
	return scanSchedule(row.Scan)
}

// ListSchedules는 모든 스케줄을 반환한다.
func (s *SQLiteStore) ListSchedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, cron_expr, prompt, last_run, last_result, enabled, created_at
		 FROM schedules ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()

	var result []Schedule
	for rows.Next() {
		sch, err := scanSchedule(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan schedule: %w", err)
		}
		result = append(result, sch)
	}
	return result, nil
}

// SetScheduleEnabled는 스케줄의 활성/비활성 상태를 변경한다.
func (s *SQLiteStore) SetScheduleEnabled(ctx context.Context, id int64, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE schedules SET enabled = ? WHERE id = ?`, boolToInt(enabled), id)
	if err != nil {
		return fmt.Errorf("set schedule enabled: %w", err)
	}
	return nil
}

// UpdateLastRun은 마지막 실행 시간과 결과를 업데이트한다.
func (s *SQLiteStore) UpdateLastRun(ctx context.Context, id int64, result string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE schedules SET last_run = ?, last_result = ? WHERE id = ?`,
		now, result, id)
	if err != nil {
		return fmt.Errorf("update last run: %w", err)
	}
	return nil
}

// DeleteSchedule은 ID로 스케줄을 삭제한다.
func (s *SQLiteStore) DeleteSchedule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	return nil
}

// scanSchedule은 Scan 함수로 Schedule을 구성한다.
func scanSchedule(scan func(dest ...interface{}) error) (Schedule, error) {
	var sch Schedule
	var enabled int
	var lastRun sql.NullTime
	if err := scan(&sch.ID, &sch.Name, &sch.CronExpr, &sch.Prompt,
		&lastRun, &sch.LastResult, &enabled, &sch.CreatedAt); err != nil {
		return Schedule{}, fmt.Errorf("scan schedule: %w", err)
	}
	sch.Enabled = enabled == 1
	if lastRun.Valid {
		sch.LastRun = &lastRun.Time
	}
	return sch, nil
}
