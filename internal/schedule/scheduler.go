// Package schedule
// File: scheduler.go
// Description: cron 기반 스케줄 실행 관리자
// Responsibility: 스케줄 등록/시작/중지, cron 진입점 관리, 스케줄 CRUD 위임

package schedule

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/robfig/cron/v3"
	"github.com/yourorg/infractl/internal/store"
)

// AgentRunner는 스케줄 실행 시 에이전트를 구동하는 콜백이다.
type AgentRunner func(ctx context.Context, prompt string) (string, error)

// Scheduler는 cron 기반 스케줄을 관리한다.
type Scheduler struct {
	store       store.ScheduleStore
	cron        *cron.Cron
	agentRunner AgentRunner
	entryIDs    map[int64]cron.EntryID // schedule id → cron entry id
}

// NewScheduler는 Scheduler를 생성한다.
func NewScheduler(s store.ScheduleStore, runner AgentRunner) *Scheduler {
	return &Scheduler{
		store:       s,
		cron:        cron.New(),
		agentRunner: runner,
		entryIDs:    make(map[int64]cron.EntryID),
	}
}

// Start는 저장된 활성 스케줄을 모두 로드하고 cron 데몬을 시작한다.
func (s *Scheduler) Start(ctx context.Context) error {
	schedules, err := s.store.ListSchedules(ctx)
	if err != nil {
		return fmt.Errorf("load schedules: %w", err)
	}
	for _, sch := range schedules {
		if sch.Enabled {
			if err := s.addEntry(sch); err != nil {
				slog.Warn("schedule cron add failed", "name", sch.Name, "err", err)
			}
		}
	}
	s.cron.Start()
	slog.Info("scheduler started", "schedules", len(schedules))
	return nil
}

// Stop은 cron 데몬을 중지한다.
func (s *Scheduler) Stop() {
	s.cron.Stop()
}

// Add는 새 스케줄을 저장하고 cron에 등록한다.
func (s *Scheduler) Add(ctx context.Context, sch store.Schedule) (int64, error) {
	id, err := s.store.SaveSchedule(ctx, sch)
	if err != nil {
		return 0, fmt.Errorf("save schedule: %w", err)
	}
	sch.ID = id
	if sch.Enabled {
		if err := s.addEntry(sch); err != nil {
			slog.Warn("add cron entry", "id", id, "err", err)
		}
	}
	slog.Info("schedule added", "id", id, "name", sch.Name, "cron", sch.CronExpr)
	return id, nil
}

// SetEnabled는 스케줄 활성/비활성 상태를 변경한다.
func (s *Scheduler) SetEnabled(ctx context.Context, id int64, enabled bool) error {
	sch, err := s.store.GetSchedule(ctx, id)
	if err != nil {
		return fmt.Errorf("get schedule: %w", err)
	}
	if err := s.store.SetScheduleEnabled(ctx, id, enabled); err != nil {
		return err
	}
	if enabled {
		return s.addEntry(sch)
	}
	s.removeEntry(id)
	return nil
}

// Delete는 스케줄을 cron에서 제거하고 저장소에서 삭제한다.
func (s *Scheduler) Delete(ctx context.Context, id int64) error {
	s.removeEntry(id)
	return s.store.DeleteSchedule(ctx, id)
}

// List는 모든 스케줄을 반환한다.
func (s *Scheduler) List(ctx context.Context) ([]store.Schedule, error) {
	return s.store.ListSchedules(ctx)
}

func (s *Scheduler) addEntry(sch store.Schedule) error {
	entryID, err := s.cron.AddFunc(sch.CronExpr, func() {
		ctx := context.Background()
		result, err := s.agentRunner(ctx, sch.Prompt)
		if err != nil {
			result = fmt.Sprintf("Error: %s", err)
			slog.Error("schedule execution failed", "name", sch.Name, "err", err)
		} else {
			slog.Info("schedule executed", "name", sch.Name)
		}
		if updateErr := s.store.UpdateLastRun(ctx, sch.ID, result); updateErr != nil {
			slog.Warn("update last run", "id", sch.ID, "err", updateErr)
		}
	})
	if err != nil {
		return fmt.Errorf("invalid cron expr '%s': %w", sch.CronExpr, err)
	}
	s.entryIDs[sch.ID] = entryID
	return nil
}

func (s *Scheduler) removeEntry(scheduleID int64) {
	if entryID, ok := s.entryIDs[scheduleID]; ok {
		s.cron.Remove(entryID)
		delete(s.entryIDs, scheduleID)
	}
}
